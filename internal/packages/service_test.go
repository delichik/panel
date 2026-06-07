package packages

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/linux"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestPackageServiceBlocksUnsupportedServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, _ := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	taskSvc := tasks.NewService(store.AppDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, _ := serverSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	if _, err := svc.Refresh(ctx, srv.ID); err == nil {
		t.Fatal("expected unsupported server to be blocked")
	}
}

func TestRefreshRunsWithoutCreatingTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), server.NewService(store.AppDB(), nil, tasks.NewService(store.AppDB())), nil, tasks.NewService(store.AppDB()))
	svc.adapter = fakePackageAdapter{updates: []linux.PackageUpdate{{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2"}}}

	result, err := svc.Refresh(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Refreshing {
		t.Fatalf("expected refresh to be marked running, got %#v", result)
	}
	waitForPackageRefresh(t, svc, "srv")

	var count int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE type='package_refresh'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no package refresh tasks, got %d", count)
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

func TestRefreshUsesUbuntuAdapter(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_pretty_name,os_supported,sudo_passwordless,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','ubuntu','24.04','Ubuntu 24.04 LTS',1,1,'now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	exec := &aptPackageExecutor{stdout: "Listing...\nopenssl/jammy-updates 3.0.2-0ubuntu1 amd64 [upgradable from: 3.0.1-0ubuntu1]\n"}
	taskSvc := tasks.NewService(store.AppDB())
	svc := NewService(store.AppDB(), server.NewService(store.AppDB(), nil, taskSvc), exec, taskSvc)

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
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','now','now')`)
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
}

func (f fakePackageAdapter) ListUpgradeable(context.Context, sshx.RemoteExecutor, sshx.Target) ([]linux.PackageUpdate, error) {
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
