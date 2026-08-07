package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

func (s *Service) DeployAgent(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.ensureAgentDeployTask(ctx, serverID, "user", true)
}

func (s *Service) HandleAgentError(ctx context.Context, srv Server, cause error) bool {
	return s.handleAgentCertificateTimeError(ctx, srv, cause)
}

func (s *Service) RunAgentDeployTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if s.exec == nil {
		return panelerr.Validation("server_executor_unavailable", "Server executor is unavailable")
	}
	if s.agentTLS == nil {
		return panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return panelerr.Validation("server_id_required", "Server ID is required")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		if isNotFoundError(err) && s.tasks != nil {
			_ = s.tasks.Cancel(ctx, task.ID, "Task cancelled because the server was removed")
		}
		return err
	}
	if task.Status != tasks.StatusRunning {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
	}
	s.runDeployAgent(ctx, task.ID, srv)
	return nil
}

func (s *Service) CheckConfiguredAgents(ctx context.Context) {
	if s.agent == nil {
		return
	}
	serverIDs, err := s.repo.ListIDs(ctx)
	if err != nil {
		return
	}
	for _, serverID := range serverIDs {
		srv, err := s.Get(ctx, serverID)
		if err != nil {
			continue
		}
		s.CheckConfiguredAgent(ctx, srv)
	}
}

func (s *Service) CheckConfiguredAgent(ctx context.Context, srv Server) {
	if s.agent == nil {
		return
	}
	if agentAutoDeployBlocked(srv) || srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusUndeployable {
		if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusUndeployable {
			_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUndeployable, "", firstNonEmpty(srv.Traits[agentcontract.TraitLastError], "Agent auto deployment is stopped; use Reinstall Agent after fixing the error"))
		}
		return
	}
	if _, configured := configuredAgentURL(srv); configured && !agentURLMatchesDefault(srv) {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", nonDefaultAgentURLMessage(srv))
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if _, ok := agentURL(srv); !ok {
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if renewal, msg := agentCertificateRenewalProblem(srv, time.Now()); renewal {
		if msg != "" {
			_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", msg)
		}
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if agentStatusNeedsDeploy(srv) {
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if err := s.checkAgent(ctx, srv); err != nil {
		if s.handleAgentCertificateTimeError(ctx, srv, err) {
			if updated, getErr := s.Get(ctx, srv.ID); getErr == nil && agentStatusNeedsDeploy(updated) {
				_, _ = s.ensureAgentDeployTask(context.Background(), updated.ID, "system", true)
			}
			return
		}
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUnavailable, "", err.Error())
		return
	}
	updated, err := s.Get(ctx, srv.ID)
	if err != nil {
		return
	}
	if renewal, msg := agentCertificateRenewalProblem(updated, time.Now()); renewal {
		if msg != "" {
			_ = s.markAgentStatus(ctx, updated.ID, agentcontract.StatusIncompatible, "", msg)
		}
		_, _ = s.ensureAgentDeployTask(context.Background(), updated.ID, "system", true)
		return
	}
	if updated.Traits[agentcontract.TraitStatus] == agentcontract.StatusIncompatible {
		_, _ = s.ensureAgentDeployTask(context.Background(), updated.ID, "system", true)
	}
}

func (s *Service) ensureAgentDeployTask(ctx context.Context, serverID, triggeredBy string, run bool) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("server_executor_unavailable", "Server executor is unavailable")
	}
	if s.agentTLS == nil {
		return tasks.Task{}, panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	if triggeredBy == "user" {
		_ = s.setAgentAutoDeployBlocked(ctx, serverID, false)
		_ = s.resetAgentAutoDeployBackoffTime(ctx, serverID)
	}
	if triggeredBy != "user" {
		srv, err := s.Get(ctx, serverID)
		if err != nil {
			return tasks.Task{}, err
		}
		if agentAutoDeployBlocked(srv) || srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusUndeployable {
			return tasks.Task{}, panelerr.Conflict("agent_auto_deploy_blocked", firstNonEmpty(srv.Traits[agentcontract.TraitLastError], "Agent auto deployment is stopped; use Reinstall Agent after fixing the error"))
		}
		needed, err := s.agentAutoDeployNeeded(ctx, srv, time.Now())
		if err != nil {
			return tasks.Task{}, err
		}
		if !needed {
			return tasks.Task{}, nil
		}
		allowed, failures, err := s.agentAutoDeployAllowed(ctx, serverID)
		if err != nil {
			return tasks.Task{}, err
		}
		if !allowed {
			msg := fmt.Sprintf("agent auto deployment stopped after %d failed attempts; use Reinstall Agent after fixing the error", failures)
			_ = s.markAgentStatus(ctx, serverID, agentcontract.StatusUndeployable, "", msg)
			_ = s.setAgentAutoDeployBlocked(ctx, serverID, true)
			return tasks.Task{}, panelerr.Conflict("agent_auto_deploy_exhausted", msg)
		}
		if !s.agentAutoDeployRetryDue(srv, time.Now()) {
			return tasks.Task{}, nil
		}
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         agentDeployTaskType,
		ServerID:     srv.ID,
		ResourceType: connectivityResourceType,
		ResourceID:   srv.ID,
		TriggeredBy:  triggeredBy,
		Summary:      "Deploying panel agent for " + srv.Name,
	}, tasks.Trigger{Type: triggeredBy, Manual: triggeredBy == "user", Periodic: triggeredBy != "user"})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		if triggeredBy == "user" {
			_ = s.tasks.SetTriggeredBy(ctx, task.ID, "user")
			task.TriggeredBy = "user"
		}
		if run && task.Status != tasks.StatusRunning {
			if triggeredBy != "user" && !agentAutoDeployTaskDue(task, time.Now()) {
				return task, nil
			}
			task, err = s.tasks.RunNow(ctx, task.ID)
			if err != nil {
				return tasks.Task{}, err
			}
			s.startAgentDeployTask(task, srv)
			task, _ = s.tasks.Get(ctx, task.ID)
		}
		return task, nil
	}
	if run {
		s.startAgentDeployTask(task, srv)
		task, _ = s.tasks.Get(ctx, task.ID)
	}
	return task, nil
}

