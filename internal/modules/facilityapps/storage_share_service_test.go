package facilityapps

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
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

type fakeSSHExecutor struct {
	execSudoCalls int
	execErr       error
}

func (f *fakeSSHExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, f.execErr
}
func (f *fakeSSHExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	f.execSudoCalls++
	return sshx.CommandResult{}, f.execErr
}
func (f *fakeSSHExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return f.execErr
}
func (f *fakeSSHExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return f.execErr
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

// fakeStorageAgent 同时满足设施服务的 AgentRuntimeClient 与 StorageAgentClient。
type fakeStorageAgent struct {
	configureCalls   int
	configureEnabled []bool
	configureErr     error
	ensureCalls      int
	ensureErr        error
	archiveErr       error
	deleteErr        error
	archiveContent   []byte
	storageStatus    agentcontract.StorageExportStatus
	storageStatusErr error
	mountStatus      agentcontract.StorageMountStatus
	mountStatusErr   error
}

func (f *fakeStorageAgent) StorageEnsureDirectory(context.Context, string, string) error {
	f.ensureCalls++
	return f.ensureErr
}
func (f *fakeStorageAgent) StorageConfigureExport(_ context.Context, _ string, _ string, _ []string, enabled bool) error {
	f.configureCalls++
	f.configureEnabled = append(f.configureEnabled, enabled)
	return f.configureErr
}
func (f *fakeStorageAgent) StorageArchiveDirectory(_ context.Context, _ string, _ string) ([]byte, string, error) {
	if f.archiveErr != nil {
		return nil, "", f.archiveErr
	}
	return f.archiveContent, "partition.tgz", nil
}
func (f *fakeStorageAgent) StorageDeleteDirectory(_ context.Context, _ string, _ string) error {
	return f.deleteErr
}
func (f *fakeStorageAgent) StorageStatus(context.Context, string, string) (agentcontract.StorageExportStatus, error) {
	return f.storageStatus, f.storageStatusErr
}
func (f *fakeStorageAgent) StorageMountStatus(context.Context, string, string, string) (agentcontract.StorageMountStatus, error) {
	return f.mountStatus, f.mountStatusErr
}
func (f *fakeStorageAgent) RuntimeWriteFiles(context.Context, string, agentcontract.RuntimeWriteFilesRequest) error {
	return nil
}
func (f *fakeStorageAgent) RuntimeCreateContainer(context.Context, string, agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	return agentcontract.RuntimeCreateContainerResponse{}, nil
}
func (f *fakeStorageAgent) RuntimeStop(context.Context, string, agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	return agentcontract.RuntimeInstanceResponse{}, nil
}
func (f *fakeStorageAgent) DockerImagePull(context.Context, string, string) error { return nil }
func (f *fakeStorageAgent) DockerContainerAction(context.Context, string, string, string) error {
	return nil
}

func newStorageShareTestService(t *testing.T) (*Service, *fakeServerProvider, *fakeAppsProvider, *fakeStorageAgent, *storage.Store) {
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
	withAgent := func(host string) map[string]string {
		return map[string]string{agentcontract.TraitEnabled: "true", agentcontract.TraitURL: "https://" + host + ":9786"}
	}
	servers := &fakeServerProvider{servers: []server.Server{
		{ID: "srv-a", Name: "storage-a", Host: "10.0.0.5", Port: 22, SSHUsername: "root", CredentialID: "cred-a", Traits: withAgent("10.0.0.5")},
		{ID: "srv-b", Name: "app-node-b", Host: "10.0.0.6", Port: 22, SSHUsername: "root", CredentialID: "cred-b", Traits: withAgent("10.0.0.6")},
		{ID: "srv-c", Name: "storage-c", Host: "10.0.0.7", Port: 22, SSHUsername: "root", CredentialID: "cred-c", Traits: withAgent("10.0.0.7")},
	}}
	apps := &fakeAppsProvider{}
	agent := &fakeStorageAgent{}
	ssh := &fakeSSHExecutor{}
	svc := NewService(store.AppDB(), agent, servers, apps, WithSSHExecutor(ssh), WithDataRoot(cfg.DataRoot))
	return svc, servers, apps, agent, store
}

func TestStorageShareSaveAndResolveMounts(t *testing.T) {
	svc, _, _, agent, _ := newStorageShareTestService(t)
	ctx := context.Background()
	cfg, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}})
	if err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if !cfg.Enabled || len(cfg.Servers) != 1 || cfg.Servers[0].ServerID != "srv-a" || cfg.Servers[0].Root != "/srv/panel-storage" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if agent.configureCalls != 1 {
		t.Fatalf("expected one agent configure call, got %d", agent.configureCalls)
	}
	resolved, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-a", Target: "/data"}},
	)
	if err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	if len(resolved) != 1 || resolved[0].Type != "nfs" || resolved[0].Source != "10.0.0.5:/srv/panel-storage/srv-a/srv-b/app-1" || resolved[0].Target != "/data" {
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

// TestStorageShareResolverIgnoresNonStorageMountsWhenUnconfigured 回归：应用没有
// storage_share 挂载时，即使设施未配置也不得报错。
func TestStorageShareResolverIgnoresNonStorageMountsWhenUnconfigured(t *testing.T) {
	svc, _, _, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	mounts := []appruntime.Mount{
		{Type: "persistent", Source: "data", Target: "/data"},
		{Type: "volume", Source: "web-data", Target: "/srv"},
	}
	resolved, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		mounts,
	)
	if err != nil {
		t.Fatalf("non-storage mounts must not be affected by facility state: %v", err)
	}
	if len(resolved) != len(mounts) || resolved[0].Type != "persistent" {
		t.Fatalf("mounts must pass through unchanged: %#v", resolved)
	}
}

