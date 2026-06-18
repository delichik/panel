package scheduling

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/secrets"
	"panel/internal/platform/ssh"
)

func newSchedulerTestCredentialService(t *testing.T, store *storage.Store, cfg config.Config) *credential.Service {
	t.Helper()
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	return credential.NewService(store.AppDB(), secrets)
}

func TestCollectMetricsCreatesTaskRecord(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := newSchedulerTestCredentialService(t, store, cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, err := serverSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`UPDATE servers SET os_id='debian',os_version_id='12',os_supported=1,reachable=1 WHERE id=?`, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, `{"agent.enabled":"true","agent.url":"https://agent.local","agent.status":"compatible"}`, srv.ID); err != nil {
		t.Fatal(err)
	}
	srv, err = serverSvc.Get(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, fakeMetricsExecutor{}, metrics.WithAgentClient(fakeSchedulerAgentClient{}))
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, taskSvc)

	batch, shouldRun, err := sched.collectMetricsInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun {
		t.Fatal("expected metrics collection inputs")
	}
	if _, _, err := tasks.NewManager(taskSvc).CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: "scheduler", Periodic: true}); err != nil {
		t.Fatal(err)
	}
	waitForMetricCount(t, store.MetricsDB(), srv.ID, 1)
	result, err := taskSvc.List(ctx, tasks.ListFilter{Type: "metrics_collect", IncludeInternal: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one metrics task record, got %#v", result)
	}
	if result.Items[0].Status != tasks.StatusCompleted || result.Items[0].ServerID != srv.ID {
		t.Fatalf("expected completed metrics task for server, got %#v", result.Items[0])
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
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := newSchedulerTestCredentialService(t, store, cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	for _, name := range []string{"s1", "s2"} {
		srv, err := serverSvc.Create(ctx, server.SaveRequest{Name: name, Host: "h-" + name, Port: 22, SSHUsername: "du", CredentialID: cred.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppDB().Exec(`UPDATE servers SET os_id='debian',os_version_id='12',os_supported=1,reachable=1 WHERE id=?`, srv.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, `{"agent.enabled":"true","agent.url":"https://`+name+`.agent","agent.status":"compatible"}`, srv.ID); err != nil {
			t.Fatal(err)
		}
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, fakeMetricsExecutor{}, metrics.WithAgentClient(fakeSchedulerAgentClient{}))
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, taskSvc)

	batch, shouldRun, err := sched.collectMetricsInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun {
		t.Fatal("expected metrics collection inputs")
	}
	if _, _, err := tasks.NewManager(taskSvc).CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: "scheduler", Periodic: true}); err != nil {
		t.Fatal(err)
	}
	waitForTotalMetricCount(t, store.MetricsDB(), 2)

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
	taskRows, err := store.TaskDB().Query(`SELECT operation_id FROM tasks WHERE type='metrics_collect' AND resource_type='server' ORDER BY server_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer taskRows.Close()
	operationIDs := []string{}
	for taskRows.Next() {
		var operationID string
		if err := taskRows.Scan(&operationID); err != nil {
			t.Fatal(err)
		}
		operationIDs = append(operationIDs, operationID)
	}
	if err := taskRows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(operationIDs) != 2 || operationIDs[0] == "" || operationIDs[0] != operationIDs[1] {
		t.Fatalf("expected metrics tasks to share an operation, got %#v", operationIDs)
	}
}

func TestRunDueMetricsCollectionSkipsServersWithoutReadyAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := newSchedulerTestCredentialService(t, store, cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, err := serverSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE servers SET os_id='debian',os_version_id='12',os_supported=1,reachable=1,traits=? WHERE id=?`, `{"agent.enabled":"true","agent.url":"https://agent.local","agent.status":"unavailable"}`, srv.ID); err != nil {
		t.Fatal(err)
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, fakeMetricsExecutor{}, metrics.WithAgentClient(fakeSchedulerAgentClient{}))
	sched := New(settingsSvc, serverSvc, metricsSvc, nil, taskSvc)

	batch, shouldRun, err := sched.collectMetricsInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shouldRun || len(batch.Inputs) != 0 {
		t.Fatalf("expected no metrics inputs for unavailable agent, got %#v", batch)
	}
	result, err := taskSvc.List(ctx, tasks.ListFilter{Type: "metrics_collect", IncludeInternal: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected no metrics task for unavailable agent, got %#v", result.Items)
	}
	var metricCount int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, srv.ID).Scan(&metricCount); err != nil {
		t.Fatal(err)
	}
	if metricCount != 0 {
		t.Fatalf("expected no metrics snapshot for unavailable agent, got %d", metricCount)
	}
}

func TestRunDueConnectivityTestsStartsQueuedTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := newSchedulerTestCredentialService(t, store, cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
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

	batch, shouldRun, err := sched.collectQueueDrainInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun {
		t.Fatal("expected queue drain input")
	}
	if _, _, err := tasks.NewManager(taskSvc).CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: "scheduler", Periodic: true}); err != nil {
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

func TestRunNowConnectivityTaskStartsProvidedTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := newSchedulerTestCredentialService(t, store, cfg)
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
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
		Summary:      "Retrying Testing SSH connectivity",
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

	if err := sched.RunNow(ctx, task); err != nil {
		t.Fatal(err)
	}

	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
}

