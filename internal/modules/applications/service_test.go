package applications

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
)

func TestCreateDisabledAppStoresRowAndDoesNotDeployRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID == "" || app.Name != "web" || app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	if app.Generation != 1 || app.JobID != "panel-web" || app.Namespace != "apps" {
		t.Fatalf("app persistence fields = %#v", app)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("runtime deploys = %#v", runtime.deploys)
	}
}

func TestDeployEnablesDisabledApplicationWithVersionCAS(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := app.Version
	result, err := svc.Deploy(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatal("expected deploy to trigger an application reconcile task")
	}
	after, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Enabled || after.Version != beforeVersion+1 {
		t.Fatalf("HTTP deploy should explicitly persist enable intent with version bump, before=%d after=%#v", beforeVersion, after)
	}
}

func TestListSummariesDoesNotLoadApplicationDetails(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `UPDATE applications SET enabled=1,spec_yaml=?,reverse_proxy_json=?,image_update_available=1 WHERE id=?`,
		"not: [valid", "not-json", app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "instance-1", app.ID, "srv-a", "panel-web", "container-1", "running", appruntime.StatusRunning, "not-json", 1, "", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "instance-2", app.ID, "srv-b", "panel-web", "container-2", "running", appruntime.StatusRunning, "not-json", 1, "", now, now); err != nil {
		t.Fatal(err)
	}

	page, err := svc.ListSummaries(ctx, 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	summaries := page.Items
	if len(summaries) != 1 || page.Total != 1 {
		t.Fatalf("summaries = %#v", page)
	}
	if summaries[0].ID != app.ID || summaries[0].RuntimeStatus != appruntime.StatusRunning || !summaries[0].ImageUpdateAvailable {
		t.Fatalf("summary = %#v", summaries[0])
	}
	if summaries[0].InstanceCount != 2 {
		t.Fatalf("summary instance count = %d", summaries[0].InstanceCount)
	}
}

func TestCreateEnabledAppDeploysToAgentRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if len(runtime.writes) != 2 || len(runtime.pulls) != 2 || len(runtime.deletes) != 2 || len(runtime.actions) != 2 {
		t.Fatalf("atomic deploy calls writes=%#v pulls=%#v deletes=%#v actions=%#v", runtime.writes, runtime.pulls, runtime.deletes, runtime.actions)
	}
	if runtime.pulls[0] != "nginx" || runtime.deletes[0] != "panel-web" || runtime.actions[0] != "container-srv-a:start" {
		t.Fatalf("atomic deploy sequence pulls=%#v deletes=%#v actions=%#v", runtime.pulls, runtime.deletes, runtime.actions)
	}
	deploy := runtime.deploys[0]
	if deploy.ServerID != "srv-a" || deploy.Spec.ApplicationID != app.ID || deploy.Spec.InstanceID != app.ID+"-srv-a" {
		t.Fatalf("deploy = %#v", deploy)
	}
	if deploy.Spec.ContainerName != "panel-web" || deploy.Spec.Env["PANEL_SERVER_ID"] != "srv-a" {
		t.Fatalf("runtime spec = %#v", deploy.Spec)
	}
}

func TestCreateEnabledAnyTLSHostNetworkAppDeploysRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:    "anytls",
		Enabled: true,
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - "/app/anytls-server"
  - "-l"
  - ":9443"
  - "-p"
  - "password"
restart:
  policy: "unless-stopped"
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled || len(runtime.deploys) != 2 {
		t.Fatalf("app=%#v deploys=%#v", app, runtime.deploys)
	}
	spec := runtime.deploys[0].Spec
	if spec.NetworkMode != "host" || len(spec.Ports) != 0 {
		t.Fatalf("runtime network = %q ports %#v", spec.NetworkMode, spec.Ports)
	}
	if len(spec.Command) != 5 || spec.Command[0] != "/app/anytls-server" || spec.Command[2] != ":9443" {
		t.Fatalf("command = %#v", spec.Command)
	}
}

func TestCreateEnabledAppWrapsRuntimeDeploymentError(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	runtime.deployErr = errors.New("create container failed: invalid runtime config")

	_, err := svc.Create(context.Background(), SaveInput{
		Name:     "anytls",
		Enabled:  true,
		SpecYAML: "name: anytls\nimage: jiasongji/anytls\nnetworkMode: host\n",
	})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_runtime_operation_failed" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(appErr.Message, "invalid runtime config") {
		t.Fatalf("message = %q", appErr.Message)
	}
}

func TestCreateEnabledAppContinuesDeployingRemainingTargetsAfterRuntimeError(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	runtime.deployErrByServer = map[string]error{"srv-a": errors.New("agent down")}

	_, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_runtime_operation_failed" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(appErr.Message, "srv-a") || !strings.Contains(appErr.Message, "1 of 2") {
		t.Fatalf("message = %q", appErr.Message)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].ServerID != "srv-a" || runtime.deploys[1].ServerID != "srv-b" {
		t.Fatalf("deploy order = %#v", runtime.deploys)
	}
}

func TestCreateEnabledAppFailsTargetWhenContainerExitsAfterStart(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	var appID string
	runtime.statuses = map[string]appruntime.InstanceStatus{}
	runtimeStatus := appruntime.InstanceStatus{
		Status:     appruntime.StatusStopped,
		LastError:  "container exited with code 1",
		ObservedAt: time.Now().UTC(),
	}
	runtime.statusHook = func(instanceID string) {
		if strings.HasSuffix(instanceID, "-srv-a") {
			runtime.statuses[instanceID] = runtimeStatus
		}
	}

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err == nil {
		t.Fatal("expected deploy error")
	}
	appID = app.ID
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_runtime_operation_failed" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(appErr.Message, "container exited with code 1") || !strings.Contains(appErr.Message, "1 of 2") {
		t.Fatalf("message = %q", appErr.Message)
	}
	if err := svc.db.QueryRowContext(context.Background(), `SELECT id FROM applications WHERE name=?`, "web").Scan(&appID); err != nil {
		t.Fatal(err)
	}
	instance, err := svc.runtimeInstanceForServer(context.Background(), appID, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if instance.Status != appruntime.StatusFailed || !strings.Contains(instance.LastError, "container exited with code 1") {
		t.Fatalf("instance = %#v", instance)
	}
}

func TestCreateDuplicateAppNameReturnsValidation(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	if _, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web-copy\nimage: nginx\n"})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_name_duplicate" {
		t.Fatalf("err = %#v", err)
	}
}

