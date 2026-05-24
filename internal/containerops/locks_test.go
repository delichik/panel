package containerops

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/containerservice"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestLeaseAcquireHeartbeatExpiryAndRelease(t *testing.T) {
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

	locks := NewLeaseService(store.AppDB(), time.Minute)
	ctx := context.Background()
	ok, err := locks.Acquire(ctx, "service", "svc_1", "task_1")
	if err != nil || !ok {
		t.Fatalf("first acquire = %v, %v", ok, err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_2")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second owner should not acquire live lease")
	}
	if err := locks.Heartbeat(ctx, "service", "svc_1", "task_1"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := store.AppDB().ExecContext(ctx, `UPDATE operation_locks SET expires_at=? WHERE scope=? AND resource_id=?`, expired, "service", "svc_1"); err != nil {
		t.Fatal(err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_2")
	if err != nil || !ok {
		t.Fatalf("expired lease should be acquirable = %v, %v", ok, err)
	}
	if err := locks.Release(ctx, "service", "svc_1", "task_2"); err != nil {
		t.Fatal(err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_3")
	if err != nil || !ok {
		t.Fatalf("released lease should be acquirable = %v, %v", ok, err)
	}
}

func TestWorkerRunsQueuedReconcileAndBlocksWhenNoNode(t *testing.T) {
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
	taskSvc := tasks.NewService(store.AppDB())
	serviceSvc := containerservice.NewService(store.AppDB(), taskSvc)
	item, err := serviceSvc.Create(ctx, containerservice.SaveRequest{Name: "api", Enabled: true, ComposeServiceYAML: "image: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskSvc.Get(ctx, item.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(taskSvc, NewLeaseService(store.AppDB(), time.Minute), serviceSvc, nilServerService(store), nil)
	if err := worker.RunNow(ctx, task); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := taskSvc.Get(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == tasks.StatusFailed {
			if got.Error == "" {
				t.Fatal("failed task should include error")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected reconcile task to be consumed and failed when no node is available")
}

func TestWorkerWritesPlacementAndDisableUsesPlacedNode(t *testing.T) {
	store := newOpsTestStore(t)
	ctx := context.Background()
	taskSvc := tasks.NewService(store.AppDB())
	serviceSvc := containerservice.NewService(store.AppDB(), taskSvc)
	serverSvc := nilServerService(store)
	nodeID := addOpsNode(t, store, "node-1")
	exec := &recordingExec{}
	worker := NewWorker(taskSvc, NewLeaseService(store.AppDB(), time.Minute), serviceSvc, serverSvc, exec)
	item, err := serviceSvc.Create(ctx, containerservice.SaveRequest{Name: "api", Enabled: true, ComposeServiceYAML: "image: nginx\n", Selector: map[string]string{"role": "web"}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskSvc.Get(ctx, item.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunNow(ctx, task); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
	placement, ok, err := serviceSvc.Placement(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || placement.NodeID != nodeID || placement.Generation != item.Generation || placement.SpecRevision != item.SpecRevision {
		t.Fatalf("unexpected placement: %#v ok=%v", placement, ok)
	}
	if _, err := store.AppDB().ExecContext(ctx, `UPDATE container_services SET selector_json=? WHERE id=?`, `{"role":"other"}`, item.ID); err != nil {
		t.Fatal(err)
	}
	preview, err := serviceSvc.Disable(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Tasks) != 1 {
		t.Fatalf("expected one disable task: %#v", preview.Tasks)
	}
	if err := worker.RunNow(ctx, preview.Tasks[0]); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, taskSvc, preview.Tasks[0].ID, tasks.StatusCompleted)
	_, ok, err = serviceSvc.Placement(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("disable should clear placement")
	}
	if !exec.contains("docker compose -p 'panel_managed' -f root.compose.yaml rm -sf 'api'") {
		t.Fatalf("disable did not run compose rm on placed node; commands: %#v", exec.commands())
	}
}

func TestWorkerRejectsConflictingPortClaims(t *testing.T) {
	store := newOpsTestStore(t)
	ctx := context.Background()
	taskSvc := tasks.NewService(store.AppDB())
	serviceSvc := containerservice.NewService(store.AppDB(), taskSvc)
	serverSvc := nilServerService(store)
	addOpsNode(t, store, "node-1")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload := `[{"name":"other","managed":true,"labels":{"panel.managed":"true","panel.service.name":"other","panel.claims.ports":"8080"}}]`
	if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO docker_runtime_cache(server_id,resource,payload,refreshed_at) VALUES(?,?,?,?)`, "node-1", "services", payload, now); err != nil {
		t.Fatal(err)
	}
	item, err := serviceSvc.Create(ctx, containerservice.SaveRequest{Name: "api", Enabled: true, ComposeServiceYAML: "image: nginx\nports:\n  - '8080:80'\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := taskSvc.Get(ctx, item.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(taskSvc, NewLeaseService(store.AppDB(), time.Minute), serviceSvc, serverSvc, &recordingExec{})
	if err := worker.RunNow(ctx, task); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, taskSvc, task.ID, tasks.StatusFailed)
	got, err := taskSvc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Error, "No eligible node") {
		t.Fatalf("expected port claim conflict to make node ineligible, got %q", got.Error)
	}
}

func nilServerService(store *storage.Store) *server.Service {
	return server.NewService(store.AppDB(), nil, tasks.NewService(store.AppDB()))
}

func newOpsTestStore(t *testing.T) *storage.Store {
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
	return store
}

func addOpsNode(t *testing.T, store *storage.Store, nodeID string) string {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,password_secret,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, nodeID+"-cred", "cred", "password", "root", "secret", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,credential_id,traits,os_supported,reachable,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, nodeID, "node", "127.0.0.1", 22, nodeID+"-cred", `{"role":"web"}`, 1, 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO docker_capabilities(server_id,docker_installed,docker_version,compose_installed,compose_version,include_supported,supported,last_checked_at) VALUES(?,?,?,?,?,?,?,?)`, nodeID, 1, "25", 1, "2", 1, 1, now); err != nil {
		t.Fatal(err)
	}
	return nodeID
}

func waitTaskStatus(t *testing.T, svc *tasks.Service, taskID, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := svc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return
		}
		if task.Status == tasks.StatusFailed && status != tasks.StatusFailed {
			t.Fatalf("task failed: %s", task.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", taskID, status)
}

type recordingExec struct {
	mu   sync.Mutex
	cmds []string
}

func (e *recordingExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	return e.record(command.Command), nil
}

func (e *recordingExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	return e.record(command.Command), nil
}

func (e *recordingExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error     { return nil }
func (e *recordingExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error { return nil }

func (e *recordingExec) record(cmd string) sshx.CommandResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cmds = append(e.cmds, cmd)
	if strings.Contains(cmd, "docker inspect") {
		return sshx.CommandResult{Stdout: ""}
	}
	return sshx.CommandResult{Stdout: "[]"}
}

func (e *recordingExec) contains(fragment string) bool {
	for _, cmd := range e.commands() {
		if strings.Contains(cmd, fragment) {
			return true
		}
	}
	return false
}

func (e *recordingExec) commands() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.cmds...)
}
