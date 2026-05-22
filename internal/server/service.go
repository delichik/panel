package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/id"
	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

const connectivitySudoTimeout = 8 * time.Second
const connectivityTaskType = "server_connectivity_test"
const connectivityResourceType = "server"
const connectivityMaxRetries = 8
const connectivityStaleAfter = 10 * time.Minute

type Service struct {
	db      *sql.DB
	exec    sshx.RemoteExecutor
	tasks   *tasks.Service
	adapter linux.DebianAdapter
}

func NewService(db *sql.DB, exec sshx.RemoteExecutor, taskSvc *tasks.Service) *Service {
	return &Service{db: db, exec: exec, tasks: taskSvc, adapter: linux.DebianAdapter{}}
}

func (s *Service) Create(ctx context.Context, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	now := time.Now().UTC()
	srv := Server{
		ID:           id.New("srv"),
		Name:         req.Name,
		Host:         req.Host,
		Port:         req.Port,
		SSHUsername:  req.SSHUsername,
		CredentialID: req.CredentialID,
		Traits:       req.Traits,
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if srv.Traits == nil {
		srv.Traits = map[string]string{}
	}
	traits, _ := json.Marshal(srv.Traits)
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, srv.CredentialID, string(traits), srv.Notes, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		_, _ = s.EnsureConnectivityTask(ctx, srv.ID, true)
	}
	return srv, nil
}

func (s *Service) Update(ctx context.Context, serverID string, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	if req.Traits == nil {
		req.Traits = map[string]string{}
	}
	traits, _ := json.Marshal(req.Traits)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET name=?,host=?,port=?,ssh_username=?,credential_id=?,traits=?,notes=?,updated_at=? WHERE id=?`,
		req.Name, req.Host, req.Port, req.SSHUsername, req.CredentialID, string(traits), req.Notes, now, serverID)
	if err != nil {
		return Server{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Server{}, panelerr.NotFound("server")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		_, _ = s.EnsureConnectivityTask(ctx, serverID, true)
	}
	return srv, nil
}

func (s *Service) Delete(ctx context.Context, serverID string) error {
	var running int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE server_id=? AND status IN ('queued','running')`, serverID).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return panelerr.Conflict("server_has_running_tasks", "Server has running tasks")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("server")
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,traits,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		if s.exec != nil && (srv.LastCheckedAt == nil || time.Since(*srv.LastCheckedAt) > connectivityStaleAfter) {
			_, _ = s.EnsureConnectivityTask(ctx, srv.ID, false)
		}
		var load sql.NullString
		_ = s.db.QueryRowContext(ctx, `SELECT load_average FROM metrics_snapshots WHERE server_id=? ORDER BY time DESC LIMIT 1`, srv.ID).Scan(&load)
		if load.Valid {
			srv.LoadAverage = load.String
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, serverID string) (Server, error) {
	srv, err := scanServer(s.db.QueryRowContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,traits,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers WHERE id=?`, serverID))
	if err == sql.ErrNoRows {
		return Server{}, panelerr.NotFound("server")
	}
	if err == nil {
		var load sql.NullString
		_ = s.db.QueryRowContext(ctx, `SELECT load_average FROM metrics_snapshots WHERE server_id=? ORDER BY time DESC LIMIT 1`, srv.ID).Scan(&load)
		if load.Valid {
			srv.LoadAverage = load.String
		}
	}
	return srv, err
}

func (s *Service) TestConnectivity(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.EnsureConnectivityTask(ctx, serverID, true)
}

func (s *Service) EnsureConnectivityTask(ctx context.Context, serverID string, runNow bool) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, connectivityTaskType, connectivityResourceType, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		if runNow && existing.Status != tasks.StatusRunning {
			existing, err = s.tasks.RunNow(ctx, existing.ID)
			if err != nil {
				return tasks.Task{}, err
			}
			s.startConnectivityTask(existing, srv)
		}
		return existing, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         connectivityTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		Summary:      "Testing SSH connectivity",
		MaxRetries:   connectivityMaxRetries,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if runNow {
		s.startConnectivityTask(task, srv)
	}
	return task, nil
}

func (s *Service) RunDueConnectivityTests(ctx context.Context) error {
	servers, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, srv := range servers {
		task, ok, err := s.tasks.FirstRunnable(ctx, connectivityTaskType, connectivityResourceType, srv.ID)
		if err != nil {
			return err
		}
		if ok {
			s.startConnectivityTask(task, srv)
		}
	}
	return nil
}

func (s *Service) startConnectivityTask(task tasks.Task, srv Server) {
	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		s.runConnectivityTest(taskCtx, task.ID, srv)
	}()
}

func (s *Service) runConnectivityTest(ctx context.Context, taskID string, srv Server) {
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	_ = s.tasks.Advance(ctx, taskID, "connecting", "connecting to server")
	osInfo, err := linux.Detect(ctx, s.exec, target)
	if err != nil {
		_ = s.markCheck(ctx, srv.ID, false, linux.OSRelease{}, false, nil, err.Error())
		_ = s.tasks.FailRetryable(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "checking passwordless sudo")
	sudoRes, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0
	if sudoErr != nil {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "passwordless sudo unavailable: "+sudoErr.Error())
	}

	sysTraits := map[string]string{}
	if osInfo.Supported {
		_ = s.tasks.Advance(ctx, taskID, "traits", "discovering server system traits")
		detected, traitsErr := s.detectSystemTraits(ctx, target)
		if traitsErr == nil {
			sysTraits = detected
		} else {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "failed to detect system traits: "+traitsErr.Error())
		}
		sysTraits["sys.os"] = strings.ToLower(osInfo.ID + "-" + osInfo.VersionID)
	}

	if !osInfo.Supported {
		_ = s.markCheck(ctx, srv.ID, true, osInfo, passwordless, sysTraits, "unsupported distribution")
		_ = s.tasks.Complete(ctx, taskID, "Connected, but distribution is unsupported")
		return
	}

	_ = s.markCheck(ctx, srv.ID, true, osInfo, passwordless, sysTraits, "")
	if passwordless {
		_ = s.tasks.Complete(ctx, taskID, "Connectivity test passed")
	} else {
		_ = s.tasks.Complete(ctx, taskID, "Connected, passwordless sudo unavailable")
	}
}

func (s *Service) detectSystemTraits(ctx context.Context, target sshx.Target) (map[string]string, error) {
	cmd := `sh -lc 'echo "cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"; ` +
		`echo "mem=$(grep MemTotal /proc/meminfo 2>/dev/null | awk "{print \$2}" | awk "{print int(\$1/1024)}" || echo 0)"; ` +
		`echo "disk=$(df -m / 2>/dev/null | awk "NR==2{print \$2}" | awk "{print int(\$1/1024)}" || echo 0)"; ` +
		`echo "hostname=$(hostname 2>/dev/null || echo unknown)"'`

	res, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: cmd, Timeout: 12 * time.Second})
	if err != nil {
		return nil, err
	}

	traits := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "cores":
			traits["sys.cpu_cores"] = value
		case "mem":
			traits["sys.memory_total_mb"] = value
		case "disk":
			traits["sys.disk_total_gb"] = value
		case "hostname":
			traits["sys.hostname"] = value
		}
	}
	return traits, nil
}

