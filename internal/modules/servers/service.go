package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/servers/ports"
	serversqlite "panel/internal/modules/servers/store/sqlite"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
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
const agentCertificateResetTaskType = "agent_certificate_reset"
const agentCertificateResourceType = "agent_certificate"
const agentDeployTimeout = 2 * time.Minute
const agentAutoDeployMaxFailures = 2
const agentCertificateRenewBefore = 30 * 24 * time.Hour
const reverseProxyEnabledTrait = "agent.reverse_proxy.enabled"
const defaultAgentListenAddress = "0.0.0.0:9786"
const defaultAgentPort = 9786
const agentRemoteBinaryPath = "/usr/local/bin/panel-agent"
const agentRemoteConfigDir = "/etc/panel-agent"
const agentRemoteServicePath = "/etc/systemd/system/panel-agent.service"
const agentBundleRoot = "/app/panel-agents"
const agentBundleBinaryName = "panel-agent"

var reverseProxyTCPPorts = []int{80, 443}

type Service struct {
	db        *sql.DB
	repo      ports.ServerRepository
	metricsDB *sql.DB
	exec      sshx.RemoteExecutor
	agent     agentcontract.Client
	agentTLS  *agentsecurity.TLSAssets
	tasks     *tasks.Service
}

type Option func(*Service)

func WithMetricsDB(db *sql.DB) Option {
	return func(s *Service) { s.metricsDB = db }
}

func WithAgentClient(client agentcontract.Client) Option {
	return func(s *Service) { s.agent = client }
}

func WithAgentTLSAssets(assets *agentsecurity.TLSAssets) Option {
	return func(s *Service) { s.agentTLS = assets }
}

func NewService(db *sql.DB, exec sshx.RemoteExecutor, taskSvc *tasks.Service, opts ...Option) *Service {
	s := &Service{db: db, repo: serversqlite.NewServerRepository(db), exec: exec, tasks: taskSvc}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

func (s *Service) prepareServerForRead(ctx context.Context, srv Server) Server {
	srv.DockerHost = normalizeDockerHost(srv.DockerHost)
	applyDistroSystemTraits(srv.OS, srv.Traits)
	srv.LoadAverage = s.latestLoadAverage(ctx, srv.ID)
	return srv
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
	if _, err := (remoteops.Runner{Exec: s.exec, Target: serverTarget(srv)}).RunSudoLogged(ctx, script, ufwManageTimeout); err != nil {
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
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         ufwEnableTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Enabling UFW",
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runEnableUFW(s.tasks.ExecutionContext(task.ID), task.ID, srv, adapter)
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
	if _, err := (remoteops.Runner{Exec: s.exec, Target: serverTarget(srv)}).RunSudoLogged(ctx, script, ufwManageTimeout); err != nil {
		return UFWState{}, err
	}
	status, err := s.fetchUFWStatusSSH(ctx, srv)
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
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         ufwInstallTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Installing UFW",
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
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
	go s.runInstallUFW(s.tasks.ExecutionContext(task.ID), task.ID, srv, adapter)
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
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         restartTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Restarting server",
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRestart(s.tasks.ExecutionContext(task.ID), task.ID, srv)
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
	baseURL, ok := agentURL(srv)
	if !ok {
		return remoteops.UFWStatus{}, panelerr.Validation("agent_required", "Agent is required for UFW status")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return remoteops.UFWStatus{}, panelerr.Validation("agent_incompatible", "Agent is not compatible with UFW status")
	}
	if s.agent == nil {
		return remoteops.UFWStatus{}, panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
	}
	status, err := s.agent.UFWStatus(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
	}
	return status, err
}

func (s *Service) fetchUFWStatusSSH(ctx context.Context, srv Server) (remoteops.UFWStatus, error) {
	res, err := s.exec.ExecSudo(ctx, serverTarget(srv), sshx.CommandSpec{Command: remoteops.UFWStatusScript(), Timeout: ufwManageTimeout})
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
		if isNotFoundError(err) && s.tasks != nil {
			_ = s.tasks.Cancel(ctx, task.ID, "Task cancelled because the server was removed")
		}
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
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		OperationID:  operationID,
		Type:         taskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		Summary:      summary,
		MaxRetries:   connectivityMaxRetries,
	}, tasks.Trigger{Type: "scheduler", Periodic: !runNow})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		if runNow && task.Status != tasks.StatusRunning {
			task, err = s.tasks.RunNow(ctx, task.ID)
			if err != nil {
				return tasks.Task{}, err
			}
			s.startConnectivityTask(task, srv)
		}
		return task, nil
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
	if err := s.tasks.Start(context.Background(), task.ID); err != nil {
		return
	}
	go func() {
		taskCtx, cancel := context.WithTimeout(s.tasks.ExecutionContext(task.ID), 45*time.Second)
		defer cancel()
		s.runConnectivityTest(taskCtx, task, srv)
	}()
}