func TestCreateDisabledSelectedApplicationWithCommandStoresTargets(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:    "anytls",
		Enabled: false,
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - /app/anytls-server
  - -l
  - :9443
  - -p
  - "this is password"
env:
  TZ: Asia/Shanghai
restart:
  policy: unless-stopped
`,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.DeploymentMode != DeploymentModeSelected || !reflect.DeepEqual(app.DeploymentServers, []string{"srv-a", "srv-b", "srv-c"}) {
		t.Fatalf("deployment targets = %q %#v", app.DeploymentMode, app.DeploymentServers)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("disabled create should not call agent runtime, deploys = %#v", runtime.deploys)
	}
}

func TestRedeployEnabledApplicationsDeploysEnabledApps(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.Create(ctx, SaveInput{Name: "enabled", Enabled: true, SpecYAML: "name: enabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, SaveInput{Name: "disabled", Enabled: false, SpecYAML: "name: disabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	runtime.deploys = nil

	count, err := svc.RedeployEnabledApplications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("redeployed = %d, want 1", count)
	}
	if len(runtime.deploys) != 2 || runtime.deploys[0].Spec.Name != "enabled" || runtime.deploys[1].Spec.Name != "enabled" {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
}

func TestDeployTaskExecutorWithoutLifecycleTargetDoesNotMutateRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeTargetApply,
		ResourceType: "application",
		ResourceID:   app.ID,
		ServerID:     "srv-a",
		Summary:      "Syncing application " + app.Name,
		ParamsJSON:   `{"serverId":"srv-a","action":"apply"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected application target apply executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy task, got %#v", storedTask)
	}
}

func TestPlanApplicationDeploymentSkipsSatisfiedTargetsUnlessForced(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.markRuntimeInstance(ctx, app.ID+"-srv-b", appruntime.DesiredRunning, appruntime.StatusFailed, "", "boom"); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, TriggerType: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CreatedTargets) != 1 || plan.CreatedTargets[0].ServerID != "srv-b" {
		t.Fatalf("expected only failed target to reconcile, got %#v", plan)
	}
	plan, err = svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CreatedTargets) != 1 || plan.CreatedTargets[0].ServerID != "srv-a" {
		t.Fatalf("forced redeploy should include satisfied targets but not duplicate active targets, got %#v", plan)
	}
}

