package packages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/ssh"
)

// Matches apt/remoteops package name whitelist (PKG-API-005).
var packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`)

const (
	// TaskPackageUpgradeSecurity 实时读取可升级列表后只升级安全更新
	// （PKG-API-011）。它不允许在安全包为空时退化为全部升级。
	TaskPackageUpgradeSecurity = "package_upgrade_security"
)

type Service struct {
	db         *sql.DB
	servers    *server.Service
	exec       sshx.RemoteExecutor
	agent      agentcontract.MaintenanceClient
	tasks      *tasks.Service
	adapter    packageAdapter
	refreshing map[string]bool
	mu         sync.Mutex
}

type UpdateList struct {
	ServerID        string                `json:"serverId"`
	LastRefreshedAt *time.Time            `json:"lastRefreshedAt"`
	Updates         []linux.PackageUpdate `json:"updates"`
	Refreshing      bool                  `json:"refreshing"`
}

type RefreshResult struct {
	ServerID   string `json:"serverId"`
	Refreshing bool   `json:"refreshing"`
	TaskID     string `json:"taskId,omitempty"`
}

type packageAdapter interface {
	ListUpgradeable(context.Context, sshx.RemoteExecutor, sshx.Target) ([]linux.PackageUpdate, error)
	UpgradeSelected(context.Context, sshx.RemoteExecutor, sshx.Target, []string, linux.LogSink) error
	UpgradeAll(context.Context, sshx.RemoteExecutor, sshx.Target, linux.LogSink) error
}

func NewService(db *sql.DB, servers *server.Service, exec sshx.RemoteExecutor, taskSvc *tasks.Service, agent ...agentcontract.MaintenanceClient) *Service {
	s := &Service{db: db, servers: servers, exec: exec, tasks: taskSvc, refreshing: map[string]bool{}}
	if len(agent) > 0 {
		s.agent = agent[0]
	}
	return s
}

func (s *Service) List(ctx context.Context, serverID string) (UpdateList, error) {
	var rows []models.PackageUpdate
	if err := orm.New(s.db).From("package_updates").Where("server_id = ?", serverID).OrderBy("name").All(ctx, &rows); err != nil {
		return UpdateList{}, err
	}
	out := UpdateList{ServerID: serverID, Updates: []linux.PackageUpdate{}}
	for _, row := range rows {
		out.Updates = append(out.Updates, linux.PackageUpdate{
			Name: row.Name, InstalledVersion: row.InstalledVersion, CandidateVersion: row.CandidateVersion,
			Source: row.Source,
			// 安全判定完全使用 Agent 的权威结果（apt 元数据 + Allowed-Origins）；
			// SecurityKnown=false 时 Security 恒为 false，绝不用 Source 字符串等
			// 启发式猜测（也防御历史脏行 security=1/security_known=0）。
			Security:      row.Security && row.SecurityKnown,
			SecurityKnown: row.SecurityKnown,
		})
	}
	var refresh models.PackageRefresh
	if err := orm.New(s.db).From("package_refreshes").Where("server_id = ?", serverID).First(ctx, &refresh); err == nil {
		t := refresh.RefreshedAt
		out.LastRefreshedAt = &t
	}
	if out.LastRefreshedAt == nil || time.Since(*out.LastRefreshedAt) > 10*time.Minute {
		_, _ = s.refresh(ctx, serverID, "auto", true, "")
	}
	out.Refreshing = s.isRefreshing(serverID)
	return out, nil
}

func (s *Service) Refresh(ctx context.Context, serverID string, triggerType ...string) (RefreshResult, error) {
	return s.refresh(ctx, serverID, upgradeTriggerType(triggerType), false, "")
}

// SaveReportedUpdates persists package update snapshots pushed by the agent
// after a dpkg change, keeping the resources page near real-time without a
// Panel-triggered refresh.
func (s *Service) SaveReportedUpdates(ctx context.Context, serverID string, updates []linux.PackageUpdate) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.replaceUpdates(ctx, serverID, updates); err != nil {
		return err
	}
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO package_refreshes(server_id,refreshed_at) VALUES(?,?) ON CONFLICT(server_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, serverID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Service) refresh(ctx context.Context, serverID string, triggerType string, skipRecentFailure bool, operationID string) (RefreshResult, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, false)
	if err != nil {
		return RefreshResult{}, err
	}
	if _, err := s.adapterFor(srv); err != nil {
		return RefreshResult{}, err
	}
	if s.tasks == nil {
		return RefreshResult{}, panelerr.Validation("package_task_service_unavailable", "Package task service is unavailable")
	}
	if skipRecentFailure && s.hasRecentRefreshTask(ctx, serverID, 10*time.Minute) {
		return RefreshResult{ServerID: serverID, Refreshing: s.isRefreshing(serverID)}, nil
	}
	if !s.markRefreshing(serverID) {
		return RefreshResult{ServerID: serverID, Refreshing: true}, nil
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		OperationID:  operationID,
		Type:         "package_refresh",
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  triggerType,
		Summary:      "Refreshing package updates",
		MaxRetries:   0,
	}, tasks.Trigger{Type: triggerType, Periodic: triggerType == "scheduler"})
	if err != nil {
		s.clearRefreshing(serverID)
		return RefreshResult{}, err
	}
	if !created {
		s.clearRefreshing(serverID)
		if task.Status != tasks.StatusRunning {
			if !s.markRefreshing(serverID) {
				return RefreshResult{ServerID: serverID, Refreshing: true, TaskID: task.ID}, nil
			}
			task, err = s.tasks.RunNow(ctx, task.ID)
			if err != nil {
				s.clearRefreshing(serverID)
				return RefreshResult{}, err
			}
			s.dispatchRegisteredTask(task)
		}
		return RefreshResult{ServerID: serverID, Refreshing: true, TaskID: task.ID}, nil
	}
	s.dispatchRegisteredTask(task)
	return RefreshResult{ServerID: serverID, Refreshing: true, TaskID: task.ID}, nil
}

func (s *Service) dispatchRegisteredTask(task tasks.Task) {
	if s == nil || s.tasks == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	go func() {
		defer s.tasks.FinishExecution(task.ID)
		_ = tasks.NewManager(s.tasks).Run(context.Background(), task)
	}()
}

func (s *Service) RunRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if s.tasks == nil {
		return panelerr.Validation("package_task_service_unavailable", "Package task service is unavailable")
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	srv, err := s.ensurePackageAllowed(ctx, serverID, false)
	if err != nil {
		if isNotFoundError(err) {
			_ = s.tasks.Cancel(ctx, task.ID, "Task cancelled because the server was removed")
		}
		return err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return err
	}
	// refresh() may already hold the in-process lock; claim it when recovering via Execute alone.
	if !s.isRefreshing(serverID) && !s.markRefreshing(serverID) {
		if s.tasks.HasRunningExecution(task.ID) {
			return nil
		}
		return panelerr.Conflict("package_maintenance_in_progress", "Package refresh is already running for this server")
	}
	s.runRefreshTask(ctx, task, srv, adapter)
	return nil
}

func (s *Service) UpgradeSelected(ctx context.Context, serverID string, names []string) (tasks.Task, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		return tasks.Task{}, err
	}
	if _, err := s.adapterFor(srv); err != nil {
		return tasks.Task{}, err
	}
	names, err = normalizeUpgradeNames(names)
	if err != nil {
		return tasks.Task{}, err
	}
	params, err := json.Marshal(map[string]any{"names": names})
	if err != nil {
		return tasks.Task{}, err
	}
	return s.createUpgradeTask(ctx, "package_upgrade_selected", "Upgrading selected packages", serverID, string(params), "user")
}

func (s *Service) UpgradeAll(ctx context.Context, serverID string, triggerType ...string) (tasks.Task, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		return tasks.Task{}, err
	}
	if _, err := s.adapterFor(srv); err != nil {
		return tasks.Task{}, err
	}
	return s.createUpgradeTask(ctx, "package_upgrade_all", "Upgrading all packages", serverID, "", upgradeTriggerType(triggerType))
}

// UpgradeSecurity 创建并运行安全包升级任务（PKG-API-011）。安全分类在任务
// 执行时实时读取，因此这里不检查当前是否存在安全更新：任务在空集合上会直接
// 完成，绝不退化为全部升级。
func (s *Service) UpgradeSecurity(ctx context.Context, serverID string, triggerType ...string) (tasks.Task, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		return tasks.Task{}, err
	}
	if _, err := s.adapterFor(srv); err != nil {
		return tasks.Task{}, err
	}
	return s.createUpgradeTask(ctx, TaskPackageUpgradeSecurity, "Upgrading security packages", serverID, "", upgradeTriggerType(triggerType))
}

func (s *Service) createUpgradeTask(ctx context.Context, taskType, summary, serverID, paramsJSON, triggerType string) (tasks.Task, error) {
	task, _, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         taskType,
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  triggerType,
		Summary:      summary,
		ParamsJSON:   paramsJSON,
	}, tasks.Trigger{Type: triggerType, Manual: triggerType == "user"})
	if err != nil {
		return tasks.Task{}, err
	}
	return task, nil
}

// upgradeTriggerType 归一自动/手动触发类型：自动更新协调任务传 scheduler，
// 既有手动入口默认 user。
func upgradeTriggerType(values []string) string {
	if len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		return strings.TrimSpace(values[0])
	}
	return "user"
}

func (s *Service) ensurePackageAllowed(ctx context.Context, serverID string, requireSudo bool) (server.Server, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, err
	}
	if !srv.OS.Supported {
		return server.Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	// PKG-API-002: production (Agent client wired) must refuse before creating a
	// task when the node has no compatible Agent; tests may leave agent nil and
	// exercise the SSH adapter path.
	if s.agent != nil {
		if _, err := packageAgentURL(srv); err != nil {
			return server.Server{}, err
		}
		return srv, nil
	}
	if requireSudo && !srv.Privilege.Privileged && srv.Privilege.Mode != sshx.PrivilegeModeRoot &&
		srv.Privilege.Mode != sshx.PrivilegeModeSudo && !srv.Sudo.Passwordless {
		return server.Server{}, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required")
	}
	return srv, nil
}

func (s *Service) adapterFor(srv server.Server) (packageAdapter, error) {
	if s.adapter != nil {
		return s.adapter, nil
	}
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok {
		return nil, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	return adapter, nil
}

func (s *Service) runRefreshTask(ctx context.Context, task tasks.Task, srv server.Server, adapter packageAdapter) {
	defer s.tasks.FinishExecution(task.ID)
	defer s.clearRefreshing(srv.ID)
	if err := ctx.Err(); err != nil {
		return
	}
	_ = s.tasks.Advance(ctx, task.ID, "running", "refreshing package updates")
	updates, err := s.listUpgradeable(ctx, srv, adapter)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, "Package updates refreshed")
}

func (s *Service) runUpgradeSelected(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter, names []string) error {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "running", "upgrading selected packages")
	if err := s.upgradePackages(ctx, taskID, srv, adapter, names, false); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
	updates, err := s.listUpgradeable(ctx, srv, adapter)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	return s.tasks.Complete(ctx, taskID, "Selected packages upgraded")
}

func (s *Service) runUpgradeAll(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter) error {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "running", "upgrading all packages")
	if err := s.upgradePackages(ctx, taskID, srv, adapter, nil, true); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
	updates, err := s.listUpgradeable(ctx, srv, adapter)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	return s.tasks.Complete(ctx, taskID, "All packages upgraded")
}

// RunUpgradeSelectedTask 是 package_upgrade_selected 的注册 executor：在
// manager/worker 执行边界内完成升级并维护 per-server 互斥。重启后遗留的
// queued 任务也能被 worker 正常恢复执行。
func (s *Service) RunUpgradeSelectedTask(tc tasks.TaskContext) error {
	return s.runUpgradeTask(tc, false)
}

// RunUpgradeAllTask 是 package_upgrade_all 的注册 executor。
func (s *Service) RunUpgradeAllTask(tc tasks.TaskContext) error {
	return s.runUpgradeTask(tc, true)
}

func (s *Service) runUpgradeTask(tc tasks.TaskContext, all bool) error {
	ctx, task := tc.Context, tc.Task
	if s.tasks == nil {
		return panelerr.Validation("package_task_service_unavailable", "Package task service is unavailable")
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		if isNotFoundError(err) {
			_ = s.tasks.Cancel(ctx, task.ID, "Task cancelled because the server was removed")
		}
		return err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return err
	}
	if task.Status == tasks.StatusRunning && s.tasks.HasRunningExecution(task.ID) {
		return nil
	}
	// 与刷新共用 per-server 维护互斥：刷新或其它升级进行中时不再并发执行。
	if !s.markRefreshing(serverID) {
		return panelerr.Conflict("package_maintenance_in_progress", "Package maintenance is already running for this server")
	}
	defer s.clearRefreshing(serverID)
	if all {
		return s.runUpgradeAll(ctx, task.ID, srv, adapter)
	}
	names, err := upgradeNames(task)
	if err != nil {
		return err
	}
	return s.runUpgradeSelected(ctx, task.ID, srv, adapter, names)
}

// RunUpgradeSecurityTask 是 package_upgrade_security 的注册 executor：复用
// per-server 维护互斥，执行时实时读取可升级列表并按 Agent 权威安全判定过滤，
// 只走 names 升级路径（PKG-API-011）。未知判定不会被猜测升级；安全包为空时
// 直接完成，绝不升级全部。
func (s *Service) RunUpgradeSecurityTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if s.tasks == nil {
		return panelerr.Validation("package_task_service_unavailable", "Package task service is unavailable")
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		if isNotFoundError(err) {
			_ = s.tasks.Cancel(ctx, task.ID, "Task cancelled because the server was removed")
		}
		return err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return err
	}
	if task.Status == tasks.StatusRunning && s.tasks.HasRunningExecution(task.ID) {
		return nil
	}
	// 与刷新/其它升级共用 per-server 维护互斥。
	if !s.markRefreshing(serverID) {
		return panelerr.Conflict("package_maintenance_in_progress", "Package maintenance is already running for this server")
	}
	defer s.clearRefreshing(serverID)
	return s.runUpgradeSecurity(ctx, task.ID, srv, adapter)
}

func (s *Service) runUpgradeSecurity(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter) error {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return err
	}
	if s.agent != nil {
		_ = s.tasks.Advance(ctx, taskID, "running", "upgrading security packages")
		baseURL, err := packageAgentURL(srv)
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return err
		}
		result, err := s.agent.UpgradePackages(ctx, baseURL, agentcontract.PackageUpgradeRequest{SecurityOnly: true})
		if strings.TrimSpace(result.Output) != "" {
			_ = taskLogSink{s.tasks, taskID}.AppendLog(ctx, "stdout", result.Output)
		}
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return err
		}
		_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
		remaining, err := s.listUpgradeable(ctx, srv, adapter)
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return err
		}
		if err := s.replaceUpdates(ctx, srv.ID, remaining); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return err
		}
		if strings.Contains(result.Output, "No security updates available") {
			return s.tasks.Complete(ctx, taskID, "No security updates available")
		}
		return s.tasks.Complete(ctx, taskID, "Security packages upgraded")
	}

	_ = s.tasks.Advance(ctx, taskID, "running", "listing security package updates")
	updates, err := s.listUpgradeable(ctx, srv, adapter)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	names, unknown := securityPackageSelection(updates)
	if len(unknown) > 0 {
		// 未知判定绝不启发式猜测升级：跳过并记录 warning，任务继续处理已知安全包。
		_ = s.tasks.AppendLog(ctx, taskID, "warning", fmt.Sprintf(
			"Skipping %d package(s) with unknown security status: %s",
			len(unknown), strings.Join(unknown, ", "),
		))
	}
	if len(names) == 0 {
		// 没有安全更新也要刷新缓存：实时列表可能已清空旧记录。
		if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return err
		}
		return s.tasks.Complete(ctx, taskID, "No security updates available")
	}
	_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("Upgrading %d security package(s)", len(names)))
	if err := s.upgradePackages(ctx, taskID, srv, adapter, names, false); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
	remaining, err := s.listUpgradeable(ctx, srv, adapter)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	if err := s.replaceUpdates(ctx, srv.ID, remaining); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return err
	}
	return s.tasks.Complete(ctx, taskID, "Security packages upgraded")
}

// securityPackageSelection 选择安全更新包：只有 Agent 权威判定
// （SecurityKnown=true 且 Security=true）的包才升级。未知包单独返回，供调用方
// 记录 warning 后跳过；未知绝不能用 Source 等启发式猜测。
func securityPackageSelection(updates []linux.PackageUpdate) (names, unknown []string) {
	names = make([]string, 0, len(updates))
	for _, update := range updates {
		name := strings.TrimSpace(update.Name)
		if name == "" {
			continue
		}
		switch {
		case !update.SecurityKnown:
			unknown = append(unknown, name)
		case update.Security:
			names = append(names, name)
		}
	}
	return names, unknown
}

func upgradeNames(task tasks.Task) ([]string, error) {
	raw := strings.TrimSpace(task.ParamsJSON)
	if raw == "" || raw == "{}" {
		return nil, panelerr.Validation("packages_required", "At least one package is required")
	}
	var payload struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return normalizeUpgradeNames(payload.Names)
}

func normalizeUpgradeNames(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !packageNamePattern.MatchString(name) {
			return nil, panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, panelerr.Validation("packages_required", "At least one package is required")
	}
	return out, nil
}

func (s *Service) listUpgradeable(ctx context.Context, srv server.Server, adapter packageAdapter) ([]linux.PackageUpdate, error) {
	if s.agent == nil {
		return adapter.ListUpgradeable(ctx, s.exec, server.Target(srv))
	}
	baseURL, err := packageAgentURL(srv)
	if err != nil {
		return nil, err
	}
	return s.agent.PackageUpdates(ctx, baseURL)
}

func (s *Service) upgradePackages(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter, names []string, all bool) error {
	if s.agent == nil {
		if all {
			return adapter.UpgradeAll(ctx, s.exec, server.Target(srv), taskLogSink{s.tasks, taskID})
		}
		return adapter.UpgradeSelected(ctx, s.exec, server.Target(srv), names, taskLogSink{s.tasks, taskID})
	}
	baseURL, err := packageAgentURL(srv)
	if err != nil {
		return err
	}
	result, err := s.agent.UpgradePackages(ctx, baseURL, agentcontract.PackageUpgradeRequest{Names: names, All: all})
	if strings.TrimSpace(result.Output) != "" {
		_ = taskLogSink{s.tasks, taskID}.AppendLog(ctx, "stdout", result.Output)
	}
	return err
}

func packageAgentURL(srv server.Server) (string, error) {
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return "", panelerr.Validation("agent_incompatible", "Agent is not compatible with package maintenance")
	}
	baseURL := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if baseURL == "" {
		return "", panelerr.Validation("agent_required", "Agent is required for package maintenance")
	}
	return baseURL, nil
}

func (s *Service) replaceUpdates(ctx context.Context, serverID string, updates []linux.PackageUpdate) error {
	now := time.Now().UTC()
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).From("package_updates").Where("server_id = ?", serverID).Delete(ctx); err != nil {
			return err
		}
		records := make([]models.PackageUpdate, 0, len(updates))
		for _, u := range updates {
			records = append(records, models.PackageUpdate{
				ServerID: serverID, Name: u.Name, InstalledVersion: u.InstalledVersion,
				CandidateVersion: u.CandidateVersion, Source: u.Source, RefreshedAt: now,
				// 持久化 Agent 权威判定；SecurityKnown=false 表示不可判定，List 与
				// 安全升级任务都会保持 unknown（跳过），绝不回退启发式。
				Security: u.Security, SecurityKnown: u.SecurityKnown,
			})
		}
		if err := orm.New(tx).InsertBatch(ctx, records); err != nil {
			return err
		}
		if _, err := orm.RawExec(ctx, tx, `INSERT INTO package_refreshes(server_id,refreshed_at) VALUES(?,?) ON CONFLICT(server_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, serverID, now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return nil
	})
}