func (s *Service) markCheck(ctx context.Context, serverID string, reachable bool, osInfo linux.OSRelease, sudo bool, sysTraits map[string]string, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 首先拉取已有的 traits (包含用户打的 custom.env 等自定义特征)
	var rawTraits string
	err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	current := map[string]string{}
	if rawTraits != "" {
		_ = json.Unmarshal([]byte(rawTraits), &current)
	}

	// 剔除之前所有的 sys. 特征，用本次新探测到的 sysTraits 覆盖
	for k := range current {
		if strings.HasPrefix(k, "sys.") {
			delete(current, k)
		}
	}
	for k, v := range sysTraits {
		current[k] = v
	}

	traitsJSON, _ := json.Marshal(current)

	_, err = s.db.ExecContext(ctx, `UPDATE servers SET reachable=?,os_id=?,os_version_id=?,os_pretty_name=?,os_supported=?,sudo_passwordless=?,sudo_last_checked_at=?,last_checked_at=?,traits=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(reachable), osInfo.ID, osInfo.VersionID, osInfo.PrettyName, boolInt(osInfo.Supported), boolInt(sudo), now, now, string(traitsJSON), msg, now, serverID)
	return err
}

func (srv Server) Target() sshx.Target {
	return sshx.Target{ServerID: srv.ID, Host: srv.Host, Port: srv.Port, Username: srv.SSHUsername, CredentialID: srv.CredentialID}
}

func validateSave(req SaveRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.CredentialID) == "" {
		return panelerr.Validation("server_invalid", "Server name, host, and credentialId are required")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return panelerr.Validation("server_port_invalid", "Server port must be between 1 and 65535")
	}
	return nil
}

type serverScanner interface{ Scan(dest ...any) error }

func scanServer(row serverScanner) (Server, error) {
	var srv Server
	var traits, created, updated string
	var osSupported, reachable, sudo int
	var sudoAt, checkedAt sql.NullString
	err := row.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUsername, &srv.CredentialID, &traits, &srv.Notes, &srv.OS.ID, &srv.OS.VersionID, &srv.OS.PrettyName, &osSupported, &reachable, &sudo, &sudoAt, &checkedAt, &srv.LastError, &created, &updated)
	if err != nil {
		return Server{}, err
	}
	srv.Traits = map[string]string{}
	_ = json.Unmarshal([]byte(traits), &srv.Traits)
	srv.OS.Supported = osSupported == 1
	srv.Reachable = reachable == 1
	srv.Sudo.Passwordless = sudo == 1
	if sudoAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, sudoAt.String)
		srv.Sudo.LastCheckedAt = &v
	}
	if checkedAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, checkedAt.String)
		srv.LastCheckedAt = &v
	}
	srv.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	srv.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return srv, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