func TestPlanApplicationDeploymentDoesNotTreatLegacyActiveTaskAsAuthority(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeTargetApply,
		ResourceType: "application",
		ResourceID:   app.ID,
		ServerID:     "srv-a",
		Status:       tasks.StatusRunning,
		ParamsJSON:   `{"appId":"` + app.ID + `","serverId":"srv-a","action":"apply"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.tasks.FinishExecution(active.ID)

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CreatedTargets) != 2 {
		t.Fatalf("legacy active tasks must not block durable target planning, got %#v", plan)
	}
}

func TestDeployTaskSupersedesStaleDesiredRevision(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx:1\n"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx:2\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation == app.Generation {
		t.Fatal("expected application generation to change")
	}
	runtime.deploys = nil
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, plan.CreatedTargets[0], "Syncing application web", "system"))
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("stale task should not deploy runtime, got %#v", runtime.deploys)
	}
	targets, err := svc.lifecycleTargets(ctx, plan.CreatedTargets[0].OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Status != LifecycleTargetStatusSuperseded {
		t.Fatalf("expected superseded lifecycle target, got %#v", targets)
	}
}

func TestDeploymentDispatcherMarksTaskCreateFailure(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	createErr := errors.New("task insert failed")
	dispatcher := NewDeploymentDispatcher(svc).(*deploymentDispatcher)
	if err := dispatcher.markTargetTaskCreateFailed(ctx, plan.CreatedTargets[0].ID, createErr); err != nil {
		t.Fatal(err)
	}
	targets, err := svc.lifecycleTargets(ctx, plan.CreatedTargets[0].OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].State != LifecycleTargetStateFailedRetryable || !strings.Contains(targets[0].Error, createErr.Error()) {
		t.Fatalf("expected retryable task-create failure target, got %#v", targets)
	}
	if targets[0].ErrorCode != "task_create_failed" {
		t.Fatalf("expected structured task_create_failed error, got %#v", targets[0])
	}
}

func TestPurgeRuntimeInstanceTreatsAlreadyRequestedStateAsSuccess(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.stopErr = errors.New("docker resource already has requested state: /containers/panel-web/stop?t=10")
	if err := svc.purgeRuntimeInstanceForServer(ctx, "task-1", app.ID, "srv-a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.runtimeInstanceForServer(ctx, app.ID, "srv-a"); !isNotFound(err) {
		t.Fatalf("expected runtime instance to be removed, got err=%v", err)
	}
}

func TestDeployReturnsBeforeRuntimeDeploymentCompletes(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.createEntered = make(chan struct{})
	runtime.createRelease = make(chan struct{})
	done := make(chan OperationResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := svc.Deploy(ctx, app.ID)
		if err != nil {
			errs <- err
			return
		}
		done <- result
	}()
	var result OperationResult
	select {
	case err := <-errs:
		t.Fatal(err)
	case result = <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Deploy waited for runtime deployment to complete")
	}
	if result.TaskID == "" || !result.Application.Enabled {
		t.Fatalf("deploy result = %#v", result)
	}
	select {
	case <-runtime.createEntered:
	case <-time.After(time.Second):
		t.Fatal("background deployment did not reach runtime create")
	}
	close(runtime.createRelease)
	waitApplicationTaskStatus(t, svc.tasks, result.TaskID, tasks.StatusCompleted)
}

func TestRefreshTaskExecutorRedeploysChangedApplicationAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{"message": "one"})

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\nenv:\n  MESSAGE: '{{ .message }}'\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{"message": "two"})
	runtime.deploys = nil
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeRefresh,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Refreshing application " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeRefresh)
	if !ok || def.Execute == nil {
		t.Fatal("expected application refresh executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].Spec.Env["MESSAGE"] != "two" || runtime.deploys[1].Spec.Env["MESSAGE"] != "two" {
		t.Fatalf("runtime env = %#v", runtime.deploys)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed refresh task, got %#v", storedTask)
	}
	refreshed, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Generation <= app.Generation || refreshed.SpecHash == app.SpecHash {
		t.Fatalf("expected refreshed snapshot, before=%#v after=%#v", app, refreshed)
	}
}

func TestTemplateVariablesRenderIntoRuntimeSpec(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	registry := NewApplicationVariableRegistry()
	registry.Register("certs", fakeVariableSource{value: map[string]any{
		"example_com": map[string]any{
			"certificatePem": "CERT",
		},
	}})
	svc.SetBuiltinVariableResolver(registry)

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		SpecYAML: "name: web\nimage: 'nginx:1.27'\nenv:\n  APP_NAME: '{{ .app.name }}'\n  TLS_CERT: '{{ .certs.example_com.certificatePem }}'\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, issues, err := svc.renderApplication(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) > 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if spec.Image != "nginx:1.27" || spec.Env["APP_NAME"] != "web" || spec.Env["TLS_CERT"] != "CERT" {
		t.Fatalf("rendered spec = %#v", spec)
	}
}

func TestApplicationFileMountCreatesManagedRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/app.conf\n    target: /etc/app.conf\n    uid: 1000\n    gid: 1001\n    mode: \"0755\"\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Name:          "config/app.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("server={{ .app.name }}")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: app.SpecYAML,
	}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	spec := runtime.deploys[0].Spec
	allocation := applicationFileAllocationName(file.ID)
	if len(spec.Files) != 1 || spec.Files[0].Path != allocation || string(spec.Files[0].Content) != "server=web" {
		t.Fatalf("files = %#v", spec.Files)
	}
	if spec.Files[0].Mode != "0755" || spec.Files[0].UID == nil || *spec.Files[0].UID != 1000 || spec.Files[0].GID == nil || *spec.Files[0].GID != 1001 {
		t.Fatalf("file permissions = %#v", spec.Files[0])
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Type != "managed_file" || spec.Mounts[0].Source != allocation || spec.Mounts[0].Target != "/etc/app.conf" {
		t.Fatalf("mounts = %#v", spec.Mounts)
	}
}

func TestApplicationArchiveFileMountDeploysSingleManagedArchive(t *testing.T) {
	svc, runtimeClient, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	archive := testApplicationZipArchive(t, "index.html", "<h1>ok</h1>")
	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{Save: SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: public\n    target: /usr/share/nginx/html\n    readOnly: true\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := svc.UploadSaveSessionArchive(ctx, session.ID, FileArchiveInput{
		Name:     "public",
		Kind:     "binary",
		FileName: "site.zip",
		Content:  archive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitSaveSession(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	if len(runtimeClient.deploys) == 0 {
		t.Fatal("expected deployment")
	}
	spec := runtimeClient.deploys[0].Spec
	allocation := applicationFileAllocationName(files[0].ID)
	if len(spec.Files) != 1 || spec.Files[0].Kind != appruntime.ManagedFileKindArchive || spec.Files[0].Path != allocation || !bytes.Equal(spec.Files[0].Content, archive) {
		t.Fatalf("files = %#v", spec.Files)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Type != "managed_file" || spec.Mounts[0].Source != allocation || spec.Mounts[0].Target != "/usr/share/nginx/html" || !spec.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %#v", spec.Mounts)
	}
}

func TestApplicationFileTemplateRendersPerTargetServerVariables(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/node.conf\n    target: /etc/node.conf\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/node.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("name={{ .server.name }} role={{ index .server.variables \"role\" }} app={{ .app.name }}")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, SaveInput{
		Name:     app.Name,
		Enabled:  true,
		SpecYAML: app.SpecYAML,
	}); err != nil {
		t.Fatal(err)
	}

	byServer := map[string]string{}
	for _, deploy := range runtime.deploys {
		if len(deploy.Spec.Files) != 1 {
			t.Fatalf("files = %#v", deploy.Spec.Files)
		}
		byServer[deploy.ServerID] = string(deploy.Spec.Files[0].Content)
	}
	if byServer["srv-a"] != "name=srv-a role=srv-a-role app=web" || byServer["srv-b"] != "name=srv-b role=srv-b-role app=web" {
		t.Fatalf("rendered files by server = %#v", byServer)
	}
}

func TestPanelFileMountCreatesReadOnlyRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	svc.SetInternalFileProvider(fakeInternalFileProvider{content: []byte("CERTIFICATE")})

	if _, err := svc.Create(context.Background(), SaveInput{
		Name:     "tls",
		Enabled:  true,
		SpecYAML: "name: tls\nimage: nginx\nmounts:\n  - type: panel_file\n    source: key_asset:cert_1:certificate\n    target: /etc/tls/cert.pem\n    readOnly: true\n    uid: 1000\n    gid: 1001\n",
	}); err != nil {
		t.Fatal(err)
	}
	spec := runtime.deploys[0].Spec
	if len(spec.Files) != 1 || string(spec.Files[0].Content) != "CERTIFICATE" || spec.Files[0].Mode != "0644" {
		t.Fatalf("files = %#v", spec.Files)
	}
	if spec.Files[0].UID == nil || *spec.Files[0].UID != 1000 || spec.Files[0].GID == nil || *spec.Files[0].GID != 1001 {
		t.Fatalf("file ownership = %#v", spec.Files[0])
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Target != "/etc/tls/cert.pem" || !spec.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %#v", spec.Mounts)
	}
}

func TestSelectedDeploymentDeploysOneInstancePerSelectedServer(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	if _, err := svc.Create(context.Background(), SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].ServerID != "srv-a" || runtime.deploys[1].ServerID != "srv-b" {
		t.Fatalf("deploy order = %#v", runtime.deploys)
	}
}

func TestPersistentDeploymentRequiresExactlyOneServer(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()

	_, err := svc.Create(context.Background(), SaveInput{
		Name:           "db",
		Enabled:        true,
		SpecYAML:       "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode: DeploymentModeAll,
	})
	if err == nil || !strings.Contains(err.Error(), "persistent applications must target exactly one server") {
		t.Fatalf("expected persistent target validation, got %v", err)
	}
}

func TestPersistentPathIsDerivedAfterUpdate(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           false,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.PersistentPath == "" {
		t.Fatal("expected managed persistent path")
	}
	updated, err := svc.Update(ctx, app.ID, SaveInput{
		Name:              app.Name,
		Enabled:           app.Enabled,
		SpecYAML:          app.SpecYAML,
		DeploymentMode:    app.DeploymentMode,
		DeploymentServers: app.DeploymentServers,
		ReverseProxy:      app.ReverseProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PersistentPath != app.PersistentPath {
		t.Fatalf("persistent path = %q, want %q", updated.PersistentPath, app.PersistentPath)
	}
}

func TestStopAppCallsRuntimeAndDisablesApp(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Enabled {
		t.Fatalf("app should be disabled: %#v", stopped)
	}
	if len(runtime.stops) != 2 || runtime.stops[0].InstanceID != app.ID+"-srv-a" || runtime.stops[1].InstanceID != app.ID+"-srv-b" {
		t.Fatalf("stops = %#v", runtime.stops)
	}
	for _, req := range runtime.stops {
		if req.ApplicationID != app.ID || req.Purge || req.RemoveApplicationData {
			t.Fatalf("stop should remove container without purging files: %#v", req)
		}
	}
	result, err := svc.tasks.List(ctx, tasks.ListFilter{Type: TaskTypeTargetStop, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	foundStop := false
	for _, item := range result.Items {
		if strings.Contains(item.ParamsJSON, `"action":"stop"`) && !strings.Contains(item.ParamsJSON, `"purge":true`) {
			foundStop = true
			break
		}
	}
	if !foundStop {
		t.Fatalf("expected target stop task, got %#v", result.Items)
	}
}

func TestStopAppWrapsReverseProxyReconcileFailure(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetReverseProxyReconciler(fakeReverseProxyReconciler{err: errors.New("create container failed: Conflict")})

	_, err = svc.Stop(ctx, app.ID, false)
	if err == nil {
		t.Fatal("expected reverse proxy reconcile error")
	}
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected panel error, got %T %v", err, err)
	}
	if appErr.Code != "application_runtime_operation_failed" || appErr.HTTPStatus != 502 || !strings.Contains(appErr.Message, "create container failed: Conflict") {
		t.Fatalf("unexpected wrapped error: %#v", appErr)
	}
}

func TestStopTaskExecutorUsesPurgeParamAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeStop,
		ResourceType: "application",
		ResourceID:   app.ID,
		ParamsJSON:   `{"purge":true}`,
		Summary:      "Stopping application " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeStop)
	if !ok || def.Execute == nil {
		t.Fatal("expected application stop executor")
	}
	before := len(runtime.stops)
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.stops) != before+2 {
		t.Fatalf("expected two stop calls, got %d before=%d", len(runtime.stops), before)
	}
	for _, req := range runtime.stops[before:] {
		if !req.Purge {
			t.Fatalf("expected purge stop request, got %#v", req)
		}
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed stop task, got %#v", storedTask)
	}
}

func TestSelectedDeploymentRemovalPurgesRemovedInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a", "srv-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeStops := len(runtime.stops)
	updated, err := svc.Update(ctx, app.ID, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.DeploymentServers) != 1 || updated.DeploymentServers[0] != "srv-b" {
		t.Fatalf("deployment servers = %#v", updated.DeploymentServers)
	}
	if len(runtime.stops) != beforeStops+1 {
		t.Fatalf("stops = %#v", runtime.stops)
	}
	cleanup := runtime.stops[len(runtime.stops)-1]
	if cleanup.InstanceID != app.ID+"-srv-a" || !cleanup.Purge || cleanup.RemoveApplicationData {
		t.Fatalf("removed selected target cleanup = %#v", cleanup)
	}
	instances, err := svc.runtimeInstances(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ServerID != "srv-b" {
		t.Fatalf("instances = %#v", instances)
	}
}

func TestDeleteApplicationPurgesRuntimeData(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	beforeDeleteStops := len(runtime.stops)
	if err := svc.Delete(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	if len(runtime.stops) != beforeDeleteStops+2 {
		t.Fatalf("delete cleanup stops = %#v", runtime.stops)
	}
	for _, req := range runtime.stops[beforeDeleteStops:] {
		if req.ApplicationID != app.ID || !req.Purge || !req.RemoveApplicationData {
			t.Fatalf("delete should purge application runtime data: %#v", req)
		}
	}
	if _, err := svc.Get(ctx, app.ID); err == nil {
		t.Fatal("expected application to be deleted")
	}
}

func TestRuntimeRefreshesInstanceStatuses(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	instanceID := app.ID + "-srv-a"
	runtime.statuses = map[string]appruntime.InstanceStatus{
		instanceID: {InstanceID: instanceID, ContainerName: "panel-web", Status: appruntime.StatusRunning, Image: "nginx", ObservedAt: time.Now().UTC()},
	}

	result, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != appruntime.StatusRunning || len(result.Instances) != 2 {
		t.Fatalf("runtime = %#v", result)
	}
	found := false
	for _, inst := range result.Instances {
		if inst.InstanceID == instanceID && inst.ContainerName == "panel-web" && inst.Image == "nginx" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime instances = %#v", result.Instances)
	}
}

func TestRuntimeShowsSelectedTargetThatFailsBeforeInstanceDeploy(t *testing.T) {
	svc, _, servers, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	srvC := readyServer("srv-c")
	srvC.Traits[agentcontract.TraitStatus] = agentcontract.StatusUndeployable
	servers.items[srvC.ID] = srvC
	insertApplicationTestServer(t, svc, srvC)

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           false,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a", "srv-b", "srv-c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deploy(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	var runtime Runtime
	for i := 0; i < 20; i++ {
		runtime, err = svc.Runtime(ctx, app.ID)
		if err == nil && len(runtime.Instances) == 3 {
			for _, instance := range runtime.Instances {
				if instance.ServerID == "srv-c" && instance.Status == appruntime.StatusFailed {
					goto runtimeReady
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
runtimeReady:
	if len(runtime.Instances) == 0 {
		runtime, err = svc.Runtime(ctx, app.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Instances) != 3 {
		t.Fatalf("runtime instances = %#v", runtime.Instances)
	}
	var failed appruntime.InstanceStatus
	for _, instance := range runtime.Instances {
		if instance.ServerID == "srv-c" {
			failed = instance
			break
		}
	}
	if failed.ServerID != "srv-c" || failed.Status != appruntime.StatusFailed || failed.LastError == "" {
		t.Fatalf("failed target not represented: %#v", runtime.Instances)
	}
	if runtime.Operation == nil || len(runtime.Operation.Targets) != 3 || runtime.Operation.Status != LifecycleStatusDeploying {
		t.Fatalf("operation = %#v", runtime.Operation)
	}
	var failedTarget LifecycleTarget
	for _, target := range runtime.Operation.Targets {
		if target.ServerID == "srv-c" {
			failedTarget = target
			break
		}
	}
	if failedTarget.ServerID != "srv-c" || failedTarget.State != LifecycleTargetStateFailedRetryable || failedTarget.NextRunAt == nil {
		t.Fatalf("retryable failed target not represented: %#v", runtime.Operation.Targets)
	}
}

func TestReconcileInterruptedLifecycleTasksMarksDeployingTargetFailed(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{
		ApplicationID: app.ID,
		InstanceID:    app.ID + "-srv-a",
		ContainerName: "panel-web",
		Generation:    app.Generation,
		SpecHash:      app.SpecHash,
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, spec, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if err := svc.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{
		Status:        LifecycleTargetStatusDeploying,
		Stage:         "start_container",
		InstanceID:    spec.InstanceID,
		ContainerName: spec.ContainerName,
		ContainerID:   "container-srv-a",
		Started:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", spec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(deployTaskParams{
		AppID:                app.ID,
		ServerID:             "srv-a",
		LifecycleOperationID: operation.ID,
		Action:               "apply",
	})
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeTargetApply,
		ServerID:     "srv-a",
		ResourceType: "application",
		ResourceID:   app.ID,
		ParamsJSON:   string(params),
		Status:       tasks.StatusRunning,
		Summary:      "Syncing application web",
	})
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("Task was marked running but no active execution exists in this Panel process")
	if err := svc.tasks.Fail(ctx, task.ID, interrupted); err != nil {
		t.Fatal(err)
	}

	if err := svc.ReconcileInterruptedLifecycleTasks(ctx); err != nil {
		t.Fatal(err)
	}

	targets, err := svc.lifecycleTargets(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Status != LifecycleTargetStatusFailed || !strings.Contains(targets[0].Error, interrupted.Error()) || targets[0].FinishedAt == nil {
		t.Fatalf("target = %#v", targets)
	}
	finished, err := svc.latestLifecycleOperation(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != LifecycleStatusFailed || !strings.Contains(finished.Error, interrupted.Error()) {
		t.Fatalf("operation = %#v", finished)
	}
	instances, err := svc.runtimeInstances(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].Status != appruntime.StatusFailed || !strings.Contains(instances[0].LastError, interrupted.Error()) {
		t.Fatalf("instances = %#v", instances)
	}
}

func TestListWithRuntimeUsesCachedRuntimeStatus(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.statuses = map[string]appruntime.InstanceStatus{
		app.ID + "-srv-a": {InstanceID: app.ID + "-srv-a", ContainerName: "panel-web", Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()},
		app.ID + "-srv-b": {InstanceID: app.ID + "-srv-b", ContainerName: "panel-web", Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()},
	}
	statusCalls := 0
	runtime.statusHook = func(string) {
		statusCalls++
	}

	apps, err := svc.ListWithRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].RuntimeStatus != appruntime.StatusRunning {
		t.Fatalf("apps = %#v", apps)
	}
	if statusCalls != 0 {
		t.Fatalf("list runtime summary made %d remote status calls", statusCalls)
	}
}

func TestAggregateRuntimeStatusReportsMissingInstance(t *testing.T) {
	status := aggregateRuntimeStatus(true, []appruntime.InstanceStatus{
		{Status: appruntime.StatusRunning},
		{Status: appruntime.StatusMissing},
	})
	if status != appruntime.StatusMissing {
		t.Fatalf("status = %q, want %q", status, appruntime.StatusMissing)
	}
}

func TestLogsReadFromRuntimeInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.logs = "hello\n"
	result, err := svc.Logs(ctx, app.ID, LogInput{InstanceID: app.ID + "-srv-a", Tail: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.InstanceID != app.ID+"-srv-a" || result.ContainerName == "" || result.Logs != "hello\n" {
		t.Fatalf("logs = %#v", result)
	}
	if runtime.logTail != 20 {
		t.Fatalf("tail = %d", runtime.logTail)
	}
}

func TestPersistentDataDownloadsFromRuntimeInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           true,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.archiveContent = []byte("zip")
	result, err := svc.PersistentData(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != "zip" {
		t.Fatalf("content = %q", result.Content)
	}
	if runtime.archiveApplicationID != app.ID {
		t.Fatalf("archive application = %q, want %q", runtime.archiveApplicationID, app.ID)
	}
	if runtime.archiveBaseURL != "https://srv-a.agent" {
		t.Fatalf("archive baseURL = %q", runtime.archiveBaseURL)
	}
}

func TestPersistentDataRejectsApplicationWithoutPersistentStorage(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PersistentData(ctx, app.ID); err == nil {
		t.Fatal("expected persistent data download to be rejected")
	}
}

func TestImageCheckTaskExecutorUpdatesApplicationAndCompletesTask(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{
		Reference: "registry.example.com/team/web:latest",
		Digest:    "sha256:latest",
	}}

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: registry.example.com/team/web:latest\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeImageCheck,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Checking image for " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeImageCheck)
	if !ok || def.Execute == nil {
		t.Fatal("expected application image check executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageReference != "registry.example.com/team/web:latest" || got.ImageLatestDigest != "sha256:latest" || got.ImageDigest != "sha256:latest" || got.ImageLastError != "" {
		t.Fatalf("unexpected image state: %#v", got)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed image check task, got %#v", storedTask)
	}
}

func TestImageUpdateTaskExecutorUpdatesImageDeploysRuntimeAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{
		Reference: "registry.example.com/team/web:latest",
		Digest:    "sha256:latest",
	}}

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: registry.example.com/team/web:latest\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.deploys = nil
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeImageUpdate,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Updating image for " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeImageUpdate)
	if !ok || def.Execute == nil {
		t.Fatal("expected application image update executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageReference != "registry.example.com/team/web:latest" || got.ImageDigest != "sha256:latest" || got.ImageLatestDigest != "sha256:latest" || got.ImageUpdateAvailable || got.ImageLastError != "" {
		t.Fatalf("unexpected image state: %#v", got)
	}
	if got.Generation != app.Generation+1 {
		t.Fatalf("generation = %d, want %d", got.Generation, app.Generation+1)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed image update task, got %#v", storedTask)
	}
}

func TestGetAggregatesImageUpdatesFromRuntimeInstances(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?), (?,?,?,?,?,?,?)`,
		"srv-a", "nginx:latest", "sha256:old", "sha256:new", 1, "", now,
		"srv-b", "nginx:latest", "sha256:new", "sha256:new", 0, "", now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ImageUpdateAvailable {
		t.Fatalf("expected app update to be available: %#v", got)
	}
	if len(got.ImageUpdateTargets) != 2 {
		t.Fatalf("targets = %#v", got.ImageUpdateTargets)
	}
	if got.ImageUpdateTargets[0].ServerID != "srv-a" || !got.ImageUpdateTargets[0].UpdateAvailable || got.ImageUpdateTargets[0].LatestDigest != "sha256:new" {
		t.Fatalf("first target = %#v", got.ImageUpdateTargets[0])
	}
	if got.ImageUpdateTargets[1].ServerID != "srv-b" || got.ImageUpdateTargets[1].UpdateAvailable {
		t.Fatalf("second target = %#v", got.ImageUpdateTargets[1])
	}
}

