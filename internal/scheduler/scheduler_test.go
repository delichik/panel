package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, taskSvc)

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

func TestRunDueMetricsCollectionAlignsServersToSameSecond(t *testing.T) {
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
	for _, name := range []string{"s1", "s2"} {
		srv, err := serverSvc.Create(ctx, server.SaveRequest{Name: name, Host: "h-" + name, Port: 22, SSHUsername: "du", CredentialID: cred.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppDB().Exec(`UPDATE servers SET os_id='debian',os_version_id='12',os_supported=1,reachable=1 WHERE id=?`, srv.ID); err != nil {
			t.Fatal(err)
		}
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, fakeMetricsExecutor{})
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, taskSvc)

	if err := sched.runDueMetricsCollection(ctx); err != nil {
		t.Fatal(err)
	}

	rows, err := store.MetricsDB().Query(`SELECT time FROM metrics_snapshots ORDER BY server_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var times []string
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			t.Fatal(err)
		}
		times = append(times, ts)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(times) != 2 {
		t.Fatalf("expected two metrics snapshots, got %d", len(times))
	}
	if times[0] != times[1] {
		t.Fatalf("expected metrics timestamps to align, got %v", times)
	}
	aligned, err := time.Parse(time.RFC3339Nano, times[0])
	if err != nil {
		t.Fatal(err)
	}
	if aligned.Nanosecond() != 0 {
		t.Fatalf("expected second-aligned timestamp, got %s", aligned)
	}
}

func TestRunDueConnectivityTestsStartsQueuedTask(t *testing.T) {
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
	createServerSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, err := createServerSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskSvc.Create(ctx, tasks.CreateInput{
		Type:         "server_connectivity_test",
		ServerID:     srv.ID,
		ResourceType: "server",
		ResourceID:   srv.ID,
		Summary:      "Testing SSH connectivity",
	})
	if err != nil {
		t.Fatal(err)
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	runServerSvc := server.NewService(store.AppDB(), fakeConnectivityExecutor{}, taskSvc)
	sched := New(settingsSvc, runServerSvc, nil, nil, taskSvc)

	if err := sched.runDueConnectivityTests(ctx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := taskSvc.Get(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == tasks.StatusCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := taskSvc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("expected queued connectivity task to complete, got %#v", got)
}

type fakeMetricsExecutor struct{}

func (fakeMetricsExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{Stdout: "100 90\n1000 200\n2000 500\n1000000000 10 20\n2000000000 15 25\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3\n"}, nil
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

type fakeConnectivityExecutor struct{}

func (fakeConnectivityExecutor) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.Contains(command.Command, "cat /etc/os-release") {
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "cores=") {
		return sshx.CommandResult{Stdout: "cores=2\nmem=2048\ndisk=64\nhostname=test-node\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (fakeConnectivityExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (fakeConnectivityExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (fakeConnectivityExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
