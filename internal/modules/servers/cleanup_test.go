package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	sshx "panel/internal/platform/ssh"
)

type cleanupFakeExec struct {
	mu           sync.Mutex
	sudoCommands []string
	sudoErr      error
	blockCh      chan struct{}
	startedCh    chan struct{}
}

func (f *cleanupFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *cleanupFakeExec) ExecSudo(_ context.Context, _ sshx.Target, spec sshx.CommandSpec) (sshx.CommandResult, error) {
	f.mu.Lock()
	started := f.startedCh
	block := f.blockCh
	f.sudoCommands = append(f.sudoCommands, spec.Command)
	err := f.sudoErr
	f.mu.Unlock()

	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if block != nil {
		<-block
	}
	if err != nil {
		return sshx.CommandResult{ExitCode: 1, Stderr: err.Error()}, err
	}
	return sshx.CommandResult{Stdout: "[panel] server cleanup completed\n", ExitCode: 0}, nil
}

func (f *cleanupFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *cleanupFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func hasErrorCode(err error, code string) bool {
	var pe *panelerr.Error
	return errors.As(err, &pe) && pe.Code == code
}

func createPrivilegedTestServer(t *testing.T, svc *Service, taskSvc *tasks.Service, store *storage.Store, name, ip string) Server {
	t.Helper()
	ctx := context.Background()
	srv, err := svc.Create(ctx, SaveRequest{Name: name, IPv4: ip, Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if srv.InitialTaskID != "" {
		_ = waitTaskTerminal(t, taskSvc, srv.InitialTaskID)
	}
	if _, err := store.AppDB().ExecContext(ctx, `UPDATE servers SET privilege_mode='root' WHERE id=?`, srv.ID); err != nil {
		t.Fatal(err)
	}
	return srv
}

func (f *cleanupFakeExec) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.sudoCommands))
	copy(out, f.sudoCommands)
	return out
}

func TestServerCleanupScriptCoversRequiredStepsAndTimeoutBudget(t *testing.T) {
	worstCaseSeconds := serverCleanupDockerTimeoutSeconds + serverCleanupServiceOpBoundSeconds
	if worstCaseSeconds >= int(serverCleanupTimeout.Seconds()) {
		t.Fatalf("expected target worst-case timeout (%ds) < Panel SSH timeout (%ds)", worstCaseSeconds, int(serverCleanupTimeout.Seconds()))
	}

	script := serverCleanupScript(serverCleanupOptions{
		ServerID:     "srv_1",
		DockerHost:   agentcontract.DefaultDockerHost,
		StorageRoots: []string{"/srv/panel-storage", "/etc", "/var/lib"},
	})

	agentStopIdx := strings.Index(script, "systemctl stop panel-agent.service")
	agentRemoveIdx := strings.Index(script, "rm -f '/etc/systemd/system/panel-agent.service' '/usr/local/bin/panel-agent'")
	dockerRmIdx := strings.Index(script, "docker ps -aq --filter label=panel.application.managed=true")
	panelDataIdx := strings.Index(script, "rm -rf '/etc/panel-agent' /opt/panel /var/lib/panel-agent")
	if agentStopIdx < 0 || agentRemoveIdx < 0 || dockerRmIdx < 0 || panelDataIdx < 0 {
		t.Fatalf("missing required cleanup steps in script:\n%s", script)
	}
	if !(agentStopIdx < agentRemoveIdx && agentRemoveIdx < dockerRmIdx && dockerRmIdx < panelDataIdx) {
		t.Fatalf("unexpected cleanup ordering: stop=%d remove=%d docker=%d panelData=%d", agentStopIdx, agentRemoveIdx, dockerRmIdx, panelDataIdx)
	}

	requiredSnippets := []string{
		"docker rm -f",
		"docker network rm panel-apps",
		"# panel-storage-share:managed",
		"rm -rf '/srv/panel-storage/srv_1'",
		"/tmp/panel-agent-*",
		"99-panel-mptcp-mitigation.conf",
		"panel-timesync.conf",
		"/etc/panel-storage-roots.json",
		"/etc/panel-storage-nfs-hosts.json",
	}
	for _, want := range requiredSnippets {
		if !strings.Contains(script, want) {
			t.Fatalf("expected cleanup script to contain %q\nscript:\n%s", want, script)
		}
	}
	for _, forbidden := range []string{
		"apt_get",
		"apt-get",
		"docker.io",
		"ufw --force disable",
		"docker system prune",
		"systemctl stop docker",
		"systemctl stop nfs-kernel-server",
		"/var/lib/docker",
		"/etc/srv_1",
		"/var/lib/srv_1",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("cleanup script must not touch %q\nscript:\n%s", forbidden, script)
		}
	}

	remoteDockerScript := serverCleanupScript(serverCleanupOptions{
		ServerID:   "srv_1",
		DockerHost: "tcp://10.0.0.5:2375",
	})
	if !strings.Contains(remoteDockerScript, "DOCKER_HOST='tcp://10.0.0.5:2375'") {
		t.Fatalf("expected custom DOCKER_HOST in remote cleanup script:\n%s", remoteDockerScript)
	}
}