func (s *Service) runConnectivityTest(ctx context.Context, task tasks.Task, srv Server) {
	taskID := task.ID
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return
	}
	if err := s.tasks.Start(ctx, taskID); err != nil {
		return
	}
	target := serverTarget(srv)
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
	target := serverTarget(srv)
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
	target := serverTarget(srv)
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
	if _, err := (remoteops.Runner{Exec: s.exec, Target: serverTarget(srv), Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, remoteops.RestartScript(), restartTimeout); err != nil {
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
	if baseURL, ok := agentURL(srv); ok {
		if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			s.recoverAgentForSystemDetection(ctx, srv)
			return linux.OSRelease{}, panelerr.Validation("agent_incompatible", "Agent is not compatible with system detection")
		}
		if s.agent == nil {
			return linux.OSRelease{}, panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
		}
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
	if baseURL, ok := agentURL(srv); ok {
		if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			s.recoverAgentForSystemDetection(ctx, srv)
			return nil, panelerr.Validation("agent_incompatible", "Agent is not compatible with system detection")
		}
		if s.agent == nil {
			return nil, panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
		}
		traits, err := s.agent.SystemTraits(ctx, baseURL)
		if err == nil {
			return traits, nil
		}
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		return nil, err
	}
	return s.detectSystemTraits(ctx, target)
}

func systemCertificateFromInfo(id, certificateType, name string, info agentsecurity.CertificateInfo) SystemCertificate {
	notBefore := info.NotBefore
	notAfter := info.NotAfter
	return SystemCertificate{
		ID:          id,
		Type:        certificateType,
		Name:        name,
		CommonName:  info.CommonName,
		Fingerprint: info.Fingerprint,
		NotBefore:   &notBefore,
		NotAfter:    &notAfter,
		Status:      certificateTimeStatus(info, time.Now()),
		BuiltIn:     true,
		CanReset:    true,
	}
}

func agentServerCertificateFromTraits(srv Server) (SystemCertificate, bool) {
	fingerprint := strings.TrimSpace(srv.Traits[agentcontract.TraitCertificateFingerprint])
	if fingerprint == "" {
		return SystemCertificate{}, false
	}
	notBefore, err := parseAgentCertificateTraitTime(srv.Traits[agentcontract.TraitCertificateNotBefore])
	if err != nil {
		return SystemCertificate{}, false
	}
	notAfter, err := parseAgentCertificateTraitTime(srv.Traits[agentcontract.TraitCertificateNotAfter])
	if err != nil {
		return SystemCertificate{}, false
	}
	info := agentsecurity.CertificateInfo{
		Fingerprint: fingerprint,
		CommonName:  "panel-agent-" + srv.ID,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
	}
	out := systemCertificateFromInfo("agent-server:"+srv.ID, "tls_certificate", "Agent server certificate - "+srv.Name, info)
	out.ServerID = srv.ID
	out.ServerName = srv.Name
	return out, true
}

func parseAgentCertificateTraitTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("agent certificate time is empty")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func certificateTimeStatus(info agentsecurity.CertificateInfo, now time.Time) string {
	if now.Before(info.NotBefore) {
		return "not_yet_valid"
	}
	if now.After(info.NotAfter) {
		return "expired"
	}
	return "valid"
}

func (s *Service) setAgentAutoDeployBlocked(ctx context.Context, serverID string, blocked bool) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	if blocked {
		traits[agentcontract.TraitAutoDeployBlocked] = "true"
	} else {
		delete(traits, agentcontract.TraitAutoDeployBlocked)
	}
	traitsJSON, _ := json.Marshal(traits)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return err
}

func (s *Service) markAgentStatus(ctx context.Context, serverID, status, version, msg string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	traits[agentcontract.TraitStatus] = status
	traits[agentcontract.TraitLastChecked] = time.Now().UTC().Format(time.RFC3339Nano)
	if version != "" {
		traits[agentcontract.TraitVersion] = version
	}
	if msg == "" {
		delete(traits, agentcontract.TraitLastError)
	} else {
		traits[agentcontract.TraitLastError] = msg
	}
	traitsJSON, _ := json.Marshal(traits)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), now, serverID)
	return err
}