func TestStorageShareResolveMultiServerWithOwnRoots(t *testing.T) {
	svc, _, _, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{
		{ServerID: "srv-c", Root: "/srv/c-data"},
		{ServerID: "srv-a", Root: "/srv/a-data"},
	}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	cfg, err := svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share: %v", err)
	}
	if len(cfg.Servers) != 2 || cfg.Servers[0].ServerID != "srv-a" || cfg.Servers[0].Root != "/srv/a-data" || cfg.Servers[1].Root != "/srv/c-data" {
		t.Fatalf("server settings should be sorted with own roots: %#v", cfg.Servers)
	}
	resolved, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-c", Target: "/data"}},
	)
	if err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	if resolved[0].Source != "10.0.0.7:/srv/c-data/srv-c/srv-b/app-1" {
		t.Fatalf("unexpected resolved mount for srv-c: %#v", resolved[0])
	}
	legacy, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-2", Name: "app-2"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share", Target: "/data"}},
	)
	if err != nil {
		t.Fatalf("resolve legacy source: %v", err)
	}
	if legacy[0].Source != "10.0.0.5:/srv/a-data/srv-a/srv-b/app-2" {
		t.Fatalf("legacy source should use the first configured server with its own root: %#v", legacy[0])
	}
}

func TestStorageShareUninstallGatedByUsage(t *testing.T) {
	svc, _, apps, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	apps.usages = []applications.StorageShareUsage{{ApplicationID: "app-1", ApplicationName: "app-1"}}
	if _, err := svc.DeleteStorageShare(ctx); err == nil {
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
	result, err := svc.DeleteStorageShare(ctx)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if result.Enabled {
		t.Fatal("storage share should be disabled after uninstall")
	}
	cfg, err = svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share after uninstall: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("storage share should be disabled after uninstall")
	}
}

// TestStorageShareUninstallSucceedsWhenAgentCleanupFails 回归：Agent 清理失败时
// 卸载不得报错，配置照常删除并把警告写入返回配置。
func TestStorageShareUninstallSucceedsWhenAgentCleanupFails(t *testing.T) {
	svc, _, _, agent, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	agent.configureErr = errors.New("agent down")
	result, err := svc.DeleteStorageShare(ctx)
	if err != nil {
		t.Fatalf("uninstall must succeed even when agent cleanup fails: %v", err)
	}
	if !result.Enabled {
		t.Fatal("storage share must stay enabled after failed cleanup so the user can retry uninstall")
	}
	if !strings.Contains(result.LastError, "cleanup") {
		t.Fatalf("expected cleanup warning in result, got %q", result.LastError)
	}
	cfg, err := svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("config must remain enabled after failed cleanup")
	}
}

func TestStorageShareDeletePartitionGatedByUsage(t *testing.T) {
	svc, _, apps, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if _, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-a", Target: "/data"}},
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

func TestStorageShareStatusReportsExportAndMount(t *testing.T) {
	svc, _, _, agent, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if _, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-a", Target: "/data"}},
	); err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	agent.storageStatus = agentcontract.StorageExportStatus{ServerInstalled: true, RootExists: true, ExportLive: true}
	agent.mountStatus = agentcontract.StorageMountStatus{VolumeExists: true, Mounted: true, Writable: true}
	status, err := svc.StorageShareStatus(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Servers) != 1 || !status.Servers[0].ExportLive || !status.Servers[0].AgentOnline {
		t.Fatalf("unexpected server status: %#v", status.Servers)
	}
	if len(status.Partitions) != 1 || !status.Partitions[0].Mounted || !status.Partitions[0].Writable || status.Partitions[0].Target != "/data" {
		t.Fatalf("unexpected partition status: %#v", status.Partitions)
	}
}

func TestStorageShareRootImmutableAfterEnabled(t *testing.T) {
	svc, _, _, _, _ := newStorageShareTestService(t)
	ctx := context.Background()
	cfg, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/a"}}})
	if err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/b"}}, Version: cfg.Version}); err == nil {
		t.Fatal("expected root change to be rejected after the facility is enabled")
	}
	cfg, err = svc.GetStorageShare(ctx)
	if err != nil {
		t.Fatalf("get storage share: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Root != "/srv/a" {
		t.Fatalf("config must remain unchanged: %#v", cfg.Servers)
	}
}

func TestStorageShareSaveRemovedServerCleansExport(t *testing.T) {
	svc, _, _, agent, _ := newStorageShareTestService(t)
	ctx := context.Background()
	first, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{
		{ServerID: "srv-a", Root: "/srv/a"},
		{ServerID: "srv-c", Root: "/srv/c"},
	}})
	if err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	agent.configureEnabled = nil
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/a"}}, Version: first.Version}); err != nil {
		t.Fatalf("save with removed server: %v", err)
	}
	if len(agent.configureEnabled) != 2 {
		t.Fatalf("expected one enable (srv-a) and one disable (srv-c), got %#v", agent.configureEnabled)
	}
	if agent.configureEnabled[0] != false || agent.configureEnabled[1] != true {
		t.Fatalf("expected disable removed server then enable kept server, got %#v", agent.configureEnabled)
	}
}

func TestStorageShareResolveEnsuresDirectory(t *testing.T) {
	svc, _, _, agent, _ := newStorageShareTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if _, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		server.Server{ID: "srv-b", Name: "app-node-b"},
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-a", Target: "/data"}},
	); err != nil {
		t.Fatalf("resolve mounts: %v", err)
	}
	if agent.ensureCalls != 1 {
		t.Fatalf("expected partition directory to be ensured, got %d calls", agent.ensureCalls)
	}
}