func TestUpdateImageMarksNodeImageCacheCurrent(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{Reference: "nginx", Digest: "sha256:new"}}
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?)`,
		"srv-a", "nginx:latest", "sha256:old", "sha256:new", 1, "", now); err != nil {
		t.Fatal(err)
	}
	result, err := svc.UpdateImage(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Application.ImageUpdateAvailable {
		t.Fatalf("expected returned app to be current: %#v", result.Application.ImageUpdateTargets)
	}
	var available int
	var localDigest string
	if err := svc.db.QueryRowContext(ctx, `SELECT local_digest,update_available FROM image_updates WHERE server_id=? AND reference=?`, "srv-a", "nginx:latest").Scan(&localDigest, &available); err != nil {
		t.Fatal(err)
	}
	if localDigest != "sha256:new" || available != 0 {
		t.Fatalf("cache local=%q available=%d", localDigest, available)
	}
}

func TestRestartTaskExecutorPlansForcedDeploymentAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeRestart,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Restarting application",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeRestart)
	if !ok || def.Execute == nil {
		t.Fatal("expected application restart executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("restart task should plan lifecycle work instead of direct runtime restart, got %d restart calls", len(runtime.restarts))
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed restart task, got %#v", storedTask)
	}
	var targetCount int
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM application_lifecycle_targets WHERE application_id=? AND state IN ('ready','claimed','preparing','applying','verifying','failed_retryable','succeeded')`, app.ID).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if targetCount == 0 {
		t.Fatal("expected restart task to create lifecycle deployment target")
	}
}

