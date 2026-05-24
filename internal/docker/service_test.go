package docker

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestListServicesRequiresCapabilityRefresh(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	if _, err := svc.ListServices(context.Background(), srvID); err == nil || err.Error() != "Docker capability check is running in the background" {
		t.Fatalf("expected pending capability error, got %v", err)
	}
}

func TestCapabilityQueuesBackgroundRefreshWhenMissing(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	cap, err := svc.Capability(context.Background(), srvID)
	if err != nil {
		t.Fatal(err)
	}
	if !cap.Pending || cap.TaskID != "" {
		t.Fatalf("expected pending background task, got %#v", cap)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE type='docker_status_refresh'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no docker refresh task records, got %d", count)
	}
}

func TestListServicesBlocksUnsupportedCapability(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	cap := DockerCapability{ServerID: srvID, DockerInstalled: false, ComposeInstalled: false, Supported: false}
	if err := svc.writeCapability(context.Background(), cap); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListServices(context.Background(), srvID); err == nil {
		t.Fatal("expected unsupported Docker capability to block list")
	}
}

func TestRefreshRunsWithoutCreatingTask(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := svc.Refresh(ctx, srvID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Refreshing {
		t.Fatalf("expected refresh to be running, got %#v", got)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE type='docker_status_refresh'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no docker refresh task records, got %d", count)
	}
}

func TestComposeStatusUsesCachedServices(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()
	cap := DockerCapability{ServerID: srvID, DockerInstalled: true, DockerVersion: "25", ComposeInstalled: true, ComposeVersion: "2", Supported: true, LastCheckedAt: &now}
	if err := svc.writeCapability(ctx, cap); err != nil {
		t.Fatal(err)
	}
	if err := svc.writeCache(ctx, srvID, "services", []RuntimeService{{Name: "demo-web-1", Project: "demo", Service: "web", State: "running"}}, now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ComposeStatus(ctx, srvID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "running" || len(got.Services) != 1 || got.Services[0].Service != "web" {
		t.Fatalf("unexpected cached status: %#v", got)
	}
}

func TestRuntimeExplorerPruneRouteParses(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/api/v1/runtime-explorer/nodes/node-1/prune", nil)
	if err != nil {
		t.Fatal(err)
	}
	op, ok := runtimeExplorerOperation(req)
	if !ok {
		t.Fatal("expected prune route to parse")
	}
	if op.Kind != "image" || op.Action != "prune" {
		t.Fatalf("unexpected prune operation: %#v", op)
	}
}

func TestRuntimeExplorerManagedRestartCreatesContainerServiceTask(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := svc.writeCapability(ctx, DockerCapability{ServerID: srvID, DockerInstalled: true, DockerVersion: "25", ComposeInstalled: true, ComposeVersion: "2", IncludeSupported: true, Supported: true, LastCheckedAt: &now}); err != nil {
		t.Fatal(err)
	}
	restartTask, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: "container_service_restart", ResourceType: "container_service", ResourceID: "csvc_1", Summary: "Restarting api"})
	if err != nil {
		t.Fatal(err)
	}
	svc.containerServices = fakeContainerServiceRestarter{task: restartTask}
	if err := svc.writeCache(ctx, srvID, "services", []RuntimeService{{
		ID:      "container-1",
		Name:    "api",
		Managed: true,
		Labels:  map[string]string{"panel.service.id": "csvc_1", "panel.service.name": "api"},
	}}, now); err != nil {
		t.Fatal(err)
	}
	task, err := svc.RuntimeExplorerResourceTask(ctx, srvID, ResourceOperation{Kind: "container", Action: "restart", ID: "container-1"})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != restartTask.ID || task.Type != "container_service_restart" {
		t.Fatalf("expected Container Service restart task, got %#v", task)
	}
}

func newTestService(t *testing.T) (*Service, string, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, err := serverSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store.AppDB(), serverSvc, fakeRuntime{}, taskSvc), srv.ID, func() { _ = store.Close() }
}

type fakeRuntime struct{}

type fakeContainerServiceRestarter struct {
	task tasks.Task
}

func (f fakeContainerServiceRestarter) Restart(ctx context.Context, serviceID string) (tasks.Task, error) {
	if serviceID != "csvc_1" {
		return tasks.Task{}, panelerr.NotFound("container_service")
	}
	return f.task, nil
}

func (fakeRuntime) Detect(context.Context, sshx.Target) (DockerCapability, error) {
	now := time.Now().UTC()
	return DockerCapability{DockerInstalled: false, ComposeInstalled: false, Supported: false, LastCheckedAt: &now}, nil
}

func (fakeRuntime) ListComposeProjects(context.Context, sshx.Target) ([]ComposeProject, error) {
	return []ComposeProject{}, nil
}

func (fakeRuntime) ListServices(context.Context, sshx.Target) ([]RuntimeService, error) {
	return []RuntimeService{}, nil
}

func (fakeRuntime) ListNetworks(context.Context, sshx.Target) ([]RuntimeNetwork, error) {
	return []RuntimeNetwork{}, nil
}

func (fakeRuntime) ListVolumes(context.Context, sshx.Target) ([]RuntimeVolume, error) {
	return []RuntimeVolume{}, nil
}

func (fakeRuntime) ListImages(context.Context, sshx.Target) ([]RuntimeImage, error) {
	return []RuntimeImage{}, nil
}

func (fakeRuntime) ReadComposeStatus(context.Context, sshx.Target, string) (ComposeStatus, error) {
	return ComposeStatus{}, nil
}

func (fakeRuntime) ProbeComposeInclude(context.Context, sshx.Target) error { return nil }

func (fakeRuntime) StartContainer(context.Context, sshx.Target, string) error   { return nil }
func (fakeRuntime) RestartContainer(context.Context, sshx.Target, string) error { return nil }
func (fakeRuntime) StopContainer(context.Context, sshx.Target, string) error    { return nil }
func (fakeRuntime) DeleteContainer(context.Context, sshx.Target, string) error  { return nil }
func (fakeRuntime) DeleteNetwork(context.Context, sshx.Target, string) error    { return nil }
func (fakeRuntime) DeleteVolume(context.Context, sshx.Target, string) error     { return nil }
func (fakeRuntime) DeleteImage(context.Context, sshx.Target, string) error      { return nil }
func (fakeRuntime) PruneNetworks(context.Context, sshx.Target) error            { return nil }
func (fakeRuntime) PruneVolumes(context.Context, sshx.Target) error             { return nil }
func (fakeRuntime) PruneImages(context.Context, sshx.Target) error              { return nil }
func (fakeRuntime) CheckImageUpdate(context.Context, sshx.Target, RuntimeImage) (ImageUpdate, error) {
	return ImageUpdate{}, nil
}
func (fakeRuntime) PullImage(context.Context, sshx.Target, string, string) error { return nil }
