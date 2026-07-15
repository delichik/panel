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
const serverInfoTaskType = "server_info_collect"
const connectivityResourceType = "server"
const connectivityMaxRetries = 8
const ufwInstallTaskType = "server_ufw_install"
const ufwEnableTaskType = "server_ufw_enable"
const ufwInstallTimeout = 5 * time.Minute
const ufwManageTimeout = time.Minute
const fail2banApplyTaskType = "server_fail2ban_apply"
const restartTaskType = "server_restart"
const restartTimeout = 15 * time.Second
const agentDeployTaskType = "server_agent_deploy"
const agentCertificateResetTaskType = "agent_certificate_reset"
const agentCertificateResourceType = "agent_certificate"
const agentDeployTimeout = 2 * time.Minute
const agentAutoDeployMaxFailures = 2
const agentAutoDeployHealthyChecksToReset = 5
const agentCertificateRenewBefore = 7 * 24 * time.Hour
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
	agentKeys agentTLSProvider
	tasks     *tasks.Service
	hostGuard PanelHostGuard
}

type PanelHostGuard interface {
	IsHostServer(ctx context.Context, serverID string) (bool, error)
}

type agentTLSProvider interface {
	EnsureAgentTLSAssets(ctx context.Context) (*agentsecurity.TLSAssets, error)
	IssueAgentServerCertificate(ctx context.Context, serverID, serverName, host string) (agentsecurity.ServerCertificate, []byte, error)
	ResetAgentCA(ctx context.Context) (*agentsecurity.TLSAssets, error)
	ResetAgentClientCertificate(ctx context.Context) (*agentsecurity.TLSAssets, error)
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

func WithAgentTLSProvider(provider agentTLSProvider) Option {
	return func(s *Service) { s.agentKeys = provider }
}

func WithPanelHostGuard(guard PanelHostGuard) Option {
	return func(s *Service) { s.hostGuard = guard }
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

func (s *Service) TestConnectivity(ctx context.Context, serverID string) (Server, error) {
	if s.exec == nil {
		return Server{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	target := serverTarget(srv)
	if _, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout}); err != nil {
		_ = s.recordReachability(ctx, serverID, false, err.Error())
		return Server{}, err
	}
	mode, _ := s.detectPrivilege(ctx, target)
	if err := s.recordConnectivity(ctx, serverID, true, mode, ""); err != nil {
		return Server{}, err
	}
	return s.Get(ctx, serverID)
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
	if maintenance, baseURL, ok, err := s.agentMaintenance(srv); ok || err != nil {
		if err != nil {
			return UFWState{}, err
		}
		status, callErr := maintenance.UFWAllow(ctx, baseURL, agentcontract.UFWAllowRequest{
			Rule: remoteops.UFWRule{Port: req.Port, Protocol: req.Protocol, From: req.From},
		})
		if callErr != nil {
			_ = s.handleAgentCertificateTimeError(ctx, srv, callErr)
			return UFWState{}, callErr
		}
		return ufwStateFromStatus(srv.ID, true, status), nil
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
	if maintenance, baseURL, ok, err := s.agentMaintenance(srv); ok || err != nil {
		if err != nil {
			return UFWState{}, err
		}
		status, callErr := maintenance.UFWDelete(ctx, baseURL, agentcontract.UFWDeleteRequest{Number: number})
		if callErr != nil {
			_ = s.handleAgentCertificateTimeError(ctx, srv, callErr)
			return UFWState{}, callErr
		}
		return ufwStateFromStatus(srv.ID, true, status), nil
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
	if !hasPrivilege(srv) {
		return tasks.Task{}, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required")
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
	if !hasPrivilege(srv) {
		return tasks.Task{}, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required")
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
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok {
		return Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !adapter.SupportsUFW() {
		return Server{}, panelerr.Validation("ufw_not_supported", "UFW is not supported on this distribution")
	}
	if !hasPrivilege(srv) && !hasCompatibleAgent(srv) {
		return Server{}, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required")
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

	mode, privilegeErr := s.detectPrivilege(probeCtx, target)
	root := mode == sshx.PrivilegeModeRoot
	passwordless := mode == sshx.PrivilegeModeSudo

	architecture, architectureErr := s.detectArchitectureInfo(probeCtx, target)
	if architectureErr != nil {
		return ProbeResult{Reachable: false, Error: architectureErr.Error(), Traits: map[string]string{}}, nil
	}
	sysTraits := map[string]string{}
	applyDistroSystemTraits(osInfo, sysTraits)

	result := ProbeResult{
		Reachable:        true,
		PasswordlessSudo: passwordless,
		Root:             root,
		Privileged:       mode != sshx.PrivilegeModeNone,
		PrivilegeMode:    mode,
		OS:               osInfo,
		Architecture:     architecture,
		Traits:           sysTraits,
	}
	if privilegeErr != nil && mode == sshx.PrivilegeModeNone {
		result.PasswordlessSudoText = privilegeErr.Error()
	}
	if !osInfo.Supported {
		result.Error = "unsupported distribution"
	}
	return result, nil
}

func (s *Service) EnsureInitialInfoTask(ctx context.Context, serverID string, runNow bool) (tasks.Task, error) {
	return s.ensureServerInfoTask(ctx, serverID, runNow, "Collecting initial server information", "", true)
}

func (s *Service) RunServerInfoTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
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
	taskCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	s.runServerInfoTask(taskCtx, task, srv)
	return nil
}

func (s *Service) ensureServerInfoTask(ctx context.Context, serverID string, runNow bool, summary string, operationID string, bootstrap bool) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server connectivity test executor is unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		OperationID:  operationID,
		Type:         serverInfoTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		Summary:      summary,
		MaxRetries:   connectivityMaxRetries,
		ParamsJSON:   `{"bootstrap":` + strconv.FormatBool(bootstrap) + `}`,
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
			s.startServerInfoTask(task, srv)
		}
		return task, nil
	}
	if runNow {
		s.startServerInfoTask(task, srv)
	}
	return task, nil
}

func (s *Service) startServerInfoTask(task tasks.Task, srv Server) {
	if err := s.tasks.Start(context.Background(), task.ID); err != nil {
		return
	}
	go func() {
		taskCtx, cancel := context.WithTimeout(s.tasks.ExecutionContext(task.ID), 45*time.Second)
		defer cancel()
		s.runServerInfoTask(taskCtx, task, srv)
	}()
}

func (s *Service) runServerInfoTask(ctx context.Context, task tasks.Task, srv Server) {
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
	if serverInfoBootstrap(task) {
		s.runInitialServerInfoCollection(ctx, task, srv, target)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "traits", "collecting full system information")
	if err := s.refreshServerTraits(ctx, taskID, srv); err != nil {
		_ = s.tasks.FailRetryable(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Server information collected")
}

func (s *Service) runInitialServerInfoCollection(ctx context.Context, task tasks.Task, srv Server, target sshx.Target) {
	taskID := task.ID
	osInfo, err := linux.Detect(ctx, s.exec, target)
	if err != nil {
		_ = s.markCheck(ctx, srv.ID, false, linux.OSRelease{}, sshx.PrivilegeModeNone, nil, err.Error())
		s.failConnectivityTask(ctx, task, srv, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "architecture", "detecting server architecture")
	architecture, err := s.detectArchitectureInfo(ctx, target)
	if err != nil {
		_ = s.markCheck(ctx, srv.ID, true, osInfo, sshx.PrivilegeModeNone, nil, err.Error())
		s.failConnectivityTask(ctx, task, srv, err)
		return
	}
	if err := s.markArchitecture(ctx, srv.ID, architecture); err != nil {
		s.failConnectivityTask(ctx, task, srv, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "checking privileged access")
	mode, privilegeErr := s.detectPrivilege(ctx, target)
	if privilegeErr != nil && mode == sshx.PrivilegeModeNone {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "privileged access unavailable: "+privilegeErr.Error())
	}
	sysTraits := map[string]string{}
	applyDistroSystemTraits(osInfo, sysTraits)
	msg := ""
	if !osInfo.Supported {
		msg = "unsupported distribution"
	}
	if err := s.markCheck(ctx, srv.ID, true, osInfo, mode, sysTraits, msg); err != nil {
		s.failConnectivityTask(ctx, task, srv, err)
		return
	}
	if !osInfo.Supported {
		_ = s.tasks.Complete(ctx, taskID, "Connected, but distribution is unsupported")
		return
	}
	if mode == sshx.PrivilegeModeNone {
		_ = s.tasks.Complete(ctx, taskID, "Architecture detected, privileged access unavailable")
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Architecture detected")
	_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
}

func (s *Service) failConnectivityTask(ctx context.Context, task tasks.Task, srv Server, err error) {
	if !serverInfoBootstrap(task) {
		_ = s.tasks.FailRetryable(ctx, task.ID, err)
		return
	}
	_ = s.tasks.AppendLog(ctx, task.ID, "system", "server creation rolled back because initial information collection failed")
	if rollbackErr := s.rollbackInitialServer(ctx, srv.ID); rollbackErr != nil {
		_ = s.tasks.AppendLog(ctx, task.ID, "stderr", "failed to roll back server creation: "+rollbackErr.Error())
	}
	_ = s.tasks.Fail(ctx, task.ID, err)
}

func serverInfoBootstrap(task tasks.Task) bool {
	var params struct {
		Bootstrap bool `json:"bootstrap"`
	}
	return json.Unmarshal([]byte(task.ParamsJSON), &params) == nil && params.Bootstrap
}

func (s *Service) RecordMetricsReachability(ctx context.Context, serverID string, reachable bool, message string) error {
	return s.recordReachability(ctx, serverID, reachable, message)
}

func (s *Service) recordReachability(ctx context.Context, serverID string, reachable bool, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET reachable=?,last_checked_at=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(reachable), now, message, now, serverID)
	return err
}

func (s *Service) recordConnectivity(ctx context.Context, serverID string, reachable bool, mode string, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET reachable=?,privilege_mode=?,privilege_last_checked_at=?,last_checked_at=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(reachable), mode, now, now, message, now, serverID)
	return err
}

func (s *Service) rollbackInitialServer(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	return err
}

func (s *Service) runInstallUFW(ctx context.Context, taskID string, srv Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	if maintenance, baseURL, ok, err := s.agentMaintenance(srv); ok || err != nil {
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "installing", "installing UFW through panel agent")
		rules := []remoteops.UFWRule{{Port: normalizedTCPPort(srv.Port), Protocol: "tcp"}, {Port: defaultAgentPort, Protocol: "tcp"}}
		if traitEnabled(srv.Traits[reverseProxyEnabledTrait]) {
			for _, port := range reverseProxyTCPPorts {
				rules = append(rules, remoteops.UFWRule{Port: port, Protocol: "tcp"})
			}
		}
		if _, callErr := maintenance.UFWInstall(ctx, baseURL, agentcontract.UFWInstallRequest{Rules: uniqueUFWRules(rules)}); callErr != nil {
			_ = s.handleAgentCertificateTimeError(ctx, srv, callErr)
			_ = s.tasks.Fail(ctx, taskID, callErr)
			return
		}
		if err := s.refreshServerTraits(ctx, taskID, srv); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "UFW installed")
		return
	}
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
	mode, _ := s.detectPrivilege(ctx, target)
	sysTraits := map[string]string{}
	if osInfo.Supported {
		detected, traitsErr := s.detectSystemTraitsForServer(ctx, srv, target)
		if traitsErr == nil {
			sysTraits = detected
		} else {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "failed to detect system traits: "+traitsErr.Error())
			if status, statusErr := s.fetchUFWStatusSSH(ctx, srv); statusErr == nil {
				sysTraits["sys.ufw_installed"] = boolString(status.Installed)
				sysTraits["sys.ufw_active"] = boolString(status.Active)
			}
		}
	}
	applyDistroSystemTraits(osInfo, sysTraits)
	msg := ""
	if !osInfo.Supported {
		msg = "unsupported distribution"
	}
	if err := s.markCheck(ctx, srv.ID, true, osInfo, mode, sysTraits, msg); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "UFW installed")
}

func (s *Service) runEnableUFW(ctx context.Context, taskID string, srv Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	if maintenance, baseURL, ok, err := s.agentMaintenance(srv); ok || err != nil {
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "enabling", "enabling UFW through panel agent")
		if _, callErr := maintenance.UFWEnable(ctx, baseURL, agentcontract.UFWEnableRequest{SSHPort: normalizedTCPPort(srv.Port)}); callErr != nil {
			_ = s.handleAgentCertificateTimeError(ctx, srv, callErr)
			_ = s.tasks.Fail(ctx, taskID, callErr)
			return
		}
		if err := s.refreshServerTraits(ctx, taskID, srv); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "UFW enabled")
		return
	}
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
	if maintenance, baseURL, ok, err := s.agentMaintenance(srv); ok || err != nil {
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		if callErr := maintenance.RestartSystem(ctx, baseURL); callErr != nil {
			_ = s.handleAgentCertificateTimeError(ctx, srv, callErr)
			_ = s.tasks.Fail(ctx, taskID, callErr)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "Server restart scheduled")
		return
	}
	if _, err := (remoteops.Runner{Exec: s.exec, Target: serverTarget(srv), Log: serverTaskLogSink{s.tasks, taskID}}).RunSudoLogged(ctx, remoteops.RestartScript(), restartTimeout); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Server restart scheduled")
}

