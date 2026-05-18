package server

import (
	"context"
	"path/filepath"
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
	_, err = svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID, Labels: []string{"lab"}})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Labels[0] != "lab" {
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

type connectivityFakeExec struct {
	sudoTimeout time.Duration
}

func (f *connectivityFakeExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
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
