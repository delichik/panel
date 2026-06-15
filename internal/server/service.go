package server

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/id"
	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/remoteops"
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
const ufwEnableTaskType = "server_ufw_enable"
const ufwInstallTimeout = 5 * time.Minute
const ufwManageTimeout = time.Minute
const restartTaskType = "server_restart"
const restartTimeout = 15 * time.Second
const agentDeployTaskType = "server_agent_deploy"
const agentDeployTimeout = 2 * time.Minute
const reverseProxyEnabledTrait = "agent.reverse_proxy.enabled"
const defaultAgentListenAddress = "0.0.0.0:9443"
const defaultAgentPort = "9443"
const agentRemoteBinaryPath = "/usr/local/bin/panel-agent"
const agentRemoteConfigDir = "/etc/panel-agent"
const agentRemoteServicePath = "/etc/systemd/system/panel-agent.service"
const agentBundleRoot = "/app/panel-agents"
const agentBundleBinaryName = "panel-agent"

var reverseProxyTCPPorts = []int{80, 443}

type Service struct {
	db        *sql.DB
	metricsDB *sql.DB
	exec      sshx.RemoteExecutor
	agent     agent.Client
	agentTLS  *agent.TLSAssets
	tasks     *tasks.Service
}

func NewService(db *sql.DB, exec sshx.RemoteExecutor, taskSvc *tasks.Service) *Service {
	return &Service{db: db, exec: exec, tasks: taskSvc}
}

func (s *Service) SetMetricsDB(db *sql.DB) {
	s.metricsDB = db
}

func (s *Service) SetAgentClient(client agent.Client) {
	s.agent = client
}

func (s *Service) SetAgentTLSAssets(assets *agent.TLSAssets) {
	s.agentTLS = assets
}

func (s *Service) IssueAgentCertificate(ctx context.Context, serverID string) (AgentCertificateBundle, error) {
	if s.agentTLS == nil {
		return AgentCertificateBundle{}, panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return AgentCertificateBundle{}, err
	}
	cert, err := s.agentTLS.IssueServerCertificate("panel-agent-"+srv.ID, []string{srv.Host})
	if err != nil {
		return AgentCertificateBundle{}, err
	}
	agentURL := strings.TrimSpace(srv.Traits[agent.TraitURL])
	if agentURL == "" {
		agentURL = "https://" + srv.Host + ":" + defaultAgentPort
	}
	return AgentCertificateBundle{
		CA:            string(s.agentTLS.CAPEM),
		Certificate:   string(cert.CertPEM),
		PrivateKey:    string(cert.KeyPEM),
		ListenAddress: defaultAgentListenAddress,
		AgentURL:      agentURL,
		DockerHost:    normalizeDockerHost(srv.DockerHost),
	}, nil
}

func (s *Service) DeployAgent(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.ensureAgentDeployTask(ctx, serverID, "user", true)
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
		DockerHost:   normalizeDockerHost(req.DockerHost),
		Traits:       req.Traits,
		Variables:    normalizeServerVariables(req.Variables, req.Traits),
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if srv.Traits == nil {
		srv.Traits = map[string]string{}
	}
	traits, _ := json.Marshal(srv.Traits)
	variables, _ := json.Marshal(srv.Variables)
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, srv.CredentialID, srv.DockerHost, string(traits), string(variables), srv.Notes, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		task, err := s.EnsureInitialInfoTask(ctx, srv.ID, true)
		if err != nil {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, srv.ID)
			return Server{}, err
		}
		srv.InitialTaskID = task.ID
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
	variables, _ := json.Marshal(normalizeServerVariables(req.Variables, req.Traits))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET name=?,host=?,port=?,ssh_username=?,credential_id=?,docker_host=?,traits=?,variables_json=?,notes=?,updated_at=? WHERE id=?`,
		req.Name, req.Host, req.Port, req.SSHUsername, req.CredentialID, normalizeDockerHost(req.DockerHost), string(traits), string(variables), req.Notes, now, serverID)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	operationID := ""
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		applyDistroSystemTraits(srv.OS, srv.Traits)
		if s.exec != nil && (srv.LastCheckedAt == nil || time.Since(*srv.LastCheckedAt) > connectivityStaleAfter) {
			if operationID == "" {
				operationID = id.New("op")
			}
			_, _ = s.ensureConnectivityTask(ctx, srv.ID, false, connectivityTaskType, "Testing SSH connectivity", operationID)
		}
		srv.LoadAverage = s.latestLoadAverage(ctx, srv.ID)
		out = append(out, srv)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, serverID string) (Server, error) {
	srv, err := scanServer(s.db.QueryRowContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers WHERE id=?`, serverID))
	if err == sql.ErrNoRows {
		return Server{}, panelerr.NotFound("server")
	}
	if err == nil {
		applyDistroSystemTraits(srv.OS, srv.Traits)
		srv.LoadAverage = s.latestLoadAverage(ctx, srv.ID)
	}
	return srv, err
}