func TestQueueDrainStartsDuePackageRefreshTask(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	task, err := taskSvc.Create(ctx, tasks.CreateInput{
		Type:         "package_refresh",
		ServerID:     "srv",
		ResourceType: "server",
		ResourceID:   "srv",
		TriggerType:  "retry",
		Summary:      "Retrying Refreshing package updates",
	})
	if err != nil {
		t.Fatal(err)
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	packageSvc := packages.NewService(store.AppDB(), serverSvc, fakePackageExecutor{}, taskSvc)
	sched := New(settingsSvc, serverSvc, nil, packageSvc, taskSvc)

	batch, shouldRun, err := sched.collectQueueDrainInput(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun {
		t.Fatal("expected queue drain input")
	}
	if _, _, err := tasks.NewManager(taskSvc).CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: "scheduler", Periodic: true}); err != nil {
		t.Fatal(err)
	}

	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
}

func TestRunScheduledPackageRefreshesSharesOperation(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"srv_1", "srv_2"} {
		if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES(?,?,?,22,'du','cred','debian','12',1,1,1,'now','now')`, id, id, "h-"+id); err != nil {
			t.Fatal(err)
		}
	}
	taskSvc := tasks.NewService(store.TaskDB())
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	packageSvc := packages.NewService(store.AppDB(), serverSvc, fakePackageExecutor{}, taskSvc)
	sched := New(settingsSvc, serverSvc, nil, packageSvc, taskSvc)

	batch, shouldRun, err := sched.collectScheduledPackageRefreshInputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun {
		t.Fatal("expected scheduled package inputs")
	}
	if _, _, err := tasks.NewManager(taskSvc).CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: "scheduler", Periodic: true}); err != nil {
		t.Fatal(err)
	}

	result, err := taskSvc.List(ctx, tasks.ListFilter{Type: "package_refresh", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	serverTasks := []tasks.Task{}
	for _, task := range result.Items {
		if task.ResourceType == "server" {
			serverTasks = append(serverTasks, task)
		}
	}
	if len(serverTasks) != 2 {
		t.Fatalf("expected two package refresh tasks, got %#v", result.Items)
	}
	operationID := serverTasks[0].OperationID
	if operationID == "" || serverTasks[1].OperationID != operationID {
		t.Fatalf("expected scheduled package refresh tasks to share operation, got %#v", serverTasks)
	}
	for _, task := range serverTasks {
		waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
	}
}

func TestExpireStaleQueuedWorkerTasksOnlyMarksOneShotWorkerTypes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	taskSvc := tasks.NewService(store.TaskDB())
	workerTask, err := taskSvc.Create(ctx, tasks.CreateInput{Type: "server_ufw_install", Summary: "Installing firewall"})
	if err != nil {
		t.Fatal(err)
	}
	scheduledTask, err := taskSvc.Create(ctx, tasks.CreateInput{Type: "package_refresh", Summary: "Refreshing packages"})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339Nano)
	for _, taskID := range []string{workerTask.ID, scheduledTask.ID} {
		if _, err := store.TaskDB().Exec(`UPDATE tasks SET created_at=? WHERE id=?`, old, taskID); err != nil {
			t.Fatal(err)
		}
	}
	sched := &Scheduler{tasks: taskSvc}

	sched.expireStaleQueuedWorkerTasks(ctx)

	gotWorker, err := taskSvc.Get(ctx, workerTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotWorker.Status != tasks.StatusFailed || !strings.Contains(gotWorker.Error, "worker startup timeout") {
		t.Fatalf("expected stale worker task to fail, got %#v", gotWorker)
	}
	gotScheduled, err := taskSvc.Get(ctx, scheduledTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotScheduled.Status != tasks.StatusQueued {
		t.Fatalf("scheduler-owned task should remain queued, got %#v", gotScheduled)
	}
}

func TestFailRunningTasksWithoutExecution(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	taskSvc := tasks.NewService(store.TaskDB())
	task, err := taskSvc.Create(ctx, tasks.CreateInput{Type: "server_restart", Summary: "Restarting server"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TaskDB().Exec(`UPDATE tasks SET status='running' WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	sched := &Scheduler{tasks: taskSvc}

	sched.failRunningTasksWithoutExecution(ctx)

	got, err := taskSvc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tasks.StatusFailed || !strings.Contains(got.Error, "no active execution") {
		t.Fatalf("expected orphaned running task to fail, got %#v", got)
	}
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

type fakeSchedulerAgentClient struct{}

func (fakeSchedulerAgentClient) Health(context.Context, string) (agentcontract.HealthResponse, error) {
	return agentcontract.HealthResponse{}, nil
}

func (fakeSchedulerAgentClient) OSRelease(context.Context, string) (linux.OSRelease, error) {
	return linux.OSRelease{}, nil
}

func (fakeSchedulerAgentClient) SystemTraits(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (fakeSchedulerAgentClient) MetricsSnapshot(context.Context, string, string) (linux.MetricsSnapshot, error) {
	return linux.MetricsSnapshot{
		CPUUsagePercent:  50,
		MemoryUsedBytes:  200,
		MemoryTotalBytes: 1000,
		DiskUsedBytes:    500,
		DiskTotalBytes:   2000,
		Status:           linux.SystemStatus{Hostname: "agent-host", LoadAverage: "0.1 0.2 0.3"},
	}, nil
}

func (fakeSchedulerAgentClient) UFWStatus(context.Context, string) (remoteops.UFWStatus, error) {
	return remoteops.UFWStatus{}, nil
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

type fakePackageExecutor struct{}

func (fakePackageExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (fakePackageExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{Stdout: "Listing...\nopenssl/stable-security 3.0.17-1 amd64 [upgradable from: 3.0.16-1]\n", ExitCode: 0}, nil
}

func (fakePackageExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (fakePackageExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func waitForTaskStatus(t *testing.T, taskSvc *tasks.Service, taskID string, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	task, err := taskSvc.Get(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("expected task %s to reach %s, got %#v", taskID, status, task)
}

func waitForMetricCount(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}, serverID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, serverID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var got int
	_ = db.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, serverID).Scan(&got)
	t.Fatalf("expected %d metrics snapshots for %s, got %d", want, serverID, got)
}

func waitForTotalMetricCount(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	var got int
	_ = db.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots`).Scan(&got)
	t.Fatalf("expected %d metrics snapshots, got %d", want, got)
}
