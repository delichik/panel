package server

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	serverCleanupDeleteTaskType = "server_cleanup_delete"

	// serverCleanupTimeout bounds the remote cleanup SSH step.
	// Target-side worst case:
	//   serverCleanupDockerTimeoutSeconds (removing Panel-managed containers & network)
	// + serverCleanupServiceOpBoundSeconds (panel-agent removal/exports/Panel files)
	// = 120 + 60 = 180s < 300s (5m).
	serverCleanupTimeout               = 5 * time.Minute
	serverCleanupDockerTimeoutSeconds  = 120
	serverCleanupServiceOpBoundSeconds = 60
)

var (
	cleanupStorageRootPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	cleanupDeniedRootPrefixes = []string{
		"/etc", "/var", "/usr", "/bin", "/sbin", "/lib", "/boot",
		"/dev", "/proc", "/sys", "/run", "/home", "/root", "/tmp",
	}
)

type serverCleanupOptions struct {
	ServerID     string
	DockerHost   string
	StorageRoots []string
}

// DeleteWithCleanup schedules and starts a background task that removes Panel
// Agent, Panel-managed containers, certificates, and Panel data from the
// target server via privileged SSH before removing the server from inventory.
func (s *Service) DeleteWithCleanup(ctx context.Context, serverID string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         serverCleanupDeleteTaskType,
		ServerID:     srv.ID,
		ResourceType: connectivityResourceType,
		ResourceID:   srv.ID,
		TriggerType:  "user",
		TriggeredBy:  "user",
		Summary:      "Cleaning up and removing server " + srv.Name,
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		if task.Status == tasks.StatusRunning {
			return tasks.Task{}, panelerr.Conflict("server_cleanup_delete_in_progress", "Server cleanup and removal task is already running")
		}
		task, err = s.tasks.RunNow(ctx, task.ID)
		if err != nil {
			return tasks.Task{}, err
		}
	}
	s.dispatchRegisteredTask(task)
	return s.awaitTaskRunning(ctx, task.ID, 2*time.Second)
}

func serverCleanupDeleteConcurrencyKey(in tasks.CreateInput) string {
	serverID := firstNonEmpty(in.ResourceID, in.ServerID, in.NodeID)
	if serverID == "" {
		return ""
	}
	return "server_cleanup_delete:" + serverID
}

// RunServerCleanupDeleteTask executes remote cleanup over privileged SSH and,
// only after remote cleanup succeeds, removes the server from local storage.
func (s *Service) RunServerCleanupDeleteTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	defer s.tasks.FinishExecution(task.ID)
	if s.exec == nil {
		return panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return panelerr.Validation("server_id_required", "Server ID is required")
	}
	if task.Status != tasks.StatusRunning {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		if isNotFoundError(err) {
			_ = s.tasks.Complete(ctx, task.ID, "Server already removed")
			return nil
		}
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	_ = s.tasks.Advance(ctx, task.ID, "preparing", "checking server connectivity and privilege")
	if !hasPrivilege(srv) {
		refreshed, privErr := s.refreshPrivilegeForDeploy(ctx, srv)
		if privErr != nil {
			_ = s.tasks.Fail(ctx, task.ID, privErr)
			return nil
		}
		srv = refreshed
	}
	if !hasPrivilege(srv) {
		_ = s.tasks.Fail(ctx, task.ID, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required to clean up server data"))
		return nil
	}
	if _, cancelErr := s.tasks.CancelByServerExcept(ctx, serverID, task.ID, "Task cancelled because server cleanup and removal started"); cancelErr != nil {
		logging.From(ctx).Warn("cancel pending tasks before server cleanup failed", zap.String("server_id", serverID), zap.Error(cancelErr))
	}
	if err := cancelActiveServerDeployJobs(ctx, s.db, serverID); err != nil {
		logging.From(ctx).Warn("cancel active deploy jobs before server cleanup failed", zap.String("server_id", serverID), zap.Error(err))
	}

	dockerHost := normalizeDockerHost(srv.DockerHost)
	opts := serverCleanupOptions{
		ServerID:     srv.ID,
		DockerHost:   dockerHost,
		StorageRoots: serverStorageCleanupRoots(ctx, s.db, srv.ID),
	}
	script := serverCleanupScript(opts)
	_ = s.tasks.Advance(ctx, task.ID, "cleaning", "cleaning Panel containers, certificates, agent, and panel data")
	runner := remoteops.Runner{Exec: s.exec, Target: serverTarget(srv), Log: serverTaskLogSink{s.tasks, task.ID}}
	if _, err := runner.RunSudoLogged(ctx, script, serverCleanupTimeout); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, panelerr.BadGateway("server_cleanup_failed", "Failed to clean server data before removal: "+err.Error()))
		return nil
	}

	_ = s.tasks.Advance(ctx, task.ID, "removing", "removing server from inventory")
	if err := s.deleteLocal(ctx, serverID, task.ID); err != nil && !isNotFoundError(err) {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return nil
	}
	_ = s.tasks.Complete(ctx, task.ID, "Server data cleaned and server removed")
	return nil
}

func cancelActiveServerDeployJobs(ctx context.Context, db *sql.DB, serverID string) error {
	if db == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := orm.RawExec(ctx, db, `UPDATE event_runs SET status='cancelled',stage='cancelled',lease_owner='',lease_token='',lease_expires_at=NULL,next_run_at=NULL,finished_at=?,updated_at=?,error_code='server_removed',error_class='cancelled',error_message='Server cleanup and removal started',error='',error_detail='' WHERE type='application_deploy' AND server_id=? AND status IN ('queued','running','failed_retryable')`,
		now, now, serverID)
	return err
}

