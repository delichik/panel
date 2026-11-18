package server

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/tasks"
	storage "panel/internal/platform/database"
	sshx "panel/internal/platform/ssh"
)

// privilegeAwareDeployExec scripts a deploy run whose Exec surface (the plain
// SSH commands, i.e. the `id -u` privilege probe) needs its own answer without
// losing the sudo command log the ordering assertions rely on.
type privilegeAwareDeployExec struct {
	execCommands []string
	sudoCommands []string
	uploads      int
	onExec       func(command string) (sshx.CommandResult, error)
	onSudo       func(command string) (sshx.CommandResult, error)
}

func (f *privilegeAwareDeployExec) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.execCommands = append(f.execCommands, command.Command)
	if f.onExec != nil {
		return f.onExec(command.Command)
	}
	if strings.TrimSpace(command.Command) == "id -u" {
		return sshx.CommandResult{Stdout: "0\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *privilegeAwareDeployExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
	if f.onSudo != nil {
		return f.onSudo(command.Command)
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *privilegeAwareDeployExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	f.uploads++
	return nil
}

func (f *privilegeAwareDeployExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

// newDeployTestServiceWithExec mirrors newDeployTestService but accepts the
// executor so a test can script the plain-SSH privilege probe.
func newDeployTestServiceWithExec(t *testing.T, traits map[string]string, exec sshx.RemoteExecutor) (*Service, *tasks.Service, string, *storage.Store, *privilegeAwareDeployExec, *serverFakeAgentClient) {
	t.Helper()
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	setServerOS(t, store, srv.ID, "debian", "13")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, exec, taskSvc)
	svc.SetAgentTLSAssets(assets)
	agent := &serverFakeAgentClient{}
	svc.SetAgentClient(agent)
	cast, ok := exec.(*privilegeAwareDeployExec)
	if !ok {
		t.Fatalf("unexpected executor type %T", exec)
	}
	return svc, taskSvc, srv.ID, store, cast, agent
}

// TestAgentDeployProvisionsTimeSyncAfterPing pins the prerequisite order:
// firewall, container runtime, ping, then clock synchronization, all before the
// certificate/binary work (AGT-DEP-009).
func TestAgentDeployProvisionsTimeSyncAfterPing(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	}
	exec := &privilegeAwareDeployExec{}
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		switch {
		case strings.Contains(command, "panel_ufw_installed"):
			return sshx.CommandResult{Stdout: "panel_ufw_installed=true\nStatus: active\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_docker_installed"):
			return sshx.CommandResult{Stdout: "panel_docker_installed=true\npanel_docker_active=true\npanel_docker_ready=true\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_agent_ping"):
			return sshx.CommandResult{Stdout: "panel_agent_ping=present\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_agent_chrony"):
			return sshx.CommandResult{Stdout: "panel_agent_chrony=present\npanel_agent_chrony_service=active\npanel_agent_chrony_synced=true\n", ExitCode: 0}, nil
		}
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	svc, taskSvc, serverID, store, exec, agent := newDeployTestServiceWithExec(t, traits, exec)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)
	if _, err := store.AppDB().Exec(`UPDATE servers SET privilege_mode='root', sudo_passwordless=0 WHERE id=?`, serverID); err != nil {
		t.Fatal(err)
	}

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy, got %#v", finished)
	}
	commands := strings.Join(exec.sudoCommands, "\n")
	firewallIndex := strings.Index(commands, "panel_ufw_installed")
	dockerIndex := strings.Index(commands, "panel_docker_installed")
	pingIndex := strings.Index(commands, "panel_agent_ping")
	timeSyncIndex := strings.Index(commands, "panel_agent_chrony")
	if firewallIndex < 0 || dockerIndex < 0 || pingIndex < 0 || timeSyncIndex < 0 {
		t.Fatalf("expected firewall, docker, ping and time-sync probes in:\n%s", commands)
	}
	if firewallIndex >= dockerIndex || dockerIndex >= pingIndex || pingIndex >= timeSyncIndex {
		t.Fatalf("expected firewall < docker < ping < time-sync order in:\n%s", commands)
	}
	if exec.uploads != 0 {
		t.Fatalf("expected no binary upload for restart-only deploy, got %d", exec.uploads)
	}
}

// TestAgentDeployRepairsStalePrivilegeMode reproduces the Aliyun Chengdu state:
// a root node whose stored privilege mode was downgraded to none by one
// transient probe failure. The deployment must re-probe before the first
// privileged step, persist root, and then run the privileged steps directly
// instead of wrapping them in `sudo -n` (SSH-OPS-003).
func TestAgentDeployRepairsStalePrivilegeMode(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	}
	exec := &privilegeAwareDeployExec{}
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		switch {
		case strings.Contains(command, "panel_ufw_installed"):
			return sshx.CommandResult{Stdout: "panel_ufw_installed=true\nStatus: active\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_docker_installed"):
			return sshx.CommandResult{Stdout: "panel_docker_installed=true\npanel_docker_active=true\npanel_docker_ready=true\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_agent_ping"):
			return sshx.CommandResult{Stdout: "panel_agent_ping=present\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_agent_chrony"):
			return sshx.CommandResult{Stdout: "panel_agent_chrony=present\npanel_agent_chrony_service=active\npanel_agent_chrony_synced=true\n", ExitCode: 0}, nil
		}
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	svc, taskSvc, serverID, store, exec, agent := newDeployTestServiceWithExec(t, traits, exec)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)
	if _, err := store.AppDB().Exec(`UPDATE servers SET privilege_mode='none', sudo_passwordless=0, privilege_last_checked_at=NULL WHERE id=?`, serverID); err != nil {
		t.Fatal(err)
	}

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy, got %#v", finished)
	}
	probed := false
	for _, command := range exec.execCommands {
		if strings.TrimSpace(command) == "id -u" {
			probed = true
			break
		}
	}
	if !probed {
		t.Fatalf("expected a plain-SSH privilege probe before the first privileged step, got %#v", exec.execCommands)
	}
	updated, err := svc.Get(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Privilege.Mode != sshx.PrivilegeModeRoot {
		t.Fatalf("expected stale none to be repaired to root, got %q", updated.Privilege.Mode)
	}
}

// TestManualAgentDeployFailureDoesNotConsumeAutoDeployBudget pins
// AGT-AUTO-003: a manual retry that fails must not move the node toward
// undeployable; only system-triggered failures spend the automatic budget.
func TestManualAgentDeployFailureDoesNotConsumeAutoDeployBudget(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		if strings.Contains(command, "panel_ufw_installed") {
			return sshx.CommandResult{ExitCode: 1}, errors.New("ufw probe failed")
		}
		return sshx.CommandResult{ExitCode: 0}, nil
	}

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusFailed {
		t.Fatalf("expected the deploy task to fail, got %#v", finished)
	}
	updated, err := svc.Get(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	if failures := updated.Traits[agentcontract.TraitAutoDeployFailures]; failures != "" && failures != "0" {
		t.Fatalf("manual deploy failure must not increment auto deploy failures, got %q", failures)
	}
	if updated.Traits[agentcontract.TraitStatus] == agentcontract.StatusUndeployable {
		t.Fatalf("manual deploy failure must not mark the node undeployable")
	}
}
