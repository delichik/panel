package containerization

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

func TestImageRefreshDoesNotHoldDatabaseWriteLockWhileResolvingDigests(t *testing.T) {
	svc, taskSvc, fakeAgent, store := newContainerizationTestService(t)
	if _, err := store.AppDB().Exec(`
		INSERT INTO credentials(id,name,type,username,created_at,updated_at)
		VALUES('credential-1','credential','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`
		INSERT INTO servers(id,name,host,port,credential_id,docker_host,traits,created_at,updated_at)
		VALUES('server-1','server','127.0.0.1',22,'credential-1','unix:///var/run/docker.sock','{}','now','now')`); err != nil {
		t.Fatal(err)
	}
	fakeAgent.images = []agentcontract.DockerImage{{
		RepoTags:    []string{"example.com/app:latest"},
		RepoDigests: []string{"example.com/app@sha256:local"},
	}}
	resolver := &blockingImageResolver{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc.resolver = resolver
	task, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:         TaskImageRefresh,
		ServerID:     "server-1",
		ResourceType: "server",
		ResourceID:   "server-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := taskSvc.Start(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	go svc.runImageRefresh(taskSvc.ExecutionContext(task.ID), task, "server-1")
	select {
	case <-resolver.entered:
	case <-time.After(time.Second):
		t.Fatal("image resolver was not called")
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if _, err := store.AppDB().ExecContext(writeCtx, `
		INSERT INTO runtime_settings(key,value,updated_at) VALUES('lock-test','ok','now')
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`); err != nil {
		t.Fatalf("database write was blocked while resolving remote digest: %v", err)
	}
	close(resolver.release)
	waitTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
	var latestDigest string
	if err := store.AppDB().QueryRow(`SELECT latest_digest FROM image_updates WHERE server_id=? AND reference=?`, "server-1", "example.com/app:latest").Scan(&latestDigest); err != nil {
		t.Fatal(err)
	}
	if latestDigest != "sha256:latest" {
		t.Fatalf("latest digest = %q, want sha256:latest", latestDigest)
	}
}

func TestManagedLabelsOnlyAcceptsApplicationLabels(t *testing.T) {
	appID, instanceID, managed := managedLabels(map[string]string{
		"panel.application.managed":     "true",
		"panel.application.id":          "app-1",
		"panel.application.instance.id": "instance-1",
	})
	if !managed || appID != "app-1" || instanceID != "instance-1" {
		t.Fatalf("new labels not recognized: managed=%v app=%q instance=%q", managed, appID, instanceID)
	}

	if _, _, managed := managedLabels(map[string]string{
		"panel.application_id": "app-1",
		"panel.instance_id":    "instance-1",
	}); managed {
		t.Fatal("unmanaged labels must not be recognized")
	}
}

func TestExecuteSerializesOperationsPerServer(t *testing.T) {
	svc := &Service{queues: map[string]*serverQueue{}}
	var running int32
	var maxRunning int32
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := svc.Execute(context.Background(), "server-1", func(context.Context) error {
				current := atomic.AddInt32(&running, 1)
				for {
					max := atomic.LoadInt32(&maxRunning)
					if current <= max || atomic.CompareAndSwapInt32(&maxRunning, max, current) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return nil
			}); err != nil {
				t.Errorf("execute: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(&maxRunning); got != 1 {
		t.Fatalf("same-server operations ran concurrently: max=%d", got)
	}
}

func TestExecuteAllowsDifferentServersToRunConcurrently(t *testing.T) {
	svc := &Service{queues: map[string]*serverQueue{}}
	entered := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, serverID := range []string{"server-1", "server-2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = svc.Execute(context.Background(), id, func(context.Context) error {
				entered <- id
				<-release
				return nil
			})
		}(serverID)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first server operation did not start")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("different server operation did not run concurrently")
	}
	close(release)
	wg.Wait()
}

func TestContainerActionRunsSynchronouslyWithoutRefreshTask(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	result, err := svc.ContainerAction(context.Background(), "server-1", "container-1", "restart")
	if err != nil {
		t.Fatalf("container action: %v", err)
	}
	if result.RefreshTaskID != "" {
		t.Fatalf("container action should not create refresh task, got %q", result.RefreshTaskID)
	}
	fakeAgent.mu.Lock()
	actions := append([]string(nil), fakeAgent.actions...)
	fakeAgent.mu.Unlock()
	if len(actions) != 1 || actions[0] != "container-1:restart" {
		t.Fatalf("expected synchronous container action before return, got %#v", actions)
	}
	var operationTasks int
	if err := store.LogDB().QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&operationTasks); err != nil {
		t.Fatal(err)
	}
	if operationTasks != 0 {
		t.Fatalf("container action should not create tasks, got %d", operationTasks)
	}
}

func TestContainerActionRejectsManagedApplicationContainer(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	if _, err := store.AppDB().Exec(`
		INSERT INTO credentials(id,name,type,username,created_at,updated_at)
		VALUES('credential-1','credential','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`
		INSERT INTO servers(id,name,host,port,credential_id,docker_host,traits,created_at,updated_at)
		VALUES('server-1','server','127.0.0.1',22,'credential-1','unix:///var/run/docker.sock','{}','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,job_id,namespace,created_at,updated_at)
		VALUES('app-1','web',1,'name: web\nimage: nginx\n','all','[]',1,'hash-1','panel-web','apps','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO container_observations(server_id,container_id,sample_at,container_json,managed,application_id,instance_id,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		"server-1",
		"container-1",
		time.Now().UTC().Format(time.RFC3339Nano),
		`{"id":"container-1"}`,
		1,
		"app-1",
		"app-1-server-1",
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ContainerAction(context.Background(), "server-1", "container-1", "restart"); err != nil {
		t.Fatalf("managed application container action should be allowed: %v", err)
	}
	fakeAgent.mu.Lock()
	actions := append([]string(nil), fakeAgent.actions...)
	fakeAgent.mu.Unlock()
	if len(actions) == 0 {
		t.Fatal("managed application container should be mutated directly")
	}
}

func TestApplicationReconcileUsesBackoffAfterFailures(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',1,?)`, time.Now().UTC().Add(30*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "exited",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("expected reconcile to wait for shared backoff, got %#v", inputs)
	}
	if len(updater.plans) != 0 {
		t.Fatalf("expected reconcile planner to wait for shared backoff, got %#v", updater.plans)
	}
}

func TestApplicationReconcileSkipsUntilStoredBackoffTime(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	nextRunAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',4,?)`, nextRunAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "exited",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("expected no reconcile input before stored backoff time, got %#v", inputs)
	}
	if len(updater.plans) != 0 {
		t.Fatalf("expected no reconcile plan before stored backoff time, got %#v", updater.plans)
	}
	var storedNextRunAt string
	if err := store.AppDB().QueryRow(`SELECT reconcile_next_run_at FROM application_reconcile_states WHERE application_id='app-1'`).Scan(&storedNextRunAt); err != nil {
		t.Fatal(err)
	}
	if storedNextRunAt != nextRunAt.Format(time.RFC3339Nano) {
		t.Fatalf("next run moved: got %s want %s", storedNextRunAt, nextRunAt.Format(time.RFC3339Nano))
	}
}

func TestAgentReportContainerChangeBypassesBackoffAfterDrift(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	nextRunAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',4,?)`, nextRunAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "exited",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{
		Type: "agent_report",
		Payload: ApplicationReconcileTrigger{
			ServerIDs: []string{"server-1"},
			Reason:    "container_change",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("reconcile collector must not create target task inputs, got %#v", inputs)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != app.ID || updater.plans[0].ServerIDs[0] != "server-1" || updater.plans[0].Force || !updater.plans[0].ObservedRuntimeDrift || updater.plans[0].TriggerType != "agent_report" {
		t.Fatalf("container change plan = %#v", updater.plans)
	}
}

func TestNonForcedExplicitApplicationReconcileRespectsStoredBackoffTime(t *testing.T) {
	svc, _, _, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	svc.apps = &fakeApplicationUpdater{apps: []applications.Application{app}}
	nextRunAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',4,?)`, nextRunAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{
		Type: "application_change",
		Payload: ApplicationReconcileTrigger{
			ApplicationIDs: []string{app.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("expected explicit reconcile to wait for stored backoff, got %#v", inputs)
	}
}

func TestForcedExplicitApplicationReconcileIgnoresStoredBackoffTime(t *testing.T) {
	svc, _, _, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	nextRunAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',4,?)`, nextRunAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{
		Type: "application_change",
		Payload: ApplicationReconcileTrigger{
			ApplicationIDs: []string{app.ID},
			ServerIDs:      []string{"server-1"},
			Force:          true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("reconcile collector must not create target task inputs, got %#v", inputs)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != app.ID || updater.plans[0].ServerIDs[0] != "server-1" || !updater.plans[0].Force {
		t.Fatalf("forced reconcile plan = %#v", updater.plans)
	}
}

func TestApplicationReconcilePlansWhenManagedFilesDrift(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "running",
		Labels: map[string]string{
			"panel.application.managed":             "true",
			"panel.application.id":                  app.ID,
			"panel.application.instance.id":         app.ID + "-server-1",
			"panel.application.generation":          "3",
			"panel.application.spec.hash":           "hash-3",
			"panel.application.managed_files.drift": "true",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("reconcile collector must not create target task inputs, got %#v", inputs)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != app.ID || len(updater.plans[0].ServerIDs) != 1 || updater.plans[0].ServerIDs[0] != "server-1" || !updater.plans[0].ObservedRuntimeDrift {
		t.Fatalf("managed file drift plan = %#v", updater.plans)
	}
}

func TestApplicationReconcilePlansWhenManagedContainerIsMissing(t *testing.T) {
	svc, _, _, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	saveReportedContainers(t, svc, "server-1", []agentcontract.DockerContainer{})

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("reconcile collector must not create target task inputs, got %#v", inputs)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != app.ID || len(updater.plans[0].ServerIDs) != 1 || updater.plans[0].ServerIDs[0] != "server-1" || !updater.plans[0].ObservedRuntimeDrift {
		t.Fatalf("missing container plan = %#v", updater.plans)
	}
}

func TestApplicationReconcilePlansWhenManagedContainerIsCreatedWithoutReconcileState(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "created",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	if _, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"}); err != nil {
		t.Fatal(err)
	}
	if len(updater.plans) != 1 || len(updater.plans[0].ServerIDs) != 1 || updater.plans[0].ServerIDs[0] != "server-1" {
		t.Fatalf("created container plan = %#v", updater.plans)
	}
}

func TestApplicationReconcileDoesNotPlanHealthyContainerWithoutReconcileState(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "running",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	if _, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"}); err != nil {
		t.Fatal(err)
	}
	if len(updater.plans) != 0 {
		t.Fatalf("healthy container should not plan, got %#v", updater.plans)
	}
}

func TestApplicationReconcileSkipsInstanceOutsideCurrentDesiredState(t *testing.T) {
	tests := []struct {
		name         string
		app          applications.Application
		desiredState string
	}{
		{
			name:         "desired stopped",
			app:          applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3", DeploymentMode: applications.DeploymentModeAll},
			desiredState: "stopped",
		},
		{
			name:         "removed selected server",
			app:          applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3", DeploymentMode: applications.DeploymentModeSelected, DeploymentServers: []string{}},
			desiredState: "running",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, store := newContainerizationTestService(t)
			insertReconcileFixtureRows(t, store, tt.app)
			if _, err := store.AppDB().Exec(`UPDATE application_instances SET desired_state=? WHERE application_id=?`, tt.desiredState, tt.app.ID); err != nil {
				t.Fatal(err)
			}
			updater := &fakeApplicationUpdater{apps: []applications.Application{tt.app}}
			svc.apps = updater
			saveReportedContainers(t, svc, "server-1", []agentcontract.DockerContainer{})

			if _, err := svc.CollectApplicationReconcileTasks(context.Background(), "op-1", tasks.PeriodicTrigger{Type: "scheduler"}); err != nil {
				t.Fatal(err)
			}
			if len(updater.plans) != 0 {
				t.Fatalf("instance outside desired state should not plan, got %#v", updater.plans)
			}
		})
	}
}

func TestApplicationReconcileFailuresClearAfterFiveHealthyObservations(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	insertReconcileFixtureRows(t, store, app)
	svc.apps = &fakeApplicationUpdater{apps: []applications.Application{app}}
	if _, err := store.AppDB().Exec(`INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at,reconcile_failures,reconcile_next_run_at)
		VALUES('app-1-server-1','app-1','server-1','now',2,?)`, time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	fakeAgent.containers = []agentcontract.DockerContainer{{
		ID:    "container-1",
		State: "running",
		Labels: map[string]string{
			"panel.application.managed":     "true",
			"panel.application.id":          app.ID,
			"panel.application.instance.id": app.ID + "-server-1",
			"panel.application.generation":  "3",
			"panel.application.spec.hash":   "hash-3",
		},
	}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)

	for i := 0; i < applicationReconcileHealthyChecksToReset-1; i++ {
		inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"})
		if err != nil {
			t.Fatal(err)
		}
		if len(inputs) != 0 {
			t.Fatalf("healthy app should not reconcile, got %#v", inputs)
		}
		var failures, streak int
		if err := store.AppDB().QueryRow(`SELECT reconcile_failures,reconcile_success_streak FROM application_reconcile_states WHERE application_id='app-1'`).Scan(&failures, &streak); err != nil {
			t.Fatal(err)
		}
		if failures != 2 || streak != i+1 {
			t.Fatalf("after %d healthy checks failures=%d streak=%d", i+1, failures, streak)
		}
	}
	if _, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{Type: "scheduler"}); err != nil {
		t.Fatal(err)
	}
	var failures, streak int
	var nextRunAt string
	if err := store.AppDB().QueryRow(`SELECT reconcile_failures,reconcile_success_streak,reconcile_next_run_at FROM application_reconcile_states WHERE application_id='app-1'`).Scan(&failures, &streak, &nextRunAt); err != nil {
		t.Fatal(err)
	}
	if failures != 0 || streak != 0 || nextRunAt != "" {
		t.Fatalf("expected retry state cleared, failures=%d streak=%d next=%q", failures, streak, nextRunAt)
	}
}

func TestApplicationReconcileForcePlansDeployment(t *testing.T) {
	svc, _, _, _ := newContainerizationTestService(t)
	ctx := context.Background()
	app := applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 3, SpecHash: "hash-3"}
	updater := &fakeApplicationUpdater{apps: []applications.Application{app}}
	svc.apps = updater

	inputs, err := svc.CollectApplicationReconcileTasks(ctx, "op-1", tasks.PeriodicTrigger{
		Type: "manual",
		Payload: ApplicationReconcileTrigger{
			ApplicationIDs: []string{app.ID},
			ServerIDs:      []string{"server-1"},
			Force:          true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 0 {
		t.Fatalf("reconcile collector must not create target task inputs, got %#v", inputs)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != app.ID || updater.plans[0].ServerIDs[0] != "server-1" || !updater.plans[0].Force {
		t.Fatalf("deploy plan = %#v", updater.plans)
	}
}

func insertReconcileServerRow(t *testing.T, store *storage.Store) {
	t.Helper()
	if _, err := store.AppDB().Exec(`
		INSERT INTO credentials(id,name,type,username,created_at,updated_at)
		VALUES('credential-1','credential','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`
		INSERT INTO servers(id,name,host,port,credential_id,docker_host,traits,created_at,updated_at)
		VALUES('server-1','server','127.0.0.1',22,'credential-1','unix:///var/run/docker.sock','{}','now','now')`); err != nil {
		t.Fatal(err)
	}
}

func enabledInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func insertReconcileFixtureRows(t *testing.T, store *storage.Store, app applications.Application) {
	t.Helper()
	if _, err := store.AppDB().Exec(`
		INSERT INTO credentials(id,name,type,username,created_at,updated_at)
		VALUES('credential-1','credential','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`
		INSERT INTO servers(id,name,host,port,credential_id,docker_host,traits,created_at,updated_at)
		VALUES('server-1','server','127.0.0.1',22,'credential-1','unix:///var/run/docker.sock','{}','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.Name, enabledInt(app.Enabled), "name: web\nimage: nginx\n", "all", "[]", app.Generation, app.SpecHash, "panel-web", "apps", "now", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,desired_state,status,runtime_spec_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, app.ID+"-server-1", app.ID, "server-1", "panel-web", "running", "running", "{}", "now", "now"); err != nil {
		t.Fatal(err)
	}
}

func TestContainerLogsClampsTail(t *testing.T) {
	svc, _, fakeAgent, _ := newContainerizationTestService(t)
	result, err := svc.ContainerLogs(context.Background(), "server-1", "container-1", 20000)
	if err != nil {
		t.Fatalf("container logs: %v", err)
	}
	if result.Logs != "logs" {
		t.Fatalf("logs = %q", result.Logs)
	}
	if fakeAgent.logTail != 10000 {
		t.Fatalf("tail = %d, want 10000", fakeAgent.logTail)
	}
}

func TestImagesMarkInUseFromReportedContainerImageIDs(t *testing.T) {
	svc, _, _, store := newContainerizationTestService(t)
	ctx := context.Background()
	insertReconcileFixtureRows(t, store, applications.Application{ID: "app-1", Name: "web", Enabled: true, Generation: 1, SpecHash: "hash"})
	usedID := "sha256:used"
	unusedID := "sha256:unused"
	if err := svc.replaceResourceSnapshot(ctx, "server-1", "images", []agentcontract.DockerImage{
		{ID: usedID, RepoTags: []string{"example/app:1"}, RepoDigests: []string{"example/app@sha256:abc"}},
		{ID: unusedID, RepoTags: []string{"example/tool:2"}, RepoDigests: []string{"example/tool@sha256:def"}},
	}); err != nil {
		t.Fatal(err)
	}
	saveReportedContainers(t, svc, "server-1", []agentcontract.DockerContainer{
		{
			ID:      "container-1",
			Names:   []string{"/app-1"},
			Image:   "example/app:1",
			ImageID: usedID,
			State:   "running",
			Status:  "Up 1 hour",
			Labels: map[string]string{
				"panel.application.managed":     "true",
				"panel.application.id":          "app-1",
				"panel.application.instance.id": "instance-1",
			},
		},
	})

	list, err := svc.Images(ctx, "server-1")
	if err != nil {
		t.Fatalf("images: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("images = %d, want 2", len(list.Items))
	}
	var used, unused *Image
	for i := range list.Items {
		switch list.Items[i].ID {
		case usedID:
			used = &list.Items[i]
		case unusedID:
			unused = &list.Items[i]
		}
	}
	if used == nil || unused == nil {
		t.Fatalf("expected both images in listing: %#v", list.Items)
	}
	if !used.InUse {
		t.Fatalf("image %s inUse = false, want true", usedID)
	}
	if unused.InUse {
		t.Fatalf("image %s inUse = true, want false", unusedID)
	}
	if len(used.ApplicationIDs) != 1 || used.ApplicationIDs[0] != "app-1" {
		t.Fatalf("applicationIds = %#v, want [app-1]", used.ApplicationIDs)
	}
	if !used.Upgradeable {
		t.Fatal("upgradeable = false, want true for managed application image")
	}
}

func TestTriggerApplicationReconcileUsesPeriodicPayload(t *testing.T) {
	svc, taskSvc, _, _ := newContainerizationTestService(t)
	taskSvc.MustRegister(tasks.Definition{
		Type:              applications.TaskTypeTargetBatch,
		ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
		Execute:           func(tasks.TaskContext) error { return nil },
	})
	taskSvc.MustRegister(tasks.Definition{
		Type: applications.TaskTypeTargetApply,
		Execute: func(tc tasks.TaskContext) error {
			return tc.Service.Complete(tc.Context, tc.Task.ID, "Application deployment handled")
		},
	})
	updater := &fakeApplicationUpdater{}
	svc.apps = updater
	task, created, err := svc.TriggerApplicationReconcile(context.Background(), tasks.PeriodicTrigger{
		Type:                "facility_app",
		TriggerResourceType: "application",
		TriggerResourceID:   "facility-reverse-proxy",
		Payload: ApplicationReconcileTrigger{
			ApplicationIDs: []string{"facility-reverse-proxy"},
			StopServers:    []string{"server-old"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created || task.ID != "" {
		t.Fatalf("reconcile trigger should only plan lifecycle targets, created=%v task=%#v", created, task)
	}
	if len(updater.plans) != 1 || updater.plans[0].ApplicationID != "facility-reverse-proxy" || updater.plans[0].StopServers[0] != "server-old" {
		t.Fatalf("unexpected reconcile plans: %#v", updater.plans)
	}
}

func newContainerizationTestService(t *testing.T) (*Service, *tasks.Service, *fakeContainerizationAgent, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskSvc := tasks.NewService(store.LogDB())
	fakeAgent := &fakeContainerizationAgent{}
	svc := NewService(store.AppDB(), fakeServerProvider{server.Server{
		ID:        "server-1",
		Reachable: true,
		Traits: map[string]string{
			agentcontract.TraitURL:    "http://agent",
			agentcontract.TraitStatus: agentcontract.StatusCompatible,
		},
	}}, fakeAgent, taskSvc)
	svc.RegisterTasks(taskSvc)
	return svc, taskSvc, fakeAgent, store
}

func waitTaskStatus(t *testing.T, taskSvc *tasks.Service, taskID, status string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := taskSvc.Get(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get task %s: %v", taskID, err)
	}
	t.Fatalf("task %s status = %s, want %s", taskID, task.Status, status)
}

type fakeServerProvider struct {
	srv server.Server
}

func (p fakeServerProvider) Get(context.Context, string) (server.Server, error) {
	return p.srv, nil
}

func (p fakeServerProvider) List(context.Context) ([]server.Server, error) {
	return []server.Server{p.srv}, nil
}

type fakeContainerizationAgent struct {
	mu         sync.Mutex
	actions    []string
	logTail    int
	images     []agentcontract.DockerImage
	containers []agentcontract.DockerContainer
}

func (a *fakeContainerizationAgent) DockerContainers(context.Context, string) ([]agentcontract.DockerContainer, error) {
	return append([]agentcontract.DockerContainer(nil), a.containers...), nil
}

func (a *fakeContainerizationAgent) DockerContainerLogs(_ context.Context, _, id string, tail int) (agentcontract.DockerContainerLogsResponse, error) {
	a.logTail = tail
	return agentcontract.DockerContainerLogsResponse{ContainerID: id, Logs: "logs"}, nil
}

func (a *fakeContainerizationAgent) DockerContainerAction(_ context.Context, _ string, id, action string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, id+":"+action)
	return nil
}

func (a *fakeContainerizationAgent) DockerContainerDelete(context.Context, string, string) error {
	return nil
}

func (a *fakeContainerizationAgent) DockerImages(context.Context, string) ([]agentcontract.DockerImage, error) {
	return a.images, nil
}

func (a *fakeContainerizationAgent) DockerImagePull(context.Context, string, string) error {
	return nil
}

func (a *fakeContainerizationAgent) DockerImageDelete(context.Context, string, string) error {
	return nil
}

func (a *fakeContainerizationAgent) DockerNetworks(context.Context, string) ([]agentcontract.DockerNetwork, error) {
	return nil, nil
}

func (a *fakeContainerizationAgent) DockerVolumes(context.Context, string) ([]agentcontract.DockerVolume, error) {
	return nil, nil
}

func (a *fakeContainerizationAgent) DockerVolumeDelete(context.Context, string, string) error {
	return nil
}

func saveReportedContainers(t *testing.T, svc *Service, serverID string, items []agentcontract.DockerContainer) {
	t.Helper()
	if err := svc.SaveReportedContainers(context.Background(), serverID, time.Now().UTC(), items); err != nil {
		t.Fatal(err)
	}
}

type blockingImageResolver struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type fakeApplicationUpdater struct {
	apps  []applications.Application
	err   error
	plans []applications.DeploymentPlanRequest
}

func (f fakeApplicationUpdater) List(context.Context) ([]applications.Application, error) {
	return append([]applications.Application(nil), f.apps...), nil
}

func (f fakeApplicationUpdater) UpdateImage(context.Context, string) (applications.OperationResult, error) {
	return applications.OperationResult{}, nil
}

func (f fakeApplicationUpdater) Deploy(context.Context, string) (applications.OperationResult, error) {
	return applications.OperationResult{}, nil
}

func (f *fakeApplicationUpdater) PlanApplicationDeployment(_ context.Context, req applications.DeploymentPlanRequest) (applications.DeploymentPlanResult, error) {
	f.plans = append(f.plans, req)
	return applications.DeploymentPlanResult{}, f.err
}

func (r *blockingImageResolver) Resolve(ctx context.Context, _ string) (applications.ImageDigestResult, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-ctx.Done():
		return applications.ImageDigestResult{}, ctx.Err()
	case <-r.release:
		return applications.ImageDigestResult{Digest: "sha256:latest"}, nil
	}
}

func TestSaveReportedContainersNilKeepsExistingObservations(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	insertReconcileServerRow(t, store)
	fakeAgent.containers = []agentcontract.DockerContainer{{ID: "container-1", Names: []string{"web"}, State: "running"}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)
	items, observedAt, err := svc.reportedContainerSummaries(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || observedAt == nil {
		t.Fatalf("seed observations: items=%#v observedAt=%v", items, observedAt)
	}
	if err := svc.SaveReportedContainers(context.Background(), "server-1", time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	items, observedAt, err = svc.reportedContainerSummaries(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "container-1" || observedAt == nil {
		t.Fatalf("nil report must keep existing observations, got items=%#v observedAt=%v", items, observedAt)
	}
}

func TestSaveReportedContainersEmptyListClearsObservations(t *testing.T) {
	svc, _, fakeAgent, store := newContainerizationTestService(t)
	insertReconcileServerRow(t, store)
	fakeAgent.containers = []agentcontract.DockerContainer{{ID: "container-1", Names: []string{"web"}, State: "running"}}
	saveReportedContainers(t, svc, "server-1", fakeAgent.containers)
	if err := svc.SaveReportedContainers(context.Background(), "server-1", time.Now().UTC(), []agentcontract.DockerContainer{}); err != nil {
		t.Fatal(err)
	}
	items, observedAt, err := svc.reportedContainerSummaries(context.Background(), "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || observedAt != nil {
		t.Fatalf("explicit empty list must clear observations, got items=%#v observedAt=%v", items, observedAt)
	}
}

func TestRunQueueRecoversFromPanic(t *testing.T) {
	svc := &Service{queues: map[string]*serverQueue{}}
	q := svc.queue("server-1")
	errs := make(chan error, 2)
	q.jobs <- queueJob{run: func(context.Context) error { panic("boom") }, result: errs}
	q.jobs <- queueJob{run: func(context.Context) error { return nil }, result: errs}
	first := <-errs
	if first == nil {
		t.Fatal("panicked job must return an error")
	}
	second := <-errs
	if second != nil {
		t.Fatalf("second job must still run after a panic, got %v", second)
	}
}
