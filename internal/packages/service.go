package packages

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

type Service struct {
	db         *sql.DB
	servers    *server.Service
	exec       sshx.RemoteExecutor
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

func NewService(db *sql.DB, servers *server.Service, exec sshx.RemoteExecutor, taskSvc *tasks.Service) *Service {
	return &Service{db: db, servers: servers, exec: exec, tasks: taskSvc, refreshing: map[string]bool{}}
}

func (s *Service) List(ctx context.Context, serverID string) (UpdateList, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name,installed_version,candidate_version,source FROM package_updates WHERE server_id=? ORDER BY name`, serverID)
	if err != nil {
		return UpdateList{}, err
	}
	out := UpdateList{ServerID: serverID, Updates: []linux.PackageUpdate{}}
	for rows.Next() {
		var u linux.PackageUpdate
		if err := rows.Scan(&u.Name, &u.InstalledVersion, &u.CandidateVersion, &u.Source); err != nil {
			_ = rows.Close()
			return UpdateList{}, err
		}
		out.Updates = append(out.Updates, u)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return UpdateList{}, err
	}
	if err := rows.Close(); err != nil {
		return UpdateList{}, err
	}
	var ts sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT refreshed_at FROM package_refreshes WHERE server_id=?`, serverID).Scan(&ts)
	if ts.Valid {
		t, _ := time.Parse(time.RFC3339Nano, ts.String)
		out.LastRefreshedAt = &t
	}
	if out.LastRefreshedAt == nil || time.Since(*out.LastRefreshedAt) > 10*time.Minute {
		_, _ = s.refresh(ctx, serverID, "auto", true, "")
	}
	out.Refreshing = s.isRefreshing(serverID)
	return out, nil
}

func (s *Service) Refresh(ctx context.Context, serverID string) (RefreshResult, error) {
	return s.refresh(ctx, serverID, "user", false, "")
}

func (s *Service) RefreshScheduled(ctx context.Context, serverID string, operationID string) (RefreshResult, error) {
	return s.refresh(ctx, serverID, "scheduler", true, operationID)
}

func (s *Service) refresh(ctx context.Context, serverID string, triggerType string, skipRecentFailure bool, operationID string) (RefreshResult, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, false)
	if err != nil {
		return RefreshResult{}, err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return RefreshResult{}, err
	}
	if s.tasks == nil {
		return RefreshResult{}, panelerr.Validation("package_task_service_unavailable", "Package task service is unavailable")
	}
	if skipRecentFailure && s.hasRecentRefreshTask(ctx, serverID, 10*time.Minute) {
		return RefreshResult{ServerID: serverID, Refreshing: s.isRefreshing(serverID)}, nil
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, "package_refresh", "server", serverID); err != nil {
		return RefreshResult{}, err
	} else if ok {
		if existing.Status != tasks.StatusRunning && s.markRefreshing(serverID) {
			if err := s.startRefreshTask(ctx, existing, srv, adapter); err != nil {
				return RefreshResult{}, err
			}
		}
		return RefreshResult{ServerID: serverID, Refreshing: true, TaskID: existing.ID}, nil
	}
	if !s.markRefreshing(serverID) {
		return RefreshResult{ServerID: serverID, Refreshing: true}, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		OperationID:  operationID,
		Type:         "package_refresh",
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  triggerType,
		Summary:      "Refreshing package updates",
		MaxRetries:   0,
	})
	if err != nil {
		s.clearRefreshing(serverID)
		return RefreshResult{}, err
	}
	if err := s.startRefreshTask(ctx, task, srv, adapter); err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{ServerID: serverID, Refreshing: true, TaskID: task.ID}, nil
}

func (s *Service) RunRefreshTask(ctx context.Context, task tasks.Task) error {
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
	if task.Status == tasks.StatusRunning {
		return nil
	}
	if !s.markRefreshing(serverID) {
		return nil
	}
	return s.startRefreshTask(ctx, task, srv, adapter)
}

func (s *Service) startRefreshTask(ctx context.Context, task tasks.Task, srv server.Server, adapter packageAdapter) error {
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		s.clearRefreshing(srv.ID)
		return err
	}
	go s.runRefreshTask(s.tasks.ExecutionContext(task.ID), task, srv, adapter)
	return nil
}

func (s *Service) UpgradeSelected(ctx context.Context, serverID string, names []string) (tasks.Task, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return tasks.Task{}, err
	}
	if len(names) == 0 {
		return tasks.Task{}, panelerr.Validation("packages_required", "At least one package is required")
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         "package_upgrade_selected",
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Upgrading selected packages",
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runUpgradeSelected(s.tasks.ExecutionContext(task.ID), task.ID, srv, adapter, names)
	return task, nil
}

func (s *Service) UpgradeAll(ctx context.Context, serverID string) (tasks.Task, error) {
	srv, err := s.ensurePackageAllowed(ctx, serverID, true)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.adapterFor(srv)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         "package_upgrade_all",
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Upgrading all packages",
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runUpgradeAll(s.tasks.ExecutionContext(task.ID), task.ID, srv, adapter)
	return task, nil
}

func (s *Service) ensurePackageAllowed(ctx context.Context, serverID string, requireSudo bool) (server.Server, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, err
	}
	if !srv.OS.Supported {
		return server.Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if requireSudo && !srv.Sudo.Passwordless {
		return server.Server{}, panelerr.Validation("passwordless_sudo_required", "Passwordless sudo is required")
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
	updates, err := adapter.ListUpgradeable(ctx, s.exec, srv.Target())
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

func (s *Service) runUpgradeSelected(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter, names []string) {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "running", "upgrading selected packages")
	err := adapter.UpgradeSelected(ctx, s.exec, srv.Target(), names, taskLogSink{s.tasks, taskID})
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
	updates, err := adapter.ListUpgradeable(ctx, s.exec, srv.Target())
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Selected packages upgraded")
}

func (s *Service) runUpgradeAll(ctx context.Context, taskID string, srv server.Server, adapter packageAdapter) {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "running", "upgrading all packages")
	err := adapter.UpgradeAll(ctx, s.exec, srv.Target(), taskLogSink{s.tasks, taskID})
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing package cache after upgrade")
	updates, err := adapter.ListUpgradeable(ctx, s.exec, srv.Target())
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.replaceUpdates(ctx, srv.ID, updates); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "All packages upgraded")
}

func (s *Service) replaceUpdates(ctx context.Context, serverID string, updates []linux.PackageUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM package_updates WHERE server_id=?`, serverID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `INSERT INTO package_updates(server_id,name,installed_version,candidate_version,source,refreshed_at) VALUES(?,?,?,?,?,?)`, serverID, u.Name, u.InstalledVersion, u.CandidateVersion, u.Source, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO package_refreshes(server_id,refreshed_at) VALUES(?,?) ON CONFLICT(server_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, serverID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Counts(ctx context.Context) (map[string]int, map[string]*time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id, COUNT(*) FROM package_updates GROUP BY server_id`)
	if err != nil {
		return nil, nil, err
	}
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			rows.Close()
			return nil, nil, err
		}
		counts[id] = c
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT server_id, refreshed_at FROM package_refreshes`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	times := map[string]*time.Time{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, raw)
		times[id] = &t
	}
	return counts, times, rows.Err()
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
