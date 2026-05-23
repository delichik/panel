package docker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
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
	if !cap.Pending || cap.TaskID == "" {
		t.Fatalf("expected pending background task, got %#v", cap)
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

func TestRefreshReturnsExistingRunningTaskWithoutBlocking(t *testing.T) {
	svc, srvID, closeStore := newTestService(t)
	defer closeStore()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: "docker_status_refresh", ServerID: srvID, Summary: "refreshing"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.tasks.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Refresh(ctx, srvID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != task.ID {
		t.Fatalf("expected existing task %s, got %s", task.ID, got.ID)
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

func (fakeRuntime) StartContainer(context.Context, sshx.Target, string) error  { return nil }
func (fakeRuntime) RestartContainer(context.Context, sshx.Target, string) error { return nil }
func (fakeRuntime) StopContainer(context.Context, sshx.Target, string) error   { return nil }
func (fakeRuntime) DeleteContainer(context.Context, sshx.Target, string) error { return nil }
func (fakeRuntime) DeleteNetwork(context.Context, sshx.Target, string) error   { return nil }
func (fakeRuntime) DeleteVolume(context.Context, sshx.Target, string) error    { return nil }
func (fakeRuntime) DeleteImage(context.Context, sshx.Target, string) error     { return nil }
func (fakeRuntime) PruneNetworks(context.Context, sshx.Target) error           { return nil }
func (fakeRuntime) PruneVolumes(context.Context, sshx.Target) error            { return nil }
func (fakeRuntime) PruneImages(context.Context, sshx.Target) error             { return nil }
func (fakeRuntime) CheckImageUpdate(context.Context, sshx.Target, RuntimeImage) (ImageUpdate, error) {
	return ImageUpdate{}, nil
}
func (fakeRuntime) PullImage(context.Context, sshx.Target, string, string) error { return nil }
