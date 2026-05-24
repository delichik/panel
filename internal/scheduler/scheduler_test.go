package scheduler

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/metrics"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestCollectMetricsDoesNotCreateTask(t *testing.T) {
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
	_, err = store.AppDB().Exec(`UPDATE servers SET os_id='debian',os_version_id='12',os_supported=1,reachable=1 WHERE id=?`, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, fakeMetricsExecutor{})
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, nil, taskSvc, nil)

	if err := sched.collectMetrics(ctx, srv); err != nil {
		t.Fatal(err)
	}
	var taskCount int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM tasks WHERE type='metrics_collect'`).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("expected no metrics task records, got %d", taskCount)
	}
	var metricCount int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, srv.ID).Scan(&metricCount); err != nil {
		t.Fatal(err)
	}
	if metricCount != 1 {
		t.Fatalf("expected one metrics snapshot, got %d", metricCount)
	}
}

type fakeMetricsExecutor struct{}

func (fakeMetricsExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{Stdout: "100 90\n1000 200\n2000 500\n10 20\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3\n"}, nil
}

func (fakeMetricsExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (fakeMetricsExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (fakeMetricsExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
