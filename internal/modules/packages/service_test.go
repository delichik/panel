package packages

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
	"panel/internal/platform/secrets"
	"panel/internal/platform/ssh"
)

func TestPackageServiceBlocksUnsupportedServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	credSvc := credential.NewService(store.AppDB(), secrets)
	cred, _ := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	srv, _ := serverSvc.Create(ctx, server.SaveRequest{Name: "s", IPv4: "10.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	registerPackageTestTasks(taskSvc, svc)
	if _, err := svc.Refresh(ctx, srv.ID); err == nil {
		t.Fatal("expected unsupported server to be blocked")
	}
}

func TestPackageServiceAcceptsRootPrivilegeWithoutSudo(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','root','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'root','cred','debian','12',1,0,'root','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	srv, err := svc.ensurePackageAllowed(context.Background(), "srv", true)
	if err != nil {
		t.Fatal(err)
	}
	if !srv.Privilege.Privileged || srv.Privilege.Mode != sshx.PrivilegeModeRoot {
		t.Fatalf("unexpected privilege state: %#v", srv.Privilege)
	}
}

func TestRefreshRecordsTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	registerPackageTestTasks(taskSvc, svc)
	svc.adapter = fakePackageAdapter{updates: []linux.PackageUpdate{{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2"}}}

	result, err := svc.Refresh(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Refreshing || result.TaskID == "" {
		t.Fatalf("expected refresh to be marked running, got %#v", result)
	}
	waitForPackageRefresh(t, svc, "srv")

	var count int
	if err := store.LogDB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE type='package_refresh'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one package refresh task, got %d", count)
	}
	task, err := taskSvc.Get(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed package refresh task, got %#v", task)
	}
	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if list.Refreshing {
		t.Fatalf("expected refreshing to clear after completion, got %#v", list)
	}
	if len(list.Updates) != 1 {
		t.Fatalf("expected cached update, got %#v", list.Updates)
	}
}

func TestRefreshFailureRecordsFailedTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	registerPackageTestTasks(taskSvc, svc)
	svc.adapter = fakePackageAdapter{err: errors.New("apt failed")}

	result, err := svc.Refresh(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatalf("expected refresh task id, got %#v", result)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == tasks.StatusFailed {
			if !strings.Contains(task.Error, "apt failed") {
				t.Fatalf("expected task error to include apt failure, got %#v", task)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := taskSvc.Get(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("expected failed package refresh task, got %#v", task)
}

func TestRefreshUsesUbuntuAdapter(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_pretty_name,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','ubuntu','24.04','Ubuntu 24.04 LTS',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	exec := &aptPackageExecutor{stdout: "Listing...\nopenssl/jammy-updates 3.0.2-0ubuntu1 amd64 [upgradable from: 3.0.1-0ubuntu1]\n"}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	svc := NewService(store.AppDB(), serverSvc, exec, taskSvc)
	registerPackageTestTasks(taskSvc, svc)

	result, err := svc.Refresh(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Refreshing {
		t.Fatalf("expected refresh to be marked running, got %#v", result)
	}
	waitForPackageRefresh(t, svc, "srv")

	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if exec.sudoCalls != 1 || !strings.Contains(exec.lastCommand, "apt list --upgradable") {
		t.Fatalf("expected apt list command through Ubuntu adapter, calls=%d command=%q", exec.sudoCalls, exec.lastCommand)
	}
	if len(list.Updates) != 1 || list.Updates[0].Name != "openssl" {
		t.Fatalf("expected cached Ubuntu package update, got %#v", list.Updates)
	}
}

func TestReplaceUpdates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), nil, nil, nil)
	if err := svc.replaceUpdates(context.Background(), "srv", []linux.PackageUpdate{{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2"}}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 1 || list.LastRefreshedAt == nil {
		t.Fatalf("unexpected list: %#v", list)
	}
}

type fakePackageAdapter struct {
	updates []linux.PackageUpdate
	err     error
}

func (f fakePackageAdapter) ListUpgradeable(context.Context, sshx.RemoteExecutor, sshx.Target) ([]linux.PackageUpdate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.updates, nil
}

func (f fakePackageAdapter) UpgradeSelected(context.Context, sshx.RemoteExecutor, sshx.Target, []string, linux.LogSink) error {
	return nil
}

func (f fakePackageAdapter) UpgradeAll(context.Context, sshx.RemoteExecutor, sshx.Target, linux.LogSink) error {
	return nil
}

type aptPackageExecutor struct {
	stdout      string
	sudoCalls   int
	lastCommand string
}

func (f *aptPackageExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *aptPackageExecutor) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCalls++
	f.lastCommand = command.Command
	return sshx.CommandResult{Stdout: f.stdout, ExitCode: 0}, nil
}

func (f *aptPackageExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *aptPackageExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func waitForPackageRefresh(t *testing.T, svc *Service, serverID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		list, err := svc.List(context.Background(), serverID)
		if err == nil && !list.Refreshing {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("package refresh did not finish")
}

func registerPackageTestTasks(taskSvc *tasks.Service, svc *Service) {
	svc.RegisterTasks(taskSvc)
}

type blockingUpgradeAdapter struct {
	fakePackageAdapter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingUpgradeAdapter) UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, sink linux.LogSink) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func waitForTaskStatus(t *testing.T, taskSvc *tasks.Service, taskID, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := taskSvc.Get(context.Background(), taskID)
		if err == nil && got.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := taskSvc.Get(context.Background(), taskID)
	t.Fatalf("task %s did not reach status %s, got %#v", taskID, status, got)
}

// TestUpgradeMaintenanceMutexBlocksConcurrentUpgrade 验证升级使用 per-server
// 维护互斥：第一个升级进行中时，第二个升级任务必须失败而不是并发执行。
func TestUpgradeMaintenanceMutexBlocksConcurrentUpgrade(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	registerPackageTestTasks(taskSvc, svc)
	adapter := &blockingUpgradeAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	svc.adapter = adapter

	first, err := svc.UpgradeAll(ctx, "srv")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-adapter.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first upgrade did not start")
	}
	second, err := svc.UpgradeSelected(ctx, "srv", []string{"openssl"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, second.ID, tasks.StatusFailed)
	secondTask, err := taskSvc.Get(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondTask.Error, "maintenance") {
		t.Fatalf("expected maintenance conflict, got %#v", secondTask)
	}
	close(adapter.release)
	waitForTaskStatus(t, taskSvc, first.ID, tasks.StatusCompleted)
}

// TestUpgradeQueuedTaskRecoversAfterRestart 验证注册 Execute 后，重启遗留的
// queued 升级任务可以由 manager 恢复执行而不是死任务。
func TestUpgradeQueuedTaskRecoversAfterRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	registerPackageTestTasks(taskSvc, svc)
	svc.adapter = fakePackageAdapter{updates: []linux.PackageUpdate{{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2"}}}

	// 模拟重启前遗留的 queued 升级任务（含 ParamsJSON 名称）。
	task, err := taskSvc.Create(ctx, tasks.CreateInput{
		Type:         "package_upgrade_selected",
		ServerID:     "srv",
		ResourceType: "server",
		ResourceID:   "srv",
		TriggerType:  "user",
		Summary:      "Upgrading selected packages",
		ParamsJSON:   `{"names":["openssl"]}`,
		Status:       tasks.StatusQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	// manager 恢复执行（等价于 worker 拾取 queued 任务后调用已注册 Execute）。
	if err := tasks.NewManager(taskSvc).Run(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := taskSvc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("expected queued upgrade task to recover and complete, got %#v", got)
	}
}