func (s *Service) markAgentCertificate(ctx context.Context, serverID string, info agentsecurity.CertificateInfo) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	traits[agentcontract.TraitCertificateFingerprint] = info.Fingerprint
	traits[agentcontract.TraitCertificateNotBefore] = info.NotBefore.UTC().Format(time.RFC3339Nano)
	traits[agentcontract.TraitCertificateNotAfter] = info.NotAfter.UTC().Format(time.RFC3339Nano)
	traitsJSON, _ := json.Marshal(traits)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return err
}

func (s *Service) clearAgentCertificate(ctx context.Context, serverID string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	delete(traits, agentcontract.TraitCertificateFingerprint)
	delete(traits, agentcontract.TraitCertificateNotBefore)
	delete(traits, agentcontract.TraitCertificateNotAfter)
	traitsJSON, _ := json.Marshal(traits)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return err
}

func (s *Service) markAgentConfigured(ctx context.Context, serverID, url string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	traits[agentcontract.TraitEnabled] = "true"
	traits[agentcontract.TraitURL] = strings.TrimSpace(url)
	delete(traits, agentcontract.TraitLastError)
	delete(traits, agentcontract.TraitAutoDeployBlocked)
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
	for _, required := range agentcontract.RequiredCapabilities {
		if _, ok := have[required]; !ok {
			missing = append(missing, required)
		}
	}
	return missing
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
	port := strconv.Itoa(defaultAgentPort)
	return strings.Join([]string{
		"set -eu",
		`if ! command -v systemctl >/dev/null 2>&1; then`,
		`  echo "[panel] systemd is required to manage panel-agent" >&2`,
		`  exit 1`,
		`fi`,
		"systemctl stop panel-agent.service >/dev/null 2>&1 || true",
		`if command -v pkill >/dev/null 2>&1; then`,
		`  pkill -x panel-agent >/dev/null 2>&1 || true`,
		`  pkill -f '^/usr/local/bin/panel-agent($| )' >/dev/null 2>&1 || true`,
		`fi`,
		"install -m 0755 " + remoteops.ShellQuote(remoteTmp) + " " + remoteops.ShellQuote(agentRemoteBinaryPath),
		"rm -f " + remoteops.ShellQuote(remoteTmp),
		remoteops.MustUFWAllowScript(remoteops.UFWRule{Port: defaultAgentPort, Protocol: "tcp"}),
		"systemctl daemon-reload",
		`if ! systemctl enable panel-agent.service; then`,
		`  echo "[panel] failed to enable panel-agent.service" >&2`,
		`  systemctl status panel-agent.service --no-pager -l >&2 || true`,
		`  exit 1`,
		`fi`,
		`if ! systemctl restart panel-agent.service; then`,
		`  echo "[panel] failed to restart panel-agent.service" >&2`,
		`  systemctl status panel-agent.service --no-pager -l >&2 || true`,
		`  journalctl -u panel-agent.service -n 80 --no-pager >&2 || true`,
		`  exit 1`,
		`fi`,
		`for i in 1 2 3 4 5 6 7 8 9 10; do`,
		`  systemctl is-active --quiet panel-agent.service && break`,
		`  sleep 1`,
		`done`,
		`if ! systemctl is-active --quiet panel-agent.service; then`,
		`  echo "[panel] panel-agent.service is not active after restart" >&2`,
		`  systemctl status panel-agent.service --no-pager -l >&2 || true`,
		`  journalctl -u panel-agent.service -n 80 --no-pager >&2 || true`,
		`  exit 1`,
		`fi`,
		`if command -v ss >/dev/null 2>&1; then`,
		`  listeners="$(ss -ltnp 'sport = :` + port + `' 2>/dev/null || true)"`,
		`  if ! printf '%s\n' "$listeners" | grep -q LISTEN; then`,
		`    echo "[panel] panel-agent did not open tcp/` + port + `" >&2`,
		`    exit 1`,
		`  fi`,
		`  if ! printf '%s\n' "$listeners" | grep -q panel-agent; then`,
		`    echo "[panel] tcp/` + port + ` listener did not expose a panel-agent process name; continuing with TLS certificate verification" >&2`,
		`    printf '%s\n' "$listeners" >&2`,
		`  fi`,
		`fi`,
		`echo "[panel] panel-agent service started"`,
	}, "\n")
}