func (s *Service) CheckConfiguredAgents(ctx context.Context) {
	if s.agent == nil {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return
	}
	var serverIDs []string
	for rows.Next() {
		var serverID string
		if err := rows.Scan(&serverID); err != nil {
			_ = rows.Close()
			return
		}
		serverIDs = append(serverIDs, serverID)
	}
	_ = rows.Close()
	for _, serverID := range serverIDs {
		srv, err := s.Get(ctx, serverID)
		if err != nil {
			continue
		}
		if _, ok := agentURL(srv); !ok {
			_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
			continue
		}
		if err := s.checkAgent(ctx, srv); err != nil {
			if s.handleAgentCertificateTimeError(ctx, srv, err) {
				continue
			}
			_ = s.markAgentStatus(ctx, srv.ID, agent.StatusUnavailable, "", err.Error())
			continue
		}
		updated, err := s.Get(ctx, srv.ID)
		if err != nil {
			continue
		}
		if updated.Traits[agent.TraitStatus] == agent.StatusIncompatible {
			_, _ = s.ensureAgentDeployTask(context.Background(), updated.ID, "system", true)
		}
	}
}

func (s *Service) latestLoadAverage(ctx context.Context, serverID string) string {
	if s.metricsDB == nil {
		return ""
	}
	var load sql.NullString
	err := s.metricsDB.QueryRowContext(ctx, `SELECT load_average FROM metrics_snapshots WHERE server_id=? ORDER BY time DESC LIMIT 1`, serverID).Scan(&load)
	if err != nil || !load.Valid {
		return ""
	}
	return load.String
}

func (s *Service) TestConnectivity(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.EnsureConnectivityTask(ctx, serverID, true)
}

func (s *Service) UFWState(ctx context.Context, serverID string) (UFWState, error) {
	srv, err := s.ensureUFWManageable(ctx, serverID)
	if err != nil {
		return UFWState{}, err
	}
	status, err := s.fetchUFWStatus(ctx, srv)
	if err != nil {
		return UFWState{}, err
	}
	return ufwStateFromStatus(srv.ID, true, status), nil
}

func (s *Service) AllowUFW(ctx context.Context, serverID string, req UFWAllowRequest) (UFWState, error) {
	srv, err := s.ensureUFWManageable(ctx, serverID)
	if err != nil {
		return UFWState{}, err
	}
	if err := s.ensureUFWInstalled(ctx, srv); err != nil {
		return UFWState{}, err
	}
	script, err := remoteops.UFWAllowScript([]remoteops.UFWRule{{Port: req.Port, Protocol: req.Protocol, From: req.From}})
	if err != nil {
		return UFWState{}, err
	}
	if _, err := (remoteops.Runner{Exec: s.exec, Target: srv.Target()}).RunSudoLogged(ctx, script, ufwManageTimeout); err != nil {
		return UFWState{}, err
	}
	status, err := s.fetchUFWStatusSSH(ctx, srv)
	if err != nil {
		return UFWState{}, err
	}
	return ufwStateFromStatus(srv.ID, true, status), nil
}