func (s *Service) startAgentDeployTask(task tasks.Task, srv Server) {
	if err := s.tasks.Start(context.Background(), task.ID); err != nil {
		return
	}
	go s.runDeployAgent(s.tasks.ExecutionContext(task.ID), task.ID, srv)
}

func (s *Service) agentAutoDeployNeeded(ctx context.Context, srv Server, now time.Time) (bool, error) {
	if _, configured := configuredAgentURL(srv); configured && !agentURLMatchesDefault(srv) {
		if err := s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", nonDefaultAgentURLMessage(srv)); err != nil {
			return false, err
		}
		return true, nil
	}
	if _, ok := agentURL(srv); !ok {
		return true, nil
	}
	if renewal, msg := agentCertificateRenewalProblem(srv, now); renewal {
		if msg != "" {
			if err := s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", msg); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	if agentStatusNeedsDeploy(srv) {
		return true, nil
	}
	return false, nil
}

func (s *Service) agentAutoDeployAllowed(ctx context.Context, serverID string) (bool, int, error) {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return false, 0, err
	}
	failures := traitInt(srv.Traits, agentcontract.TraitAutoDeployFailures)
	return failures < agentAutoDeployMaxFailures, failures, nil
}

func (s *Service) agentAutoDeployRetryDue(srv Server, now time.Time) bool {
	failures := traitInt(srv.Traits, agentcontract.TraitAutoDeployFailures)
	if failures <= 0 {
		return true
	}
	lastFailure, err := time.Parse(time.RFC3339Nano, srv.Traits[agentcontract.TraitAutoDeployLastFailure])
	if err != nil {
		return true
	}
	return !lastFailure.Add(agentAutoDeployBackoffDuration(failures)).After(now)
}

func agentAutoDeployTaskDue(task tasks.Task, now time.Time) bool {
	return task.NextRunAt == nil || !task.NextRunAt.After(now)
}

func agentAutoDeployBackoffDuration(failures int) time.Duration {
	if failures <= 1 {
		return 30 * time.Second
	}
	delay := 30 * time.Second
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= 10*time.Minute {
			return 10 * time.Minute
		}
	}
	return delay
}

