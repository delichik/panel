package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestServerValidation(t *testing.T) {
	if err := validateSave(SaveRequest{Port: 22}); err == nil {
		t.Fatal("expected validation error")
	}
	if err := validateSave(SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, CredentialID: "cred"}); err != nil {
		t.Fatalf("server username should be optional: %v", err)
	}
	if err := validateSave(SaveRequest{Name: "s", Host: "127.0.0.1", Port: 70000, CredentialID: "cred"}); err == nil {
		t.Fatal("expected port validation error")
	}
}

func TestCreateListServer(t *testing.T) {
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
	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, err := credSvc.Create(context.Background(), credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	svc := NewService(store.AppDB(), nil, taskSvc)
	_, err = svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID, Traits: map[string]string{"custom.env": "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Traits["custom.env"] != "prod" {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func TestConnectivityUsesBoundedSudoTimeoutAndCompletes(t *testing.T) {
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

	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, err := credSvc.Create(context.Background(), credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &connectivityFakeExec{}
	svc := NewService(store.AppDB(), exec, taskSvc)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.TestConnectivity(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got tasks.Task
	for time.Now().Before(deadline) {
		got, err = taskSvc.Get(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == tasks.StatusCompleted || got.Status == tasks.StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed task, got %#v", got)
	}
	if exec.sudoTimeout != connectivitySudoTimeout {
		t.Fatalf("expected sudo timeout %s, got %s", connectivitySudoTimeout, exec.sudoTimeout)
	}

	// 验证系统特征是否成功探测并自动入库
	srv, err = svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits["sys.cpu_cores"] != "8" || srv.Traits["sys.memory_total_mb"] != "16384" || srv.Traits["sys.disk_total_gb"] != "256" || srv.Traits["sys.hostname"] != "test-node" || srv.Traits["sys.os"] != "debian-13" {
		t.Fatalf("unexpected system traits detected: %#v", srv.Traits)
	}

	logs, _, err := taskSvc.Logs(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		if log.Line == "passwordless sudo unavailable: sudo denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sudo unavailable log, got %#v", logs)
	}
}

func TestCreateServerAutomaticallyStartsConnectivityTest(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, &connectivityFakeExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}

	var task tasks.Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: "server_connectivity_test", ServerID: srv.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) == 1 {
			task = result.Items[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.ID == "" {
		t.Fatal("expected auto connectivity task")
	}
	waitTaskFinished(t, taskSvc, task.ID)
}

func TestConnectivityFailureSchedulesRetryAndRunNow(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, failingConnectivityExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}

	var task tasks.Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: "server_connectivity_test", ServerID: srv.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) == 1 {
			task = result.Items[0]
			if task.Status == tasks.StatusFailedRetryable {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Status != tasks.StatusFailedRetryable || task.NextRunAt == nil || task.RetryCount != 1 {
		t.Fatalf("expected retryable scheduled task, got %#v", task)
	}
	task, err = taskSvc.RunNow(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusQueued || task.NextRunAt != nil {
		t.Fatalf("run now should queue immediately, got %#v", task)
	}
}

type connectivityFakeExec struct {
	sudoTimeout time.Duration
}

func (f *connectivityFakeExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.Contains(command.Command, "cat /etc/os-release") {
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "cores=") {
		return sshx.CommandResult{Stdout: "cores=8\nmem=16384\ndisk=256\nhostname=test-node\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *connectivityFakeExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoTimeout = command.Timeout
	return sshx.CommandResult{ExitCode: 1}, errString("sudo denied")
}

func (f *connectivityFakeExec) Upload(ctx context.Context, target sshx.Target, transfer sshx.UploadSpec) error {
	return nil
}

func (f *connectivityFakeExec) Download(ctx context.Context, target sshx.Target, transfer sshx.DownloadSpec) error {
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

type failingConnectivityExec struct{}

func (failingConnectivityExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errString("dial timeout")
}

func (failingConnectivityExec) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errString("dial timeout")
}

func (failingConnectivityExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (failingConnectivityExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func testServerService(t *testing.T, exec sshx.RemoteExecutor) (*Service, *tasks.Service, *storage.Store) {
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
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	return NewService(store.AppDB(), exec, taskSvc), taskSvc, store
}

func waitTaskFinished(t *testing.T, taskSvc *tasks.Service, taskID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}