func TestRestorePersistentDataRestoresAndPlansForcedDeployment(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           true,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RestorePersistentData(ctx, app.ID, []byte("zip"))
	if err != nil {
		t.Fatal(err)
	}
	if result.DeploymentID == "" {
		t.Fatal("expected deployment operation id")
	}
	if runtime.restoreApplicationID != app.ID || string(runtime.restoreContent) != "zip" {
		t.Fatalf("restore application=%q content=%q", runtime.restoreApplicationID, runtime.restoreContent)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("persistent restore should plan lifecycle work instead of direct runtime restart, got %d restart calls", len(runtime.restarts))
	}
}

func TestRestorePersistentDataImportsBeforeFirstDeploy(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           false,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RestorePersistentData(ctx, app.ID, []byte("zip"))
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID != "" {
		t.Fatalf("expected no restart task before first deploy, got %q", result.TaskID)
	}
	if result.Application.ID != app.ID {
		t.Fatalf("expected restored application %q, got %q", app.ID, result.Application.ID)
	}
	if runtime.restoreBaseURL != "https://srv-a.agent" || runtime.restoreApplicationID != app.ID || string(runtime.restoreContent) != "zip" {
		t.Fatalf("restore baseURL=%q application=%q content=%q", runtime.restoreBaseURL, runtime.restoreApplicationID, runtime.restoreContent)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("expected no restart before first deploy, got %d", len(runtime.restarts))
	}
}