func (s *Service) agentMaintenance(srv Server) (agentcontract.MaintenanceClient, string, bool, error) {
	baseURL, configured := agentURL(srv)
	maintenance, available := s.agent.(agentcontract.MaintenanceClient)
	if !available {
		if configured {
			return nil, "", true, panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
		}
		return nil, "", false, nil
	}
	if !configured {
		return nil, "", true, panelerr.Validation("agent_required", "Agent is required for server maintenance")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return nil, "", true, panelerr.Validation("agent_incompatible", "Agent is not compatible with server maintenance")
	}
	return maintenance, baseURL, true, nil
}

func hasCompatibleAgent(srv Server) bool {
	_, configured := agentURL(srv)
	return configured && srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusCompatible
}

func (s *Service) detectArchitectureInfo(ctx context.Context, target sshx.Target) (ArchitectureInfo, error) {
	res, err := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: "uname -m", Timeout: connectivitySudoTimeout})
	if err != nil {
		return ArchitectureInfo{}, err
	}
	rawMachine := strings.TrimSpace(res.Stdout)
	if rawMachine == "" {
		return ArchitectureInfo{}, panelerr.Validation("server_architecture_invalid", "Server architecture response is invalid")
	}
	arch := normalizeAgentArch(rawMachine)
	if arch == "" {
		return ArchitectureInfo{}, panelerr.Validation("agent_binary_unavailable", "panel-agent binary is unavailable for target platform")
	}
	return ArchitectureInfo{OS: "linux", Arch: arch, RawMachine: rawMachine}, nil
}