func serverStorageCleanupRoots(ctx context.Context, db *sql.DB, serverID string) []string {
	if db == nil || strings.TrimSpace(serverID) == "" {
		return nil
	}
	var rows []models.StorageShareConfig
	if err := orm.New(db).From("storage_share_configs").Where("servers_json<>?", "").All(ctx, &rows); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, row := range rows {
		for _, item := range row.ServersJSON {
			id, _ := item["serverId"].(string)
			root, _ := item["root"].(string)
			if strings.TrimSpace(id) != serverID {
				continue
			}
			root = strings.TrimSpace(root)
			if !validCleanupStorageRoot(root) {
				continue
			}
			if _, ok := seen[root]; ok {
				continue
			}
			seen[root] = struct{}{}
			out = append(out, root)
		}
	}
	sort.Strings(out)
	return out
}

func validCleanupStorageRoot(root string) bool {
	if !cleanupStorageRootPattern.MatchString(root) || strings.Contains(root, "..") || strings.Contains(root, "//") {
		return false
	}
	root = strings.TrimRight(root, "/")
	if root == "" {
		return false
	}
	for _, prefix := range cleanupDeniedRootPrefixes {
		if root == prefix || strings.HasPrefix(root, prefix+"/") {
			return false
		}
	}
	return true
}

func serverCleanupScript(opts serverCleanupOptions) string {
	lines := []string{
		`panel_timeout() {`,
		`  seconds="$1"`,
		`  shift`,
		`  if command -v timeout >/dev/null 2>&1; then`,
		`    timeout "$seconds" "$@"`,
		`    return $?`,
		`  fi`,
		`  "$@"`,
		`}`,
		`panel_docker_timeout() { panel_timeout ` + fmt.Sprintf("%d", serverCleanupDockerTimeoutSeconds) + ` "$@"; }`,
		`echo "[panel] stopping and removing panel-agent service"`,
		`if command -v systemctl >/dev/null 2>&1; then`,
		`  systemctl stop panel-agent.service >/dev/null 2>&1 || true`,
		`  systemctl disable panel-agent.service >/dev/null 2>&1 || true`,
		`fi`,
		`if command -v pkill >/dev/null 2>&1; then`,
		`  pkill -x panel-agent >/dev/null 2>&1 || true`,
		`  pkill -f '^/usr/local/bin/panel-agent($| )' >/dev/null 2>&1 || true`,
		`fi`,
		`rm -f ` + remoteops.ShellQuote(agentRemoteServicePath) + ` ` + remoteops.ShellQuote(agentRemoteBinaryPath),
		`if command -v systemctl >/dev/null 2>&1; then`,
		`  systemctl daemon-reload >/dev/null 2>&1 || true`,
		`  systemctl reset-failed panel-agent.service >/dev/null 2>&1 || true`,
		`fi`,
		`echo "[panel] removing Panel-managed containers"`,
		`if command -v docker >/dev/null 2>&1; then`,
	}
	dockerEnv := ""
	if strings.TrimSpace(opts.DockerHost) != "" && strings.TrimSpace(opts.DockerHost) != agentcontract.DefaultDockerHost {
		dockerEnv = "DOCKER_HOST=" + remoteops.ShellQuote(strings.TrimSpace(opts.DockerHost)) + " "
	}
	lines = append(lines,
		`  containers="$(`+dockerEnv+`panel_docker_timeout docker ps -aq --filter label=panel.application.managed=true 2>/dev/null || true)"`,
		`  if [ -n "$containers" ]; then`,
		`    `+dockerEnv+`panel_docker_timeout docker rm -f $containers >/dev/null 2>&1 || true`,
		`  fi`,
		`  `+dockerEnv+`panel_docker_timeout docker network rm panel-apps >/dev/null 2>&1 || true`,
		`fi`,
		`echo "[panel] cleaning Panel storage exports and partitions"`,
		`if [ -f /etc/exports ] && grep -q '^# panel-storage-share:managed' /etc/exports 2>/dev/null; then`,
		`  tmp_exports="$(mktemp)"`,
		`  awk '/^# panel-storage-share:managed/{skip=1; next} skip && /^# \/panel-storage-share:managed/{skip=0; next} !skip{print}' /etc/exports > "$tmp_exports" && mv "$tmp_exports" /etc/exports || rm -f "$tmp_exports"`,
		`  if command -v exportfs >/dev/null 2>&1; then exportfs -ra >/dev/null 2>&1 || true; fi`,
		`fi`,
	)
	safeServerID := strings.TrimSpace(opts.ServerID)
	if safeServerID != "" && !strings.ContainsAny(safeServerID, `/\`) && !strings.Contains(safeServerID, "..") {
		for _, root := range opts.StorageRoots {
			if !validCleanupStorageRoot(root) {
				continue
			}
			partitionDir := strings.TrimRight(root, "/") + "/" + safeServerID
			lines = append(lines, `rm -rf `+remoteops.ShellQuote(partitionDir))
		}
	}
	lines = append(lines,
		`echo "[panel] removing certificates, agent config, and Panel data"`,
		`rm -rf `+remoteops.ShellQuote(agentRemoteConfigDir)+` /opt/panel /var/lib/panel-agent`,
		`rm -f /tmp/panel-agent-* `+remoteops.ShellQuote(agentRemoteMPTCPSysctlPath)+` `+remoteops.ShellQuote(agentTimeSyncConfigPath)+` /etc/panel-storage-roots.json /etc/panel-storage-nfs-hosts.json`,
		`if command -v sysctl >/dev/null 2>&1; then sysctl --system >/dev/null 2>&1 || true; fi`,
		`echo "[panel] server cleanup completed"`,
	)
	return strings.Join(lines, "\n")
}