func TestMigrateDeploysTargetAndDropsSourceInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Migrate(ctx, app.ID, MigrationInput{SourceServerID: "srv-a", TargetServerID: "srv-b"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatal("expected migration task id")
	}
	migrated, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.DeploymentMode != DeploymentModeSelected || len(migrated.DeploymentServers) != 1 || migrated.DeploymentServers[0] != "srv-b" {
		t.Fatalf("deployment target = %q %#v", migrated.DeploymentMode, migrated.DeploymentServers)
	}
	instances, err := svc.runtimeInstances(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ServerID != "srv-b" {
		t.Fatalf("instances = %#v", instances)
	}
	if len(runtime.stops) != 1 {
		t.Fatalf("source should be cleaned during migration: %#v", runtime.stops)
	}
	if runtime.stops[0].InstanceID != app.ID+"-srv-a" || !runtime.stops[0].Purge || runtime.stops[0].RemoveApplicationData {
		t.Fatalf("source migration cleanup = %#v", runtime.stops[0])
	}
	lastDeploy := runtime.deploys[len(runtime.deploys)-1]
	if lastDeploy.ServerID != "srv-b" {
		t.Fatalf("last deploy server = %q", lastDeploy.ServerID)
	}
}

func TestMigrateRejectsExternalMounts(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\nvolumes:\n  - source: web-data\n    target: /data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Migrate(ctx, app.ID, MigrationInput{SourceServerID: "srv-a", TargetServerID: "srv-b"}); err == nil || !strings.Contains(err.Error(), "host paths or Docker volumes") {
		t.Fatalf("expected migration mount validation, got %v", err)
	}
}

func TestDecorateDeploymentTasksProjectsLifecycleDiagnostics(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a", "srv-b"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	params, err := json.Marshal(deployTaskParams{
		AppID:                app.ID,
		ServerID:             "srv-a",
		LifecycleOperationID: operation.ID,
		LifecycleTargetID:    targetID,
		Generation:           app.Generation,
		SpecHash:             app.SpecHash,
		Action:               LifecycleTargetActionApply,
	})
	if err != nil {
		t.Fatal(err)
	}
	taskNextRunAt := time.Now().UTC().Add(5 * time.Minute)
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		OperationID:   "task-op-1",
		Type:          TaskTypeTargetApply,
		ServerID:      "srv-a",
		ResourceType:  "application",
		ResourceID:    app.ID,
		ParamsJSON:    string(params),
		Status:        tasks.StatusFailedRetryable,
		RetryCount:    2,
		MaxRetries:    5,
		NextRunAt:     &taskNextRunAt,
		ExecutionMode: tasks.ExecutionModeSerial,
	})
	if err != nil {
		t.Fatal(err)
	}
	nextRunAt := time.Now().UTC().Add(3 * time.Minute)
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,stage=?,attempt=?,next_run_at=?,claimed_task_id=?,error=?,error_code=?,error_message=?,error_detail=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateFailedRetryable,
		lifecycleStatusForState(LifecycleTargetStateFailedRetryable),
		"runtime_deploy",
		3,
		formatTime(nextRunAt),
		task.ID,
		"docker create failed",
		"docker_create_failed",
		"create container failed",
		"agent stderr: invalid mount",
		formatTime(time.Now().UTC()),
		targetID,
	); err != nil {
		t.Fatal(err)
	}

	items := []tasks.Task{task, {ID: "parent", OperationID: "task-op-1", Type: TaskTypeTargetBatch, Status: tasks.StatusRunning, CreatedAt: task.CreatedAt}}
	if err := svc.DecorateDeploymentTasks(ctx, items); err != nil {
		t.Fatal(err)
	}
	if items[0].Deployment == nil || items[0].Deployment.Operation == nil || items[0].Deployment.Target == nil {
		t.Fatalf("expected target task projection, got %#v", items[0].Deployment)
	}
	target := items[0].Deployment.Target
	if target.State != LifecycleTargetStateFailedRetryable || target.Stage != "runtime_deploy" || target.Attempt != 3 {
		t.Fatalf("target diagnostics = %#v", target)
	}
	if target.ClaimedTaskID != task.ID || target.ClaimedTaskStatus != tasks.StatusFailedRetryable {
		t.Fatalf("claimed task projection = %#v", target)
	}
	if target.ErrorCode != "docker_create_failed" || target.ErrorMessage != "create container failed" || !strings.Contains(target.ErrorDetail, "invalid mount") {
		t.Fatalf("target error projection = %#v", target)
	}
	if target.NextRunAt == nil {
		t.Fatalf("expected target next retry time: %#v", target)
	}
	if len(items[0].Deployment.Operation.Targets) != 2 {
		t.Fatalf("operation should include all targets, got %#v", items[0].Deployment.Operation.Targets)
	}
	if items[1].Deployment == nil || items[1].Deployment.Operation == nil || items[1].Deployment.Operation.ID != operation.ID {
		t.Fatalf("expected parent task to inherit operation projection, got %#v", items[1].Deployment)
	}
}

func newTestService(t *testing.T) (*Service, *fakeRuntimeClient, *fakeServerProvider, func()) {
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
	runtime := &fakeRuntimeClient{}
	servers := &fakeServerProvider{items: map[string]server.Server{
		"srv-a": readyServer("srv-a"),
		"srv-b": readyServer("srv-b"),
	}}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"cred_1", "test", "password", "root", time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, srv := range servers.items {
		traits, _ := json.Marshal(srv.Traits)
		variables, _ := json.Marshal(srv.Variables)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred_1", srv.DockerHost, string(traits), string(variables), now, now); err != nil {
			t.Fatal(err)
		}
	}
	taskSvc := tasks.NewService(store.LogDB())
	svc := NewServiceWithOptions(store.AppDB(), runtime, taskSvc, Config{
		Namespace:      "apps",
		Region:         "global",
		Datacenter:     "dc1",
		SaveSessionDir: filepath.Join(dir, "sessions"),
	}, WithLogDB(store.LogDB()))
	svc.RegisterTasks(taskSvc)
	svc.SetServerProvider(servers)
	svc.SetApplicationReconcileTrigger(&fakeApplicationReconcileTrigger{svc: svc, tasks: taskSvc})
	return svc, runtime, servers, func() { _ = store.Close() }
}

