package facilityapps

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	sshx "panel/internal/platform/ssh"
)

type fakeServerProvider struct {
	servers []server.Server
}

func (f *fakeServerProvider) List(context.Context) ([]server.Server, error) { return f.servers, nil }
func (f *fakeServerProvider) Get(_ context.Context, id string) (server.Server, error) {
	for _, srv := range f.servers {
		if srv.ID == id {
			return srv, nil
		}
	}
	return server.Server{}, panelerr.NotFound("server")
}

type fakeAppsProvider struct {
	usages []applications.StorageShareUsage
}

func (f *fakeAppsProvider) ApplicationReverseProxyConfigs(context.Context) ([]applications.ApplicationReverseProxyConfig, error) {
	return nil, nil
}
func (f *fakeAppsProvider) ApplicationsUsingStorageShare(context.Context) ([]applications.StorageShareUsage, error) {
	return f.usages, nil
}

type fakeSSHExecutor struct {
	execSudoCalls int
	uploadCalls   int
	catOutput     string
}

func (f *fakeSSHExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}
func (f *fakeSSHExecutor) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.execSudoCalls++
	if strings.Contains(command.Command, "cat ") {
		return sshx.CommandResult{Stdout: f.catOutput}, nil
	}
	return sshx.CommandResult{}, nil
}
func (f *fakeSSHExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	f.uploadCalls++
	return nil
}
func (f *fakeSSHExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error { return nil }

func newStorageShareTestService(t *testing.T) (*Service, *fakeServerProvider, *fakeAppsProvider, *fakeSSHExecutor, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = dir
	cfg.AppDatabase = filepath.Join(dir, "db", "app.db")
	cfg.LogDatabase = filepath.Join(dir, "db", "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "db", "coordination.db")
	cfg.MetricsDatabase = filepath.Join(dir, "db", "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	servers := &fakeServerProvider{servers: []server.Server{
		{ID: "srv-a", Name: "storage-a", Host: "10.0.0.5", Port: 22, SSHUsername: "root", CredentialID: "cred-a"},
		{ID: "srv-b", Name: "app-node-b", Host: "10.0.0.6", Port: 22, SSHUsername: "root", CredentialID: "cred-b"},
	}}
	apps := &fakeAppsProvider{}
	ssh := &fakeSSHExecutor{}
	svc := NewService(store.AppDB(), nil, servers, apps, WithSSHExecutor(ssh), WithDataRoot(cfg.DataRoot))
	return svc, servers, apps, ssh, store
}

func TestStorageShareSaveAndResolveMounts(t *testing.T) {
	svc, _, _, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	cfg, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{ServerID: "srv-a", Root: "/srv/panel-storage"})
	if err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if !cfg.Enabled || cfg.ServerID != "srv-a" || cfg.Root != "/srv/panel-storage" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	resolved, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: StorageShareID, Target: "/data"}},
	)
	if err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Type != "nfs" || resolved[0].Source != "10.0.0.5:/srv/panel-storage/srv-b/app-1" || resolved[0].Target != "/data" {
		t.Fatalf("unexpected resolved mount: %#v", resolved)
	}
	partitions, err := svc.listStoragePartitions(ctx)
	if err != nil {
		t.Fatalf("list partitions: %v", err)
	}
	if len(partitions) != 1 || partitions[0].ApplicationID != "app-1" || partitions[0].ServerID != "srv-b" || partitions[0].StorageServerID != "srv-a" {
		t.Fatalf("unexpected partitions: %#v", partitions)
	}
}

func TestStorageShareUninstallGatedByUsage(t *testing.T) {
	svc, _, apps, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{ServerID: "srv-a", Root: "/srv/panel-storage"}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	apps.usages = []applications.StorageShareUsage{{ApplicationID: "app-1", ApplicationName: "app-1"}}
	if err := svc.DeleteStorageShare(ctx); err == nil {
		t.Fatal("expected uninstall to be blocked while an application uses the storage share")
	}
	cfg, err := svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("storage share should still be enabled after blocked uninstall")
	}
	apps.usages = nil
	if err := svc.DeleteStorageShare(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	cfg, err = svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share after uninstall: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("storage share should be disabled after uninstall")
	}
}

func TestStorageShareDeletePartitionGatedByUsage(t *testing.T) {
	svc, _, apps, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{ServerID: "srv-a", Root: "/srv/panel-storage"}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if _, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: StorageShareID, Target: "/data"}},
	); err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	partitions, err := svc.listStoragePartitions(ctx)
	if err != nil || len(partitions) != 1 {
		t.Fatalf("list partitions: %#v err=%v", partitions, err)
	}
	apps.usages = []applications.StorageShareUsage{{ApplicationID: "app-1", ApplicationName: "app-1"}}
	if err := svc.DeleteStoragePartition(ctx, partitions[0].ID); err == nil {
		t.Fatal("expected partition deletion to be blocked while its application uses the storage share")
	}
	apps.usages = nil
	if err := svc.DeleteStoragePartition(ctx, partitions[0].ID); err != nil {
		t.Fatalf("delete partition: %v", err)
	}
	partitions, err = svc.listStoragePartitions(ctx)
	if err != nil {
		t.Fatalf("list partitions after delete: %v", err)
	}
	if len(partitions) != 0 {
		t.Fatalf("expected no partitions after delete, got %#v", partitions)
	}
}