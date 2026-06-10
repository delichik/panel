package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
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
const serverInfoTaskType = "server_info_collect"
const connectivityResourceType = "server"
const connectivityMaxRetries = 8
const connectivityStaleAfter = 10 * time.Minute
const ufwInstallTaskType = "server_ufw_install"
const ufwInstallTimeout = 5 * time.Minute
const reverseProxyEnabledTrait = "nomad.reverse_proxy.enabled"

var reverseProxyTCPPorts = []int{80, 443}

type Service struct {
	db    *sql.DB
	exec  sshx.RemoteExecutor
	tasks *tasks.Service
}

func NewService(db *sql.DB, exec sshx.RemoteExecutor, taskSvc *tasks.Service) *Service {
	return &Service{db: db, exec: exec, tasks: taskSvc}
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
		_, _ = s.EnsureInitialInfoTask(ctx, srv.ID, true)
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
		applyDistroSystemTraits(srv.OS, srv.Traits)
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
		applyDistroSystemTraits(srv.OS, srv.Traits)
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

func (s *Service) InstallUFW(ctx context.Context, serverID string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if !srv.OS.Supported {
		return tasks.Task{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !srv.Reachable {
		return tasks.Task{}, panelerr.Validation("server_not_reachable", "Server connectivity has not been confirmed")
	}
	if !srv.Sudo.Passwordless {
		return tasks.Task{}, panelerr.Validation("passwordless_sudo_required", "Passwordless sudo is required")
	}
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok {
		return tasks.Task{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !adapter.SupportsUFW() {
		return tasks.Task{}, panelerr.Validation("ufw_not_supported", "UFW is not supported on this distribution")
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, ufwInstallTaskType, connectivityResourceType, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         ufwInstallTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Installing UFW",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runInstallUFW(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *Service) ProbeConnectivity(ctx context.Context, req SaveRequest) (ProbeResult, error) {
	if s.exec == nil {
		return ProbeResult{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	if err := validateProbe(req); err != nil {
		return ProbeResult{}, err
	}

	target := sshx.Target{
		Host:         req.Host,
		Port:         req.Port,
		Username:     req.SSHUsername,
		CredentialID: req.CredentialID,
	}
	probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	osInfo, err := linux.Detect(probeCtx, s.exec, target)
	if err != nil {
		return ProbeResult{Reachable: false, Error: err.Error(), Traits: map[string]string{}}, nil
	}

	rootRes, rootErr := s.exec.Exec(probeCtx, target, sshx.CommandSpec{Command: "id -u", Timeout: connectivitySudoTimeout})
	root := rootErr == nil && strings.TrimSpace(rootRes.Stdout) == "0"

	sudoRes, sudoErr := s.exec.ExecSudo(probeCtx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0

	sysTraits := map[string]string{}
	if osInfo.Supported {
		if detected, traitsErr := s.detectSystemTraits(probeCtx, target); traitsErr == nil {
			sysTraits = detected
		}
	}
	applyDistroSystemTraits(osInfo, sysTraits)

	result := ProbeResult{
		Reachable:        true,
		PasswordlessSudo: passwordless,
		Root:             root,
		Privileged:       root || passwordless,
		OS:               osInfo,
		Traits:           sysTraits,
	}
	if sudoErr != nil && !root {
		result.PasswordlessSudoText = sudoErr.Error()
	}
	if !osInfo.Supported {
		result.Error = "unsupported distribution"
	}
	return result, nil
}

func (s *Service) EnsureConnectivityTask(ctx context.Context, serverID string, runNow bool) (tasks.Task, error) {
	return s.ensureConnectivityTask(ctx, serverID, runNow, connectivityTaskType, "Testing SSH connectivity")
}

func (s *Service) EnsureInitialInfoTask(ctx context.Context, serverID string, runNow bool) (tasks.Task, error) {
	return s.ensureConnectivityTask(ctx, serverID, runNow, serverInfoTaskType, "Collecting server information")
}

func (s *Service) RunConnectivityTask(ctx context.Context, task tasks.Task) error {
	if s.exec == nil {
		return panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	serverID := task.ServerID
	if serverID == "" {
		serverID = task.ResourceID
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return err
	}
	s.startConnectivityTask(task, srv)
	return nil
}

func (s *Service) ensureConnectivityTask(ctx context.Context, serverID string, runNow bool, taskType string, summary string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if existing, ok, err := s.existingActiveConnectivityTask(ctx, serverID); err != nil {
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
		Type:         taskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		Summary:      summary,
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

func (s *Service) existingActiveConnectivityTask(ctx context.Context, serverID string) (tasks.Task, bool, error) {
	var latest tasks.Task
	found := false
	for _, taskType := range []string{serverInfoTaskType, connectivityTaskType} {
		task, ok, err := s.tasks.ExistingActive(ctx, taskType, connectivityResourceType, serverID)
		if err != nil {
			return tasks.Task{}, false, err
		}
		if !ok {
			continue
		}
		if !found || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
			found = true
		}
	}
	return latest, found, nil
}

func (s *Service) RunDueConnectivityTests(ctx context.Context) error {
	servers, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, srv := range servers {
		for _, taskType := range []string{serverInfoTaskType, connectivityTaskType} {
			task, ok, err := s.tasks.FirstRunnable(ctx, taskType, connectivityResourceType, srv.ID)
			if err != nil {
				return err
			}
			if ok {
				s.startConnectivityTask(task, srv)
				break
			}
		}
	}
	return nil
}

func (s *Service) startConnectivityTask(task tasks.Task, srv Server) {
	_ = s.tasks.Start(context.Background(), task.ID)
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
	}
	applyDistroSystemTraits(osInfo, sysTraits)

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

func (s *Service) runInstallUFW(ctx context.Context, taskID string, srv Server, adapter linux.DistroAdapter) {
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	_ = s.tasks.Advance(ctx, taskID, "installing", "installing UFW")
	if err := s.execSudoLogged(ctx, taskID, target, ufwInstallScript(adapter, srv), ufwInstallTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}

	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing server system traits")
	osInfo, err := linux.Detect(ctx, s.exec, target)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	sudoRes, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0
	sysTraits := map[string]string{}
	if osInfo.Supported {
		detected, traitsErr := s.detectSystemTraits(ctx, target)
		if traitsErr == nil {
			sysTraits = detected
		} else {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "failed to detect system traits: "+traitsErr.Error())
		}
	}
	applyDistroSystemTraits(osInfo, sysTraits)
	msg := ""
	if !osInfo.Supported {
		msg = "unsupported distribution"
	}
	if err := s.markCheck(ctx, srv.ID, true, osInfo, passwordless, sysTraits, msg); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "UFW installed")
}

func (s *Service) detectSystemTraits(ctx context.Context, target sshx.Target) (map[string]string, error) {
	cmd := `sh -lc 'echo "cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"; ` +
		`echo "mem=$(grep MemTotal /proc/meminfo 2>/dev/null | awk "{print \$2}" | awk "{print int(\$1/1024)}" || echo 0)"; ` +
		`echo "disk=$(df -m / 2>/dev/null | awk "NR==2{print \$2}" | awk "{print int(\$1/1024)}" || echo 0)"; ` +
		`echo "hostname=$(hostname 2>/dev/null || echo unknown)"; ` +
		`if command -v ufw >/dev/null 2>&1; then echo "ufw_installed=true"; if systemctl is-active --quiet ufw 2>/dev/null || ufw status 2>/dev/null | grep -qi "^Status: active"; then echo "ufw_active=true"; else echo "ufw_active=false"; fi; else echo "ufw_installed=false"; echo "ufw_active=false"; fi'`

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
		case "ufw_installed":
			traits["sys.ufw_installed"] = value
		case "ufw_active":
			traits["sys.ufw_active"] = value
		}
	}
	return traits, nil
}

func (s *Service) execSudoLogged(ctx context.Context, taskID string, target sshx.Target, command string, timeout time.Duration) error {
	stdoutStreamed := false
	stderrStreamed := false
	res, err := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{
		Command: command,
		Timeout: timeout,
		OnStdout: func(line string) {
			stdoutStreamed = true
			_ = s.tasks.AppendLog(ctx, taskID, "stdout", line)
		},
		OnStderr: func(line string) {
			stderrStreamed = true
			_ = s.tasks.AppendLog(ctx, taskID, "stderr", line)
		},
	})
	if !stdoutStreamed {
		s.appendCommandOutput(ctx, taskID, "stdout", res.Stdout)
	}
	if !stderrStreamed {
		s.appendCommandOutput(ctx, taskID, "stderr", res.Stderr)
	}
	return err
}

func (s *Service) appendCommandOutput(ctx context.Context, taskID, stream, out string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			_ = s.tasks.AppendLog(ctx, taskID, stream, line)
		}
	}
}

func ufwInstallScript(adapter linux.DistroAdapter, srv Server) string {
	command := strings.TrimSpace(adapter.UFWInstallScript())
	ports := []int{normalizedTCPPort(srv.Port)}
	if traitEnabled(srv.Traits[reverseProxyEnabledTrait]) {
		ports = append(ports, reverseProxyTCPPorts...)
	}
	seen := map[int]struct{}{}
	for _, port := range ports {
		if _, ok := seen[port]; ok {
			continue
		}
		seen[port] = struct{}{}
		command += "\nufw allow " + strconv.Itoa(port) + "/tcp"
	}
	return command
}

func normalizedTCPPort(port int) int {
	if port <= 0 || port > 65535 {
		return 22
	}
	return port
}

func traitEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func applyDistroSystemTraits(osInfo linux.OSRelease, traits map[string]string) {
	if traits == nil {
		return
	}
	if osInfo.Supported {
		traits["sys.os"] = strings.ToLower(osInfo.ID + "-" + osInfo.VersionID)
	}
	if adapter, ok := linux.AdapterFor(osInfo); ok {
		traits["sys.ufw_supported"] = boolString(adapter.SupportsUFW())
	} else if osInfo.ID != "" || osInfo.VersionID != "" || osInfo.PrettyName != "" {
		traits["sys.ufw_supported"] = "false"
	}
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

func validateProbe(req SaveRequest) error {
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.CredentialID) == "" {
		return panelerr.Validation("server_invalid", "Server host and credentialId are required")
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

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