type fakeApplicationReconcileTrigger struct {
	svc   *Service
	tasks *tasks.Service
}

func (f *fakeApplicationReconcileTrigger) TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	payload := map[string]any{}
	if raw, ok := trigger.Payload.(map[string]any); ok {
		payload = raw
	}
	appIDs := stringSlicePayload(payload["applicationIds"])
	if len(appIDs) == 0 && strings.TrimSpace(trigger.TriggerResourceID) != "" {
		appIDs = []string{strings.TrimSpace(trigger.TriggerResourceID)}
	}
	purge, _ := payload["purge"].(bool)
	force, _ := payload["force"].(bool)
	stopServers := stringSlicePayload(payload["stopServers"])
	inputs := []tasks.CreateInput{}
	for _, appID := range appIDs {
		plan, err := f.svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
			ApplicationID: appID,
			StopServers:   stopServers,
			Purge:         purge,
			Force:         force,
			Manual:        trigger.Manual,
			TriggerType:   firstNonEmpty(trigger.Type, "test"),
		})
		if err != nil {
			return tasks.Task{}, false, err
		}
		removeApplicationData := false
		if app, err := f.svc.Get(ctx, appID); err == nil {
			removeApplicationData = app.DeletionRequested
		}
		for _, target := range plan.CreatedTargets {
			input, err := targetTaskInputWithRemoveData(target, "Syncing application "+appID, firstNonEmpty(trigger.Type, "test"), removeApplicationData)
			if err != nil {
				return tasks.Task{}, false, err
			}
			inputs = append(inputs, input)
		}
	}
	if len(inputs) == 0 {
		return tasks.Task{}, false, nil
	}
	manager := tasks.NewManager(f.tasks)
	if trigger.Type == "application_sync" {
		batch := tasks.CreateBatchInput{
			Type:          inputs[0].Type,
			OperationID:   "test-reconcile",
			TriggerType:   firstNonEmpty(trigger.Type, "test"),
			Summary:       "Syncing application",
			ExecutionMode: tasks.ExecutionModeSerial,
			Inputs:        inputs,
		}
		parent, _, created, err := manager.CreateBatch(ctx, batch, tasks.Trigger{Type: firstNonEmpty(trigger.Type, "test"), Manual: trigger.Manual})
		if err != nil || !created {
			return parent, created, err
		}
		go func(task tasks.Task) {
			defer f.tasks.FinishExecution(task.ID)
			_ = manager.Run(context.Background(), task)
		}(parent)
		return parent, true, nil
	}
	var first tasks.Task
	createdAny := false
	for i, input := range inputs {
		if strings.TrimSpace(input.OperationID) == "" {
			input.OperationID = "test-reconcile"
		}
		task, created, err := manager.Create(ctx, input, tasks.Trigger{Type: firstNonEmpty(trigger.Type, "test"), Manual: trigger.Manual})
		if i == 0 {
			first = task
		}
		if err != nil || !created {
			return first, createdAny, err
		}
		createdAny = true
		if err := manager.Run(ctx, task); err != nil {
			return first, true, err
		}
		var params deployTaskParams
		if strings.TrimSpace(input.ParamsJSON) != "" {
			_ = json.Unmarshal([]byte(input.ParamsJSON), &params)
		}
		if strings.TrimSpace(params.LifecycleOperationID) != "" {
			if err := f.svc.deploymentOperationError(ctx, params.LifecycleOperationID); err != nil {
				return first, true, err
			}
		}
	}
	return first, createdAny, nil
}

func stringSlicePayload(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := []string{}
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
}

func (s *Service) SetBuiltinVariableResolver(resolver BuiltinVariableResolver) {
	s.builtinResolver = resolver
}

func (s *Service) SetInternalFileProvider(provider InternalFileProvider) {
	s.internalFiles = provider
}

func (s *Service) SetServerProvider(provider ServerProvider) {
	s.servers = provider
	if handler, ok := provider.(AgentErrorHandler); ok {
		s.agentErrors = handler
	}
}

func readyServer(id string) server.Server {
	return server.Server{
		ID:          id,
		Name:        id,
		Host:        "127.0.0.1",
		Port:        22,
		SSHUsername: "root",
		DockerHost:  agentcontract.DefaultDockerHost,
		Traits: map[string]string{
			agentcontract.TraitEnabled: "true",
			agentcontract.TraitURL:     "https://" + id + ".agent",
			agentcontract.TraitStatus:  agentcontract.StatusCompatible,
		},
		Variables: map[string]string{
			"role": id + "-role",
		},
	}
}

func TestNormalizeReverseProxyRulesTargetType(t *testing.T) {
	rules, err := normalizeReverseProxyRules([]ReverseProxyRule{
		{Domain: "local.example.test", TargetPort: 8080, OriginServerIDs: []string{"srv-a"}, AnyAccess: AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin}, Paths: []ReverseProxyPath{{Path: "/"}}},
		{Domain: "container.example.test", TargetType: ReverseProxyTargetContainer, TargetPort: 80, OriginServerIDs: []string{"srv-a"}, AnyAccess: AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin}, Paths: []ReverseProxyPath{{Path: "/app"}}},
	})
	if err != nil {
		t.Fatalf("normalize reverse proxy: %v", err)
	}
	if rules[0].TargetType != ReverseProxyTargetLocal {
		t.Fatalf("local target type = %q", rules[0].TargetType)
	}
	if rules[1].TargetType != ReverseProxyTargetContainer {
		t.Fatalf("container target type = %q", rules[1].TargetType)
	}
	if _, err := normalizeReverseProxyRules([]ReverseProxyRule{{Domain: "bad.example.test", TargetType: "remote", TargetPort: 80, OriginServerIDs: []string{"srv-a"}}}); err == nil {
		t.Fatal("expected invalid target type error")
	}
}

func TestRenderReverseProxyConfigDisablesCacheAndWritesAdvancedPathOptions(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	app := Application{
		ID:   "app-proxy-options",
		Name: "proxy-options",
		ReverseProxy: []ReverseProxyRule{{
			Domain:          "app.example.test",
			TargetType:      ReverseProxyTargetLocal,
			TargetPort:      8080,
			OriginServerIDs: []string{"srv-a"},
			AnyAccess:       AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin},
			Paths: []ReverseProxyPath{{
				Path: "/api",
				Options: HTTPRouteOptions{
					GzipMode:              HTTPRouteModeOff,
					ClientMaxBodySizeMB:   10,
					ConnectTimeoutSeconds: 5,
					ReadTimeoutSeconds:    60,
					SendTimeoutSeconds:    30,
					BufferingMode:         HTTPRouteModeOn,
					WebSocketMode:         HTTPRouteWebSocketAuto,
					RequestHeaders:        []HTTPHeader{{Name: "X-App-Request", Value: "proxy"}},
					ResponseHeaders:       []HTTPHeader{{Name: "X-App-Response", Value: "ready"}},
				},
			}},
		}},
	}

	_, config, err := svc.renderReverseProxyConfig(context.Background(), app, nil)
	if err != nil {
		t.Fatalf("render reverse proxy config: %v", err)
	}
	for _, want := range []string{
		"proxy_cache off;",
		"gzip off;",
		"client_max_body_size 10m;",
		"proxy_connect_timeout 5s;",
		"proxy_read_timeout 60s;",
		"proxy_send_timeout 30s;",
		"proxy_buffering on;",
		`proxy_set_header X-App-Request "proxy";`,
		"proxy_hide_header X-App-Response;",
		`add_header X-App-Response "ready" always;`,
		"proxy_set_header Upgrade $http_upgrade;",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("application proxy config missing %q:\n%s", want, config)
		}
	}
}