func (s *Service) markArchitecture(ctx context.Context, serverID string, architecture ArchitectureInfo) error {
	if s.db == nil || strings.TrimSpace(serverID) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET architecture_os=?,architecture_arch=?,architecture_machine=?,updated_at=? WHERE id=?`,
		architecture.OS, architecture.Arch, architecture.RawMachine, time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return err
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

func (s *Service) detectSystemTraitsForServer(ctx context.Context, srv Server, _ sshx.Target) (map[string]string, error) {
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
	return nil, panelerr.Validation("agent_required", "Agent is required for full system information collection")
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
	if status == agentcontract.StatusCompatible {
		streak := traitInt(traits, agentcontract.TraitHealthSuccessStreak) + 1
		if streak >= agentAutoDeployHealthyChecksToReset {
			delete(traits, agentcontract.TraitHealthSuccessStreak)
			delete(traits, agentcontract.TraitAutoDeployFailures)
			delete(traits, agentcontract.TraitAutoDeployLastFailure)
			delete(traits, agentcontract.TraitAutoDeployBlocked)
		} else {
			traits[agentcontract.TraitHealthSuccessStreak] = strconv.Itoa(streak)
		}
	} else {
		delete(traits, agentcontract.TraitHealthSuccessStreak)
	}
	traitsJSON, _ := json.Marshal(traits)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), now, serverID)
	return err
}

func (s *Service) recordAgentAutoDeployFailure(ctx context.Context, serverID string) (int, error) {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return 0, err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	failures := traitInt(traits, agentcontract.TraitAutoDeployFailures) + 1
	traits[agentcontract.TraitAutoDeployFailures] = strconv.Itoa(failures)
	traits[agentcontract.TraitAutoDeployLastFailure] = time.Now().UTC().Format(time.RFC3339Nano)
	delete(traits, agentcontract.TraitHealthSuccessStreak)
	traitsJSON, _ := json.Marshal(traits)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return failures, err
}

func (s *Service) resetAgentAutoDeployBackoffTime(ctx context.Context, serverID string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	if traitInt(traits, agentcontract.TraitAutoDeployFailures) == 0 {
		return nil
	}
	traits[agentcontract.TraitAutoDeployLastFailure] = time.Now().UTC().Format(time.RFC3339Nano)
	delete(traits, agentcontract.TraitHealthSuccessStreak)
	traitsJSON, _ := json.Marshal(traits)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(traitsJSON), time.Now().UTC().Format(time.RFC3339Nano), serverID)
	return err
}

func traitInt(traits map[string]string, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(traits[key]))
	if err != nil || value < 0 {
		return 0
	}
	return value
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

func (s *Service) RecordAgentReportStream(ctx context.Context, serverID string, connected bool, lastMessageAt time.Time, msg string) error {
	var rawTraits string
	if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&rawTraits); err != nil {
		return err
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	if connected {
		traits[agentcontract.TraitReportStatus] = agentcontract.ReportStatusConnected
		delete(traits, agentcontract.TraitReportLastError)
	} else {
		traits[agentcontract.TraitReportStatus] = agentcontract.ReportStatusDisconnected
		if strings.TrimSpace(msg) != "" {
			traits[agentcontract.TraitReportLastError] = strings.TrimSpace(msg)
		}
	}
	if !lastMessageAt.IsZero() {
		traits[agentcontract.TraitReportLastMessageAt] = lastMessageAt.UTC().Format(time.RFC3339Nano)
	}
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
		`  listeners=""`,
		`  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do`,
		`    listeners="$(ss -ltnp 'sport = :` + port + `' 2>/dev/null || true)"`,
		`    printf '%s\n' "$listeners" | grep -q LISTEN && break`,
		`    sleep 1`,
		`  done`,
		`  if ! printf '%s\n' "$listeners" | grep -q LISTEN; then`,
		`    echo "[panel] panel-agent did not open tcp/` + port + `" >&2`,
		`    systemctl status panel-agent.service --no-pager -l >&2 || true`,
		`    journalctl -u panel-agent.service -n 80 --no-pager >&2 || true`,
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
	osName := strings.ToLower(strings.TrimSpace(srv.Architecture.OS))
	arch := normalizeAgentArch(srv.Architecture.Arch)
	if arch == "" {
		arch = normalizeAgentArch(srv.Architecture.RawMachine)
	}
	if arch == "" && s.exec != nil {
		architecture, err := s.detectArchitectureInfo(ctx, serverTarget(srv))
		if err == nil {
			osName = architecture.OS
			arch = architecture.Arch
			_ = s.markArchitecture(ctx, srv.ID, architecture)
		}
	}
	if osName == "" {
		osName = "linux"
	}
	if osName != "linux" || arch == "" {
		return "", panelerr.Validation("agent_binary_unavailable", "panel-agent binary is unavailable for target architecture")
	}
	return osName + "-" + arch, nil
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
	if !agentURLMatchesDefault(srv) {
		return "", false
	}
	return url, true
}

func configuredAgentURL(srv Server) (string, bool) {
	if !traitEnabled(srv.Traits[agentcontract.TraitEnabled]) {
		return "", false
	}
	url := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	return url, url != ""
}

func agentURLMatchesDefault(srv Server) bool {
	url, ok := configuredAgentURL(srv)
	return ok && strings.TrimRight(url, "/") == agentDefaultURL(srv.Host)
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
	mode := privilegeMode(srv)
	sysTraits := map[string]string{}
	if osInfo.Supported {
		detected, traitsErr := s.detectSystemTraitsForServer(ctx, srv, target)
		if traitsErr == nil {
			sysTraits = detected
		} else {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "failed to detect system traits: "+traitsErr.Error())
			if status, statusErr := s.fetchUFWStatusSSH(ctx, srv); statusErr == nil {
				sysTraits["sys.ufw_installed"] = boolString(status.Installed)
				sysTraits["sys.ufw_active"] = boolString(status.Active)
			}
		}
	}
	applyDistroSystemTraits(osInfo, sysTraits)
	msg := ""
	if !osInfo.Supported {
		msg = "unsupported distribution"
	}
	return s.markCheck(ctx, srv.ID, true, osInfo, mode, sysTraits, msg)
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

func (s *Service) markCheck(ctx context.Context, serverID string, reachable bool, osInfo linux.OSRelease, privilegeMode string, sysTraits map[string]string, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	passwordless := privilegeMode == sshx.PrivilegeModeSudo
	var sudoCheckedAt any = now
	if privilegeMode == sshx.PrivilegeModeRoot {
		sudoCheckedAt = nil
	}

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

	// 剔除之前可刷新的 sys. 特征，用本次新探测到的 sysTraits 覆盖。
	for k := range current {
		if strings.HasPrefix(k, "sys.") {
			delete(current, k)
		}
	}
	for k, v := range sysTraits {
		current[k] = v
	}

	traitsJSON, _ := json.Marshal(current)

	_, err = s.db.ExecContext(ctx, `UPDATE servers SET reachable=?,os_id=?,os_version_id=?,os_pretty_name=?,os_supported=?,sudo_passwordless=?,sudo_last_checked_at=?,privilege_mode=?,privilege_last_checked_at=?,last_checked_at=?,traits=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(reachable), osInfo.ID, osInfo.VersionID, osInfo.PrettyName, boolInt(osInfo.Supported), boolInt(passwordless), sudoCheckedAt, privilegeMode, now, now, string(traitsJSON), msg, now, serverID)
	return err
}

func serverTarget(srv Server) sshx.Target {
	return Target(srv)
}

func (s *Service) detectPrivilege(ctx context.Context, target sshx.Target) (string, error) {
	rootResult, rootErr := s.exec.Exec(ctx, target, sshx.CommandSpec{Command: "id -u", Timeout: connectivitySudoTimeout})
	if rootErr == nil && strings.TrimSpace(rootResult.Stdout) == "0" {
		return sshx.PrivilegeModeRoot, nil
	}
	target.PrivilegeMode = sshx.PrivilegeModeSudo
	sudoResult, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	if sudoErr == nil && sudoResult.ExitCode == 0 {
		return sshx.PrivilegeModeSudo, nil
	}
	if sudoErr != nil {
		return sshx.PrivilegeModeNone, sudoErr
	}
	return sshx.PrivilegeModeNone, rootErr
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