func TestDeleteWithCleanupSucceedsAndRemovesLocalServer(t *testing.T) {
	exec := &cleanupFakeExec{}
	svc, taskSvc, store := testServerService(t, exec)
	ctx := context.Background()

	srv := createPrivilegedTestServer(t, svc, taskSvc, store, "cleanup-node", "127.0.0.10")
	taskSvc.MustRegister(tasks.Definition{Type: "test_cancellable"})
	siblingTask, err := taskSvc.Create(ctx, tasks.CreateInput{
		Type:         "test_cancellable",
		ServerID:     srv.ID,
		ResourceType: "server",
		ResourceID:   srv.ID,
		Status:       tasks.StatusRunning,
		Summary:      "Sibling cancellable task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,job_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"app_cleanup_test", "app", 1, "name: app\nimage: nginx\n", "selected", `["`+srv.ID+`"]`, 1, "job_cleanup_1", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO event_runs(id,type,operation_id,execution_id,application_id,server_id,action,status,desired_generation,desired_spec_hash,desired_spec_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"job_cleanup_1", "application_deploy", "op-1", "exec-1", "app_cleanup_test", srv.ID, "apply", "running", 1, "hash", "{}", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	task, err := svc.DeleteWithCleanup(ctx, srv.ID)
	if err != nil {
		t.Fatalf("DeleteWithCleanup failed: %v", err)
	}
	done := waitTaskTerminal(t, taskSvc, task.ID)
	if done.Status != tasks.StatusCompleted {
		t.Fatalf("expected cleanup task completed, got status=%s err=%s", done.Status, done.Error)
	}
	if len(exec.commands()) == 0 {
		t.Fatal("expected privileged SSH cleanup script to run")
	}
	if _, err := svc.Get(ctx, srv.ID); !hasErrorCode(err, "not_found") {
		t.Fatalf("expected server to be deleted from DB, got err=%v", err)
	}
	gotSibling, err := taskSvc.Get(ctx, siblingTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSibling.Status != tasks.StatusCancelled {
		t.Fatalf("expected sibling task to be cancelled, got %s", gotSibling.Status)
	}
	var deployJobCount int
	if err := store.AppDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM event_runs WHERE id='job_cleanup_1'`).Scan(&deployJobCount); err != nil {
		t.Fatal(err)
	}
	if deployJobCount != 0 {
		t.Fatalf("expected deploy job row removed during local deletion, got %d", deployJobCount)
	}
}

func TestDeleteWithCleanupFailsClosedOnRemoteError(t *testing.T) {
	exec := &cleanupFakeExec{sudoErr: errors.New("apt purge exit 100")}
	svc, taskSvc, store := testServerService(t, exec)
	ctx := context.Background()

	srv := createPrivilegedTestServer(t, svc, taskSvc, store, "cleanup-fail", "127.0.0.11")

	task, err := svc.DeleteWithCleanup(ctx, srv.ID)
	if err != nil {
		t.Fatalf("DeleteWithCleanup returned error: %v", err)
	}
	done := waitTaskTerminal(t, taskSvc, task.ID)
	if done.Status != tasks.StatusFailed {
		t.Fatalf("expected cleanup task failed, got %s", done.Status)
	}
	if !strings.Contains(done.Error, "Failed to clean server data before removal") {
		t.Fatalf("expected cleanup error summary, got %q", done.Error)
	}
	if _, err := svc.Get(ctx, srv.ID); err != nil {
		t.Fatalf("expected server to remain in DB after remote cleanup failure, got %v", err)
	}
}

func TestDeleteWithCleanupRejectsConcurrentRunningTask(t *testing.T) {
	exec := &cleanupFakeExec{
		blockCh:   make(chan struct{}),
		startedCh: make(chan struct{}),
	}
	svc, taskSvc, store := testServerService(t, exec)
	ctx := context.Background()

	srv := createPrivilegedTestServer(t, svc, taskSvc, store, "cleanup-busy", "127.0.0.12")

	first, err := svc.DeleteWithCleanup(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-exec.startedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("first cleanup did not start executing")
	}

	if _, err := svc.DeleteWithCleanup(ctx, srv.ID); !hasErrorCode(err, "server_cleanup_delete_in_progress") {
		close(exec.blockCh)
		t.Fatalf("expected conflict error server_cleanup_delete_in_progress, got %v", err)
	}
	close(exec.blockCh)
	done := waitTaskTerminal(t, taskSvc, first.ID)
	if done.Status != tasks.StatusCompleted {
		t.Fatalf("expected blocked cleanup to complete once released, got %s (%s)", done.Status, done.Error)
	}
}

func TestDeleteHandlerCleanDataParam(t *testing.T) {
	exec := &cleanupFakeExec{}
	svc, taskSvc, store := testServerService(t, exec)
	handler := NewHandler(svc)
	ctx := context.Background()

	// Default delete (no cleanData) removes locally returning 204 without SSH.
	srvLocal, err := svc.Create(ctx, SaveRequest{Name: "local-only", IPv4: "127.0.0.21", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	reqLocal := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/"+srvLocal.ID, nil)
	reqLocal.SetPathValue("id", srvLocal.ID)
	recLocal := httptest.NewRecorder()
	handler.Delete(recLocal, reqLocal)
	if recLocal.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for local delete, got %d (%s)", recLocal.Code, recLocal.Body.String())
	}
	if len(exec.commands()) != 0 {
		t.Fatal("local delete must not run SSH commands")
	}

	// Invalid cleanData parameter returns 400 server_delete_invalid.
	reqBad := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/any-id?cleanData=maybe", nil)
	reqBad.SetPathValue("id", "any-id")
	recBad := httptest.NewRecorder()
	handler.Delete(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest || !strings.Contains(recBad.Body.String(), "server_delete_invalid") {
		t.Fatalf("expected 400 server_delete_invalid, got %d (%s)", recBad.Code, recBad.Body.String())
	}

	// cleanData=true returns 202 with taskId and completes remote cleanup + local removal.
	srvClean := createPrivilegedTestServer(t, svc, taskSvc, store, "remote-clean", "127.0.0.22")
	reqClean := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/"+srvClean.ID+"?cleanData=true", nil)
	reqClean.SetPathValue("id", srvClean.ID)
	recClean := httptest.NewRecorder()
	handler.Delete(recClean, reqClean)
	if recClean.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for cleanData=true delete, got %d (%s)", recClean.Code, recClean.Body.String())
	}
	var resp struct {
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recClean.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.TaskID == "" {
		t.Fatalf("expected non-empty taskId in 202 response: %s", recClean.Body.String())
	}
	done := waitTaskTerminal(t, taskSvc, resp.Data.TaskID)
	if done.Status != tasks.StatusCompleted {
		t.Fatalf("expected cleanup delete task completed, got %s (%s)", done.Status, done.Error)
	}
	if _, err := svc.Get(ctx, srvClean.ID); !hasErrorCode(err, "not_found") {
		t.Fatalf("expected cleaned server to be deleted from DB, got err=%v", err)
	}
}
