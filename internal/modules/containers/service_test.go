package containerization

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

func TestManagedLabelsOnlyAcceptsNewApplicationLabels(t *testing.T) {
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
		t.Fatal("legacy labels must not be recognized")
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

func TestContainerActionRunsSynchronouslyAndStartsRefreshTask(t *testing.T) {
	svc, taskSvc, fakeAgent, store := newContainerizationTestService(t)
	result, err := svc.ContainerAction(context.Background(), "server-1", "container-1", "restart")
	if err != nil {
		t.Fatalf("container action: %v", err)
	}
	if result.RefreshTaskID == "" {
		t.Fatal("expected refresh task id")
	}
	fakeAgent.mu.Lock()
	actions := append([]string(nil), fakeAgent.actions...)
	fakeAgent.mu.Unlock()
	if len(actions) != 1 || actions[0] != "container-1:restart" {
		t.Fatalf("expected synchronous container action before return, got %#v", actions)
	}
	waitTaskStatus(t, taskSvc, result.RefreshTaskID, tasks.StatusCompleted)
	var operationTasks int
	if err := store.TaskDB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE type IN (?,?,?,?)`, TaskContainerStart, TaskContainerStop, TaskContainerRestart, TaskContainerDelete).Scan(&operationTasks); err != nil {
		t.Fatal(err)
	}
	if operationTasks != 0 {
		t.Fatalf("container operation task should not be created, got %d", operationTasks)
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

func newContainerizationTestService(t *testing.T) (*Service, *tasks.Service, *fakeContainerizationAgent, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	taskSvc := tasks.NewService(store.TaskDB())
	fakeAgent := &fakeContainerizationAgent{}
	svc := NewService(store.AppDB(), fakeServerProvider{server.Server{
		ID:        "server-1",
		Reachable: true,
		Traits: map[string]string{
			agentcontract.TraitURL:    "http://agent",
			agentcontract.TraitStatus: agentcontract.StatusCompatible,
		},
	}}, fakeAgent, taskSvc)
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
	mu      sync.Mutex
	actions []string
	logTail int
}

func (a *fakeContainerizationAgent) DockerContainers(context.Context, string) ([]agentcontract.DockerContainer, error) {
	return nil, nil
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
	return nil, nil
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