func (s *Service) EnableUFW(ctx context.Context, serverID string) (tasks.Task, error) {
	srv, err := s.ensureUFWManageable(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok || !adapter.SupportsUFW() {
		return tasks.Task{}, panelerr.Validation("ufw_not_supported", "UFW is not supported on this distribution")
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, ufwEnableTaskType, connectivityResourceType, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         ufwEnableTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Enabling UFW",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runEnableUFW(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *Service) DeleteUFWRule(ctx context.Context, serverID string, number int) (UFWState, error) {
	srv, err := s.ensureUFWManageable(ctx, serverID)
	if err != nil {
		return UFWState{}, err
	}
	if err := s.ensureUFWInstalled(ctx, srv); err != nil {
		return UFWState{}, err
	}
	script, err := remoteops.UFWDeleteRuleScript(number)
	if err != nil {
		return UFWState{}, err
	}
	if _, err := (remoteops.Runner{Exec: s.exec, Target: srv.Target()}).RunSudoLogged(ctx, script, ufwManageTimeout); err != nil {
		return UFWState{}, err
	}
	status, err := s.fetchUFWStatus(ctx, srv)
	if err != nil {
		return UFWState{}, err
	}
	return ufwStateFromStatus(srv.ID, true, status), nil
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
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	go s.runInstallUFW(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *Service) Restart(ctx context.Context, serverID string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if !srv.Reachable {
		return tasks.Task{}, panelerr.Validation("server_not_reachable", "Server connectivity has not been confirmed")
	}
	if !srv.Sudo.Passwordless {
		return tasks.Task{}, panelerr.Validation("passwordless_sudo_required", "Passwordless sudo is required")
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, restartTaskType, connectivityResourceType, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         restartTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Restarting server",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRestart(context.Background(), task.ID, srv)
	return task, nil
}

func (s *Service) ensureUFWManageable(ctx context.Context, serverID string) (Server, error) {
	if s.exec == nil {
		return Server{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	if !srv.OS.Supported {
		return Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !srv.Reachable {
		return Server{}, panelerr.Validation("server_not_reachable", "Server connectivity has not been confirmed")
	}
	if !srv.Sudo.Passwordless {
		return Server{}, panelerr.Validation("passwordless_sudo_required", "Passwordless sudo is required")
	}
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok {
		return Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !adapter.SupportsUFW() {
		return Server{}, panelerr.Validation("ufw_not_supported", "UFW is not supported on this distribution")
	}
	return srv, nil
}

func (s *Service) fetchUFWStatus(ctx context.Context, srv Server) (remoteops.UFWStatus, error) {
	if status, ok, err := s.agentUFWStatus(ctx, srv); ok {
		return status, err
	}
	return s.fetchUFWStatusSSH(ctx, srv)
}

func (s *Service) fetchUFWStatusSSH(ctx context.Context, srv Server) (remoteops.UFWStatus, error) {
	res, err := s.exec.ExecSudo(ctx, srv.Target(), sshx.CommandSpec{Command: remoteops.UFWStatusScript(), Timeout: ufwManageTimeout})
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	return remoteops.ParseUFWStatus(res.Stdout), nil
}

func (s *Service) ensureUFWInstalled(ctx context.Context, srv Server) error {
	status, err := s.fetchUFWStatusSSH(ctx, srv)
	if err != nil {
		return err
	}
	if !status.Installed {
		return panelerr.Validation("ufw_not_installed", "UFW is not installed on this server")
	}
	return nil
}

func ufwStateFromStatus(serverID string, supported bool, status remoteops.UFWStatus) UFWState {
	rules := make([]UFWRule, 0, len(status.Rules))
	for _, rule := range status.Rules {
		rules = append(rules, UFWRule{Number: rule.Number, To: rule.To, Action: rule.Action, From: rule.From})
	}
	return UFWState{
		ServerID:  serverID,
		Supported: supported,
		Installed: status.Installed,
		Active:    status.Active,
		Status:    status.Status,
		Default:   status.Default,
		Rules:     rules,
	}
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
	return s.ensureConnectivityTask(ctx, serverID, runNow, connectivityTaskType, "Testing SSH connectivity", "")
}

func (s *Service) EnsureInitialInfoTask(ctx context.Context, serverID string, runNow bool) (tasks.Task, error) {
	return s.ensureConnectivityTask(ctx, serverID, runNow, serverInfoTaskType, "Collecting server information", "")
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

func (s *Service) ensureConnectivityTask(ctx context.Context, serverID string, runNow bool, taskType string, summary string, operationID string) (tasks.Task, error) {
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
		OperationID:  operationID,
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
		s.runConnectivityTest(taskCtx, task, srv)
	}()
}

func (s *Service) runConnectivityTest(ctx context.Context, task tasks.Task, srv Server) {
	taskID := task.ID
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	_ = s.tasks.Advance(ctx, taskID, "connecting", "connecting to server")
	osInfo, err := s.detectOS(ctx, srv, target)
	if err != nil {
		_ = s.markCheck(ctx, srv.ID, false, linux.OSRelease{}, false, nil, err.Error())
		s.failConnectivityTask(ctx, task, srv, err)
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
		detected, traitsErr := s.detectSystemTraitsForServer(ctx, srv, target)
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
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
	} else {
		_ = s.tasks.Complete(ctx, taskID, "Connected, passwordless sudo unavailable")
	}
}

func (s *Service) ensureAgentDeployTask(ctx context.Context, serverID, triggeredBy string, run bool) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server executor is unavailable")
	}
	if s.agentTLS == nil {
		return tasks.Task{}, panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, agentDeployTaskType, connectivityResourceType, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         agentDeployTaskType,
		ServerID:     srv.ID,
		ResourceType: connectivityResourceType,
		ResourceID:   srv.ID,
		TriggeredBy:  triggeredBy,
		Summary:      "Deploying panel agent for " + srv.Name,
		Status:       tasks.StatusRunning,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if run {
		go s.runDeployAgent(context.Background(), task.ID, srv)
	}
	return task, nil
}

func (s *Service) runDeployAgent(ctx context.Context, taskID string, srv Server) {
	defer s.tasks.FinishExecution(taskID)
	runner := remoteops.Runner{Exec: s.exec, Target: srv.Target(), Log: serverTaskLogSink{s.tasks, taskID}}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "preparing panel agent deployment")
	bundle, err := s.IssueAgentCertificate(ctx, srv.ID)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	executable, err := s.agentBinaryPath(ctx, srv)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	remoteTmp := "/tmp/panel-agent-" + taskID
	_ = s.tasks.Advance(ctx, taskID, "uploading", "uploading panel agent binary")
	if err := s.exec.Upload(ctx, srv.Target(), sshx.UploadSpec{LocalPath: executable, RemotePath: remoteTmp}); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "configuring", "installing panel agent configuration")
	files := []struct {
		path    string
		content []byte
		mode    string
	}{
		{agentRemoteConfigDir + "/ca.pem", []byte(bundle.CA), "0644"},
		{agentRemoteConfigDir + "/server.pem", []byte(bundle.Certificate), "0644"},
		{agentRemoteConfigDir + "/server-key.pem", []byte(bundle.PrivateKey), "0600"},
		{agentRemoteConfigDir + "/panel-agent.env", []byte(agentEnvFile(bundle)), "0600"},
		{agentRemoteServicePath, []byte(agentSystemdUnit()), "0644"},
	}
	for _, file := range files {
		if err := runner.WriteFileSudo(ctx, file.path, file.content, file.mode, agentDeployTimeout); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	_ = s.tasks.Advance(ctx, taskID, "starting", "starting panel agent service")
	if _, err := runner.RunSudoLogged(ctx, agentInstallScript(remoteTmp), agentDeployTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.markAgentConfigured(ctx, srv.ID, bundle.AgentURL); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	time.Sleep(2 * time.Second)
	_ = s.tasks.Advance(ctx, taskID, "checking", "checking panel agent compatibility")
	refreshed, err := s.Get(ctx, srv.ID)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.checkAgent(ctx, refreshed); err != nil {
		_ = s.markAgentStatus(ctx, srv.ID, agent.StatusUnavailable, "", err.Error())
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	checked, err := s.Get(ctx, srv.ID)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if checked.Traits[agent.TraitStatus] != agent.StatusCompatible {
		err := panelerr.Validation("agent_incompatible", firstNonEmpty(checked.Traits[agent.TraitLastError], "Agent is not compatible after deployment"))
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Panel agent deployed")
}

func (s *Service) failConnectivityTask(ctx context.Context, task tasks.Task, srv Server, err error) {
	if task.Type != serverInfoTaskType {
		_ = s.tasks.FailRetryable(ctx, task.ID, err)
		return
	}
	_ = s.tasks.AppendLog(ctx, task.ID, "system", "server creation rolled back because initial information collection failed")
	_ = s.tasks.Fail(ctx, task.ID, err)
	if rollbackErr := s.rollbackInitialServer(ctx, srv.ID); rollbackErr != nil {
		_ = s.tasks.AppendLog(ctx, task.ID, "stderr", "failed to roll back server creation: "+rollbackErr.Error())
	}
}

func (s *Service) rollbackInitialServer(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	return err
}

func (s *Service) runInstallUFW(ctx context.Context, taskID string, srv Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	_ = s.tasks.Advance(ctx, taskID, "installing", "installing UFW")
	if _, err := (remoteops.Runner{Exec: s.exec, Target: target, Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, ufwInstallScript(adapter, srv), ufwInstallTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}

	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing server system traits")
	osInfo, err := s.detectOS(ctx, srv, target)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	sudoRes, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0
	sysTraits := map[string]string{}
	if osInfo.Supported {
		detected, traitsErr := s.detectSystemTraitsForServer(ctx, srv, target)
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

func (s *Service) runEnableUFW(ctx context.Context, taskID string, srv Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	target := srv.Target()
	status, err := s.fetchUFWStatusSSH(ctx, srv)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if !status.Installed {
		_ = s.tasks.Advance(ctx, taskID, "installing", "installing UFW")
		if _, err := (remoteops.Runner{Exec: s.exec, Target: target, Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, strings.TrimSpace(adapter.UFWInstallScript()), ufwInstallTimeout); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	_ = s.tasks.Advance(ctx, taskID, "enabling", "enabling UFW")
	enableScript, err := remoteops.UFWEnableScript(normalizedTCPPort(srv.Port))
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if _, err := (remoteops.Runner{Exec: s.exec, Target: target, Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, enableScript, ufwManageTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "refreshing server system traits")
	if err := s.refreshServerTraits(ctx, taskID, srv); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "UFW enabled")
}

func (s *Service) runRestart(ctx context.Context, taskID string, srv Server) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Advance(ctx, taskID, "restarting", "scheduling server restart")
	if _, err := (remoteops.Runner{Exec: s.exec, Target: srv.Target(), Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, remoteops.RestartScript(), restartTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Server restart scheduled")
}

func (s *Service) detectSystemTraits(ctx context.Context, target sshx.Target) (map[string]string, error) {
	script := `echo "cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"
echo "mem=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' | awk '{print int($1/1024)}' || echo 0)"
echo "disk=$(df -m / 2>/dev/null | awk 'NR==2{print $2}' | awk '{print int($1/1024)}' || echo 0)"
echo "hostname=$(hostname 2>/dev/null || echo unknown)"
echo "arch=$(uname -m 2>/dev/null || echo unknown)"
cpu_model=""
if command -v lscpu >/dev/null 2>&1; then
  cpu_model="$(lscpu | awk -F: '/Model name/{sub(/^[ \t]+/, "", $2); print $2; exit}')"
fi
if [ -z "$cpu_model" ] && [ -r /proc/cpuinfo ]; then
  cpu_model="$(awk -F: '/model name|Hardware|Processor/{sub(/^[ \t]+/, "", $2); print $2; exit}' /proc/cpuinfo)"
fi
echo "cpu_model=${cpu_model:-unknown}"
if command -v ip >/dev/null 2>&1; then
  ip -o addr show scope global | awk '{iface=$2; sub(/@.*/, "", iface); print iface "|" $3 "|" $4}' |
  while IFS='|' read -r iface family address; do
    [ -e "/sys/class/net/$iface/device" ] || continue
    case "$iface" in
      lo|docker*|veth*|br-*|virbr*|cni*|flannel*|cali*|tun*|tap*|wg*|tailscale*|zt*) continue ;;
    esac
    echo "nic=$iface|$family|$address"
  done
elif [ -r /proc/net/dev ]; then
  for iface_path in /sys/class/net/*; do
    [ -e "$iface_path/device" ] || continue
    iface="${iface_path##*/}"
    case "$iface" in
      lo|docker*|veth*|br-*|virbr*|cni*|flannel*|cali*|tun*|tap*|wg*|tailscale*|zt*) continue ;;
    esac
    echo "nic=$iface|link|"
  done
fi
if command -v ufw >/dev/null 2>&1; then
  echo "ufw_installed=true"
  if systemctl is-active --quiet ufw 2>/dev/null || ufw status 2>/dev/null | grep -qi "^Status: active"; then
    echo "ufw_active=true"
  else
    echo "ufw_active=false"
  fi
else
  echo "ufw_installed=false"
  echo "ufw_active=false"
fi`
	cmd := "sh -lc " + remoteops.ShellQuote(script)

	res, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: cmd, Timeout: 12 * time.Second})
	if err != nil {
		return nil, err
	}

	traits := map[string]string{}
	nics := []string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "cores":
			traits["sys.cpu_cores"] = value
		case "mem":
			traits["sys.memory_total_mb"] = value
		case "disk":
			traits["sys.disk_total_gb"] = value
		case "hostname":
			traits["sys.hostname"] = value
		case "arch":
			traits["sys.architecture"] = value
		case "cpu_model":
			traits["sys.cpu_model"] = value
		case "nic":
			name, _, _ := strings.Cut(value, "|")
			if value != "" && !isVirtualNetworkInterface(name) {
				nics = append(nics, value)
			}
		case "ufw_installed":
			traits["sys.ufw_installed"] = value
		case "ufw_active":
			traits["sys.ufw_active"] = value
		}
	}
	if len(nics) > 0 {
		traits["sys.network_interfaces"] = strings.Join(nics, ", ")
	}
	return traits, nil
}

func (s *Service) detectOS(ctx context.Context, srv Server, target sshx.Target) (linux.OSRelease, error) {
	if baseURL, ok := agentURL(srv); ok && s.agent != nil {
		info, err := s.agent.OSRelease(ctx, baseURL)
		if err == nil {
			return info, nil
		}
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		return linux.OSRelease{}, err
	}
	return linux.Detect(ctx, s.exec, target)
}

func (s *Service) detectSystemTraitsForServer(ctx context.Context, srv Server, target sshx.Target) (map[string]string, error) {
	if baseURL, ok := agentURL(srv); ok && s.agent != nil {
		traits, err := s.agent.SystemTraits(ctx, baseURL)
		if err == nil {
			return traits, nil
		}
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		return nil, err
	}
	return s.detectSystemTraits(ctx, target)
}

func (s *Service) agentUFWStatus(ctx context.Context, srv Server) (remoteops.UFWStatus, bool, error) {
	baseURL, ok := agentURL(srv)
	if !ok || s.agent == nil {
		return remoteops.UFWStatus{}, false, nil
	}
	status, err := s.agent.UFWStatus(ctx, baseURL)
	if s.handleAgentCertificateTimeError(ctx, srv, err) {
		return remoteops.UFWStatus{}, true, err
	}
	return status, true, err
}

func (s *Service) checkAgent(ctx context.Context, srv Server) error {
	baseURL, ok := agentURL(srv)
	if !ok || s.agent == nil {
		return nil
	}
	health, err := s.agent.Health(ctx, baseURL)
	if err != nil {
		return err
	}
	if !agentVersionCompatible(health.Version, agent.Version) {
		_ = s.markAgentStatus(ctx, srv.ID, agent.StatusIncompatible, health.Version, fmt.Sprintf("agent version %s does not match required %s", health.Version, agent.Version))
		return nil
	}
	missing := missingAgentCapabilities(health.Capabilities)
	if len(missing) > 0 {
		_ = s.markAgentStatus(ctx, srv.ID, agent.StatusIncompatible, health.Version, "agent missing capabilities: "+strings.Join(missing, ", "))
		return nil
	}
	if strings.TrimSpace(health.Docker.Host) != "" && strings.TrimSpace(health.Docker.Host) != normalizeDockerHost(srv.DockerHost) {
		_ = s.markAgentStatus(ctx, srv.ID, agent.StatusIncompatible, health.Version, fmt.Sprintf("agent docker host %s does not match server configuration %s", health.Docker.Host, normalizeDockerHost(srv.DockerHost)))
		return nil
	}
	if health.Docker.Status != "ok" {
		msg := health.Docker.Error
		if msg == "" {
			msg = "docker api is unavailable"
		}
		_ = s.markAgentStatus(ctx, srv.ID, agent.StatusUnavailable, health.Version, msg)
		return nil
	}
	return s.markAgentStatus(ctx, srv.ID, agent.StatusCompatible, health.Version, "")
}

func (s *Service) handleAgentCertificateTimeError(ctx context.Context, srv Server, cause error) bool {
	if !isAgentCertificateTimeError(cause) {
		return false
	}
	msg := "agent certificate expired or is not yet valid"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		msg = cause.Error()
	}
	_ = s.markAgentStatus(ctx, srv.ID, agent.StatusIncompatible, "", msg)
	if s.exec == nil || s.agentTLS == nil || s.tasks == nil {
		return true
	}
	_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
	return true
}

func isAgentCertificateTimeError(err error) bool {
	if err == nil {
		return false
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "certificate has expired or is not yet valid") ||
		strings.Contains(msg, "certificate has expired") ||
		strings.Contains(msg, "certificate is not yet valid")
}

func (s *Service) markAgentStatus(ctx context.Context, serverID, status, version, msg string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	traits[agent.TraitStatus] = status
	traits[agent.TraitLastChecked] = time.Now().UTC().Format(time.RFC3339Nano)
	if version != "" {
		traits[agent.TraitVersion] = version
	}
	if msg == "" {
		delete(traits, agent.TraitLastError)
	} else {
		traits[agent.TraitLastError] = msg
	}
	traitsJSON, _ := json.Marshal(traits)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), now, serverID)
	return err
}

func (s *Service) markAgentConfigured(ctx context.Context, serverID, url string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	traits[agent.TraitEnabled] = "true"
	traits[agent.TraitURL] = strings.TrimSpace(url)
	delete(traits, agent.TraitLastError)
	traitsJSON, _ := json.Marshal(traits)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), now, serverID)
	return err
}

func missingAgentCapabilities(values []string) []string {
	have := map[string]struct{}{}
	for _, value := range values {
		have[strings.TrimSpace(value)] = struct{}{}
	}
	missing := []string{}
	for _, required := range agent.RequiredCapabilities {
		if _, ok := have[required]; !ok {
			missing = append(missing, required)
		}
	}
	return missing
}

func agentVersionCompatible(actual, required string) bool {
	actual = normalizeAgentVersion(actual)
	required = normalizeAgentVersion(required)
	return actual != "" && required != "" && actual == required
}

func normalizeAgentVersion(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	return strings.TrimPrefix(value, "V")
}

func agentEnvFile(bundle AgentCertificateBundle) string {
	return strings.Join([]string{
		"PANEL_AGENT_LISTEN_ADDRESS=" + systemdQuote(bundle.ListenAddress),
		"PANEL_AGENT_CA_FILE=" + systemdQuote(agentRemoteConfigDir+"/ca.pem"),
		"PANEL_AGENT_CERT_FILE=" + systemdQuote(agentRemoteConfigDir+"/server.pem"),
		"PANEL_AGENT_KEY_FILE=" + systemdQuote(agentRemoteConfigDir+"/server-key.pem"),
		"PANEL_AGENT_DOCKER_HOST=" + systemdQuote(bundle.DockerHost),
	}, "\n") + "\n"
}

func agentSystemdUnit() string {
	return `[Unit]
Description=Panel Agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/panel-agent/panel-agent.env
ExecStart=/usr/local/bin/panel-agent
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`
}

func agentInstallScript(remoteTmp string) string {
	return strings.Join([]string{
		"set -eu",
		`if ! command -v systemctl >/dev/null 2>&1; then`,
		`  echo "[panel] systemd is required to manage panel-agent" >&2`,
		`  exit 1`,
		`fi`,
		"install -m 0755 " + remoteops.ShellQuote(remoteTmp) + " " + remoteops.ShellQuote(agentRemoteBinaryPath),
		"rm -f " + remoteops.ShellQuote(remoteTmp),
		remoteops.MustUFWAllowScript(remoteops.UFWRule{Port: 9443, Protocol: "tcp"}),
		"systemctl daemon-reload",
		"systemctl enable --now panel-agent.service",
		"systemctl restart panel-agent.service",
		`echo "[panel] panel-agent service started"`,
	}, "\n")
}

func systemdQuote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Service) agentBinaryPath(ctx context.Context, srv Server) (string, error) {
	platform, err := s.agentTargetPlatform(ctx, srv)
	if err != nil {
		return "", err
	}
	binaryPath := agentBinaryPathForPlatform(platform)
	info, statErr := os.Stat(binaryPath)
	if statErr == nil && !info.IsDir() {
		return binaryPath, nil
	}
	return "", panelerr.Validation("agent_binary_unavailable", "panel-agent binary is unavailable for "+platform)
}

func (s *Service) agentTargetPlatform(ctx context.Context, srv Server) (string, error) {
	arch := normalizeAgentArch(srv.Traits["sys.architecture"])
	if arch == "" && s.exec != nil {
		res, err := s.exec.Exec(ctx, srv.Target(), sshx.CommandSpec{Command: "uname -m", Timeout: connectivitySudoTimeout})
		if err == nil {
			arch = normalizeAgentArch(res.Stdout)
		}
	}
	if arch == "" {
		return "", panelerr.Validation("agent_binary_unavailable", "panel-agent binary is unavailable for target architecture")
	}
	return "linux-" + arch, nil
}

func normalizeAgentArch(value string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	if len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func agentBinaryPathForPlatform(platform string) string {
	return path.Join(agentBundleRoot, strings.TrimSpace(platform), agentBundleBinaryName)
}

func agentURL(srv Server) (string, bool) {
	if !traitEnabled(srv.Traits[agent.TraitEnabled]) {
		return "", false
	}
	url := strings.TrimSpace(srv.Traits[agent.TraitURL])
	if url == "" {
		return "", false
	}
	return url, true
}

func isVirtualNetworkInterface(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "lo" {
		return true
	}
	for _, prefix := range []string{
		"docker", "veth", "br-", "virbr", "cni", "flannel", "cali",
		"tun", "tap", "wg", "tailscale", "zt",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

type serverTaskLogSink struct {
	tasks  *tasks.Service
	taskID string
}

func (s serverTaskLogSink) AppendLog(ctx context.Context, stream, line string) error {
	return s.tasks.AppendLog(ctx, s.taskID, stream, line)
}

func ufwInstallScript(adapter linux.DistroAdapter, srv Server) string {
	command := strings.TrimSpace(adapter.UFWInstallScript())
	rules := []remoteops.UFWRule{{Port: normalizedTCPPort(srv.Port), Protocol: "tcp"}}
	if traitEnabled(srv.Traits[reverseProxyEnabledTrait]) {
		for _, port := range reverseProxyTCPPorts {
			rules = append(rules, remoteops.UFWRule{Port: port, Protocol: "tcp"})
		}
	}
	return command + "\n" + remoteops.MustUFWAllowScript(uniqueUFWRules(rules)...)
}

func (s *Service) refreshServerTraits(ctx context.Context, taskID string, srv Server) error {
	target := srv.Target()
	osInfo, err := s.detectOS(ctx, srv, target)
	if err != nil {
		return err
	}
	sudoRes, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0
	sysTraits := map[string]string{}
	if osInfo.Supported {
		detected, traitsErr := s.detectSystemTraitsForServer(ctx, srv, target)
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
	return s.markCheck(ctx, srv.ID, true, osInfo, passwordless, sysTraits, msg)
}

func uniqueUFWRules(rules []remoteops.UFWRule) []remoteops.UFWRule {
	out := make([]remoteops.UFWRule, 0, len(rules))
	seen := map[string]struct{}{}
	for _, rule := range rules {
		key := strconv.Itoa(rule.Port) + "/" + strings.ToLower(rule.Protocol) + "/" + strings.ToLower(rule.From)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rule)
	}
	return out
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
	if strings.TrimSpace(normalizeDockerHost(req.DockerHost)) == "" {
		return panelerr.Validation("server_docker_host_required", "Docker host is required")
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

func normalizeDockerHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return agent.DefaultDockerHost
	}
	return value
}

type serverScanner interface{ Scan(dest ...any) error }

func scanServer(row serverScanner) (Server, error) {
	var srv Server
	var traits, variables, created, updated string
	var osSupported, reachable, sudo int
	var sudoAt, checkedAt sql.NullString
	err := row.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUsername, &srv.CredentialID, &srv.DockerHost, &traits, &variables, &srv.Notes, &srv.OS.ID, &srv.OS.VersionID, &srv.OS.PrettyName, &osSupported, &reachable, &sudo, &sudoAt, &checkedAt, &srv.LastError, &created, &updated)
	if err != nil {
		return Server{}, err
	}
	srv.Traits = map[string]string{}
	_ = json.Unmarshal([]byte(traits), &srv.Traits)
	srv.DockerHost = normalizeDockerHost(srv.DockerHost)
	srv.Variables = map[string]string{}
	_ = json.Unmarshal([]byte(variables), &srv.Variables)
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

func normalizeServerVariables(variables, traits map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range variables {
		key = strings.TrimSpace(key)
		if key != "" {
			out[key] = value
		}
	}
	for key, value := range traits {
		if strings.HasPrefix(key, "custom.") {
			if _, exists := out[strings.TrimPrefix(key, "custom.")]; !exists {
				out[strings.TrimPrefix(key, "custom.")] = value
			}
		}
	}
	return out
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