func verifyRemoteAgentCertificateFile(ctx context.Context, runner remoteops.Runner, certificatePEM []byte) error {
	sum := sha256.Sum256(certificatePEM)
	script := strings.Join([]string{
		"set -eu",
		`if ! command -v sha256sum >/dev/null 2>&1; then`,
		`  echo "[panel] sha256sum is required to verify panel-agent certificate deployment" >&2`,
		`  exit 1`,
		`fi`,
		`actual="$(sha256sum ` + remoteops.ShellQuote(agentRemoteConfigDir+"/server.pem") + ` | awk '{print $1}')"`,
		`expected="` + fmt.Sprintf("%x", sum[:]) + `"`,
		`if [ "$actual" != "$expected" ]; then`,
		`  echo "[panel] deployed panel-agent certificate file does not match the newly issued certificate" >&2`,
		`  exit 1`,
		`fi`,
		`echo "[panel] panel-agent certificate file verified"`,
	}, "\n")
	_, err := runner.RunSudoLogged(ctx, script, agentDeployTimeout)
	return err
}

func verifyRemoteAgentServedCertificate(ctx context.Context, runner remoteops.Runner, bundle AgentCertificateBundle) error {
	info, err := agentsecurity.ParseCertificateInfo([]byte(bundle.Certificate))
	if err != nil {
		return err
	}
	endpoint := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(bundle.AgentURL), "https://"), "http://")
	serverName := endpoint
	port := strconv.Itoa(defaultAgentPort)
	if host, urlPort, splitErr := net.SplitHostPort(endpoint); splitErr == nil {
		serverName = host
		port = urlPort
	}
	script := strings.Join([]string{
		"set -eu",
		`if ! command -v openssl >/dev/null 2>&1; then`,
		`  echo "[panel] openssl is required to verify the certificate served on tcp/` + port + `" >&2`,
		`  exit 1`,
		`fi`,
		`fingerprint="$(timeout 10 openssl s_client -connect 127.0.0.1:` + port + ` -servername ` + remoteops.ShellQuote(serverName) + ` -showcerts </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256 2>/dev/null | awk -F= '{print $2}' | tr -d ':' | tr '[:lower:]' '[:upper:]')"`,
		`expected="` + info.Fingerprint + `"`,
		`if [ -z "$fingerprint" ]; then`,
		`  echo "[panel] tcp/` + port + ` did not serve a readable TLS certificate" >&2`,
		`  exit 1`,
		`fi`,
		`if [ "$fingerprint" != "$expected" ]; then`,
		`  echo "[panel] tcp/` + port + ` is serving an unexpected certificate fingerprint: $fingerprint" >&2`,
		`  echo "[panel] expected newly issued certificate fingerprint: $expected" >&2`,
		`  exit 1`,
		`fi`,
		`echo "[panel] panel-agent served certificate verified"`,
	}, "\n")
	_, err = runner.RunSudoLogged(ctx, script, agentDeployTimeout)
	return err
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

func removeString(values []string, target string) ([]string, bool) {
	out := make([]string, 0, len(values))
	changed := false
	for _, value := range values {
		if value == target {
			changed = true
			continue
		}
		out = append(out, value)
	}
	return out, changed
}

func isNotFoundError(err error) bool {
	var pe *panelerr.Error
	return errors.As(err, &pe) && pe.Code == "not_found"
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
		res, err := s.exec.Exec(ctx, serverTarget(srv), sshx.CommandSpec{Command: "uname -m", Timeout: connectivitySudoTimeout})
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

func agentDefaultURL(host string) string {
	return "https://" + net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(defaultAgentPort))
}

func serverHasAgentConfigured(current Server, nextTraits map[string]string) bool {
	return traitEnabled(current.Traits[agentcontract.TraitEnabled]) ||
		strings.TrimSpace(current.Traits[agentcontract.TraitURL]) != "" ||
		traitEnabled(nextTraits[agentcontract.TraitEnabled]) ||
		strings.TrimSpace(nextTraits[agentcontract.TraitURL]) != ""
}

func agentURL(srv Server) (string, bool) {
	if !traitEnabled(srv.Traits[agentcontract.TraitEnabled]) {
		return "", false
	}
	url := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
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
	target := serverTarget(srv)
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

func serverTarget(srv Server) sshx.Target {
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
		return agentcontract.DefaultDockerHost
	}
	return value
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