func (s *Service) Counts(ctx context.Context) (map[string]int, map[string]*time.Time, error) {
	var grouped []serverUpdateCount
	if err := orm.New(s.db).From("package_updates").SelectExpr("server_id, COUNT(*) AS count").GroupBy("server_id").All(ctx, &grouped); err != nil {
		return nil, nil, err
	}
	counts := map[string]int{}
	for _, row := range grouped {
		counts[row.ServerID] = int(row.Count)
	}
	var refreshes []models.PackageRefresh
	if err := orm.New(s.db).From("package_refreshes").All(ctx, &refreshes); err != nil {
		return nil, nil, err
	}
	times := map[string]*time.Time{}
	for _, row := range refreshes {
		t := row.RefreshedAt
		times[row.ServerID] = &t
	}
	return counts, times, nil
}

type serverUpdateCount struct {
	ServerID string `orm:"column:server_id"`
	Count    int64  `orm:"column:count"`
}

type taskLogSink struct {
	tasks  *tasks.Service
	taskID string
}

func (s taskLogSink) AppendLog(ctx context.Context, stream, line string) error {
	return s.tasks.AppendLog(ctx, s.taskID, stream, line)
}

func (s *Service) markRefreshing(serverID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshing[serverID] {
		return false
	}
	s.refreshing[serverID] = true
	return true
}

func (s *Service) clearRefreshing(serverID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.refreshing, serverID)
}

func (s *Service) isRefreshing(serverID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshing[serverID]
}

func (s *Service) hasRecentRefreshTask(ctx context.Context, serverID string, window time.Duration) bool {
	if s.tasks == nil {
		return false
	}
	result, err := s.tasks.List(ctx, tasks.ListFilter{Type: "package_refresh", ServerID: serverID, Limit: 1})
	if err != nil || len(result.Items) == 0 {
		return false
	}
	latest := result.Items[0]
	if time.Since(latest.CreatedAt) > window {
		return false
	}
	return true
}

func isNotFoundError(err error) bool {
	var pe *panelerr.Error
	return errors.As(err, &pe) && pe.Code == "not_found"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