func insertApplicationTestServer(t *testing.T, svc *Service, srv server.Server) {
	t.Helper()
	traits, _ := json.Marshal(srv.Traits)
	variables, _ := json.Marshal(srv.Variables)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred_1", srv.DockerHost, string(traits), string(variables), now, now); err != nil {
		t.Fatal(err)
	}
}

func waitApplicationTaskStatus(t *testing.T, taskSvc *tasks.Service, taskID, status string) {
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

type fakeRuntimeClient struct {
	writes               []agentcontract.RuntimeWriteFilesRequest
	deploys              []agentcontract.RuntimeCreateContainerRequest
	pulls                []string
	deletes              []string
	actions              []string
	stops                []agentcontract.RuntimeStopRequest
	restarts             []agentcontract.RuntimeRestartRequest
	statuses             map[string]appruntime.InstanceStatus
	logs                 string
	logTail              int
	archiveBaseURL       string
	archiveApplicationID string
	archiveContent       []byte
	restoreBaseURL       string
	restoreApplicationID string
	restoreContent       []byte
	deployErr            error
	deployErrByServer    map[string]error
	stopErr              error
	deleteFailures       int
	createEntered        chan struct{}
	createRelease        chan struct{}
	createOnce           sync.Once
	writeHook            func(agentcontract.RuntimeWriteFilesRequest)
	statusHook           func(instanceID string)
}

type fakeReverseProxyReconciler struct {
	err error
}

func (f fakeReverseProxyReconciler) ReconcileReverseProxy(context.Context) error {
	return f.err
}

func (f *fakeRuntimeClient) RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error {
	if f.writeHook != nil {
		f.writeHook(req)
	}
	f.writes = append(f.writes, req)
	return nil
}

func (f *fakeRuntimeClient) RuntimeReload(context.Context, string, agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	return agentcontract.RuntimeReloadResponse{Phase: "unsupported", Error: "reload is not configured"}, nil
}

func (f *fakeRuntimeClient) DockerImagePull(ctx context.Context, baseURL, reference string) error {
	f.pulls = append(f.pulls, reference)
	return nil
}

func (f *fakeRuntimeClient) DockerContainerDelete(ctx context.Context, baseURL, id string) error {
	f.deletes = append(f.deletes, id)
	if f.deleteFailures > 0 {
		f.deleteFailures--
		return errors.New("container delete failed")
	}
	return nil
}

func (f *fakeRuntimeClient) RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	if f.createEntered != nil {
		f.createOnce.Do(func() { close(f.createEntered) })
	}
	if f.createRelease != nil {
		select {
		case <-ctx.Done():
			return agentcontract.RuntimeCreateContainerResponse{}, ctx.Err()
		case <-f.createRelease:
		}
	}
	f.deploys = append(f.deploys, req)
	if f.deployErrByServer != nil {
		if err, ok := f.deployErrByServer[req.ServerID]; ok {
			return agentcontract.RuntimeCreateContainerResponse{}, err
		}
	}
	if f.deployErr != nil {
		return agentcontract.RuntimeCreateContainerResponse{}, f.deployErr
	}
	return agentcontract.RuntimeCreateContainerResponse{ContainerID: "container-" + req.ServerID}, nil
}

func (f *fakeRuntimeClient) DockerContainerAction(ctx context.Context, baseURL, id, action string) error {
	f.actions = append(f.actions, id+":"+action)
	return nil
}

func (f *fakeRuntimeClient) RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	f.stops = append(f.stops, req)
	if f.stopErr != nil {
		return agentcontract.RuntimeInstanceResponse{}, f.stopErr
	}
	status := appruntime.StatusStopped
	if req.Purge {
		status = "purged"
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: status, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeRestart(ctx context.Context, baseURL string, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error) {
	f.restarts = append(f.restarts, req)
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error) {
	if f.statusHook != nil {
		f.statusHook(instanceID)
	}
	if f.statuses != nil {
		if status, ok := f.statuses[instanceID]; ok {
			return agentcontract.RuntimeStatusResponse{InstanceStatus: status}, nil
		}
	}
	return agentcontract.RuntimeStatusResponse{InstanceStatus: appruntime.InstanceStatus{InstanceID: instanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}}, nil
}

func (f *fakeRuntimeClient) RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (agentcontract.RuntimeLogsResponse, error) {
	f.logTail = tail
	return agentcontract.RuntimeLogsResponse{InstanceID: instanceID, Logs: f.logs}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentArchive(ctx context.Context, baseURL, applicationID string) (agentcontract.RuntimePersistentArchiveResponse, error) {
	f.archiveBaseURL = baseURL
	f.archiveApplicationID = applicationID
	return agentcontract.RuntimePersistentArchiveResponse{
		ApplicationID: applicationID,
		Filename:      applicationID + "-persistent.zip",
		ContentBase64: base64.StdEncoding.EncodeToString(f.archiveContent),
	}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentRestore(ctx context.Context, baseURL, applicationID string, content []byte) (agentcontract.RuntimePersistentRestoreResponse, error) {
	f.restoreBaseURL = baseURL
	f.restoreApplicationID = applicationID
	f.restoreContent = content
	return agentcontract.RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: true}, nil
}

type fakeServerProvider struct {
	items map[string]server.Server
}

func (f *fakeServerProvider) List(ctx context.Context) ([]server.Server, error) {
	out := make([]server.Server, 0, len(f.items))
	for _, srv := range f.items {
		out = append(out, srv)
	}
	return out, nil
}

func (f *fakeServerProvider) Get(ctx context.Context, id string) (server.Server, error) {
	return f.items[id], nil
}

type fakeBuiltinResolver map[string]any

func (f fakeBuiltinResolver) BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error) {
	return map[string]any(f), nil
}

type fakeImageResolver struct {
	result ImageDigestResult
	err    error
}

func (f fakeImageResolver) Resolve(ctx context.Context, image string) (ImageDigestResult, error) {
	return f.result, f.err
}

type fakeInternalFileProvider struct {
	content []byte
}

func (f fakeInternalFileProvider) InternalFileCatalog(ctx context.Context) ([]PanelFileDefinition, error) {
	return nil, nil
}

func (f fakeInternalFileProvider) OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, InternalFileInfo, error) {
	content := append([]byte(nil), f.content...)
	return io.NopCloser(bytes.NewReader(content)), InternalFileInfo{Mode: "0644", Size: int64(len(content))}, nil
}