func (s *Service) runDeployAgent(ctx context.Context, taskID string, srv Server) {
	defer s.tasks.FinishExecution(taskID)
	runner := remoteops.Runner{Exec: s.exec, Target: serverTarget(srv), Log: serverTaskLogSink{s.tasks, taskID}}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "preparing panel agent deployment")
	// Before any stop/restart action, ask the running agent whether it is safe
	// to restart. Agents too old to support the RPC, or unreachable agents,
	// fall back to proceeding directly; cancellation aborts the deployment.
	if _, configured := agentURL(srv); configured {
		_ = s.tasks.Advance(ctx, taskID, "checking", "waiting for agent restart readiness")
		if err := s.prepareAgentRestart(ctx, srv); err != nil {
			if ctx.Err() != nil {
				return
			}
			_ = s.tasks.AppendLog(ctx, taskID, "system", "agent restart readiness check skipped: "+err.Error())
		}
	}
	bundle, err := s.IssueAgentCertificate(ctx, srv.ID)
	if err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	upgrade := agentNeedsBinaryUpgrade(srv)
	var remoteTmp string
	if upgrade {
		executable, err := s.agentBinaryPath(ctx, srv)
		if err != nil {
			s.failAgentDeployTask(ctx, taskID, srv, err)
			return
		}
		remoteTmp = "/tmp/panel-agent-" + taskID
		_ = s.tasks.Advance(ctx, taskID, "uploading", "uploading panel agent binary")
		if err := s.exec.Upload(ctx, serverTarget(srv), sshx.UploadSpec{LocalPath: executable, RemotePath: remoteTmp}); err != nil {
			s.failAgentDeployTask(ctx, taskID, srv, err)
			return
		}
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
			s.failAgentDeployTask(ctx, taskID, srv, err)
			return
		}
	}
	if err := verifyRemoteAgentCertificateFile(ctx, runner, []byte(bundle.Certificate)); err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if upgrade {
		_ = s.tasks.Advance(ctx, taskID, "starting", "starting panel agent service")
		if _, err := runner.RunSudoLogged(ctx, agentInstallScript(remoteTmp), agentDeployTimeout); err != nil {
			s.failAgentDeployTask(ctx, taskID, srv, err)
			return
		}
	} else {
		_ = s.tasks.Advance(ctx, taskID, "restarting", "restarting panel agent service")
		if _, err := runner.RunSudoLogged(ctx, agentRestartScript(), agentDeployTimeout); err != nil {
			s.failAgentDeployTask(ctx, taskID, srv, err)
			return
		}
	}
	if err := verifyRemoteAgentServedCertificate(ctx, runner, bundle); err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if err := s.markAgentConfigured(ctx, srv.ID, bundle.AgentURL); err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	time.Sleep(2 * time.Second)
	_ = s.tasks.Advance(ctx, taskID, "checking", "checking panel agent compatibility")
	refreshed, err := s.Get(ctx, srv.ID)
	if err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if err := s.checkAgent(ctx, refreshed); err != nil {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUnavailable, "", err.Error())
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	checked, err := s.Get(ctx, srv.ID)
	if err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if checked.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		err := panelerr.Validation("agent_incompatible", firstNonEmpty(checked.Traits[agentcontract.TraitLastError], "Agent is not compatible after deployment"))
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if info, infoErr := agentsecurity.ParseCertificateInfo([]byte(bundle.Certificate)); infoErr == nil {
		_ = s.markAgentCertificate(ctx, srv.ID, info)
	}
	_ = s.tasks.Advance(ctx, taskID, "collecting", "collecting server information through panel agent")
	checked, err = s.Get(ctx, srv.ID)
	if err != nil {
		s.failAgentDeployTask(ctx, taskID, srv, err)
		return
	}
	if err := s.refreshServerTraits(ctx, taskID, checked); err != nil {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "panel agent deployed, but initial system information collection failed: "+err.Error())
	}
	_ = s.tasks.Complete(ctx, taskID, "Panel agent deployed")
}

// agentNeedsBinaryUpgrade reports whether the deploy task must upload a new
// agent binary. Fresh installs, unknown or mismatched versions, and agents in
// an unhealthy state take the full install path; certificate and URL renewals
// of a healthy matching agent only need a service restart.
func agentNeedsBinaryUpgrade(srv Server) bool {
	if _, configured := agentURL(srv); !configured {
		return true
	}
	version := strings.TrimSpace(srv.Traits[agentcontract.TraitVersion])
	if version == "" || version != agentcontract.Version {
		return true
	}
	switch srv.Traits[agentcontract.TraitStatus] {
	case agentcontract.StatusCompatible, agentcontract.StatusIncompatible:
		return false
	default:
		return true
	}
}

func hasAgentCapability(capabilities []string, capability string) bool {
	for _, value := range capabilities {
		if strings.TrimSpace(value) == capability {
			return true
		}
	}
	return false
}

// prepareAgentRestart asks the running agent whether it is safe to restart it.
// It returns nil when the agent reports ready, when the agent is too old to
// support the RPC, or when the configured client does not implement the
// readiness interface, so deployments can proceed for older or unreachable
// agents. Other errors (network, TLS, agent failures) are returned and logged
// by the caller, which still proceeds with the deployment.
func (s *Service) prepareAgentRestart(ctx context.Context, srv Server) error {
	if s.agent == nil {
		return nil
	}
	baseURL, ok := agentURL(srv)
	if !ok {
		return nil
	}
	health, err := s.agent.Health(ctx, baseURL)
	if err != nil {
		return err
	}
	if !hasAgentCapability(health.Capabilities, agentcontract.CapabilityPrepareRestart) {
		return nil
	}
	readiness, ok := s.agent.(agentcontract.RestartReadinessClient)
	if !ok {
		return nil
	}
	return readiness.PrepareRestart(ctx, baseURL)
}
func (s *Service) failAgentDeployTask(ctx context.Context, taskID string, srv Server, cause error) {
	_ = s.tasks.Fail(ctx, taskID, cause)
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil || task.TriggeredBy == "user" {
		return
	}
	_, _ = s.recordAgentAutoDeployFailure(ctx, srv.ID)
	_ = s.markAgentUndeployableIfAutoDeployExhausted(ctx, srv.ID)
}

func (s *Service) markAgentUndeployableIfAutoDeployExhausted(ctx context.Context, serverID string) error {
	allowed, failures, err := s.agentAutoDeployAllowed(ctx, serverID)
	if err != nil || allowed {
		return err
	}
	msg := fmt.Sprintf("agent auto deployment stopped after %d failed attempts; use Reinstall Agent after fixing the error", failures)
	if err := s.markAgentStatus(ctx, serverID, agentcontract.StatusUndeployable, "", msg); err != nil {
		return err
	}
	return s.setAgentAutoDeployBlocked(ctx, serverID, true)
}
