package server

import (
	"context"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux/remoteops"
	sshx "panel/internal/platform/ssh"
)

func TestAgentDockerScriptInstallsOnlyWhenMissing(t *testing.T) {
	install, err := agentDockerScript(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"apt_get update", "apt_get install -y docker.io", "systemctl enable --now docker", "panel_agent_docker_ready", "panel_agent_docker_failed reason=not_ready"} {
		if !strings.Contains(install, want) {
			t.Fatalf("expected %q in:\n%s", want, install)
		}
	}
	// The step is additive: it never restarts a running daemon and never
	// deletes anything.
	for _, forbidden := range []string{"systemctl restart docker", "rm -", "docker rm"} {
		if strings.Contains(install, forbidden) {
			t.Fatalf("did not expect %q in:\n%s", forbidden, install)
		}
	}

	reuse, err := agentDockerScript(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reuse, "apt_get install") || strings.Contains(reuse, "apt_get update") {
		t.Fatalf("did not expect an install step:\n%s", reuse)
	}
	if !strings.Contains(reuse, "systemctl enable --now docker") {
		t.Fatalf("expected the service assertion:\n%s", reuse)
	}
}

// TestAgentDockerProvisioningWorstCaseFitsInsideThePanelBound pins the same
// invariant the download path has: the target-side worst case (two bounded apt
// invocations plus the readiness wait) must stay strictly inside the Panel-side
// bound, so the script can always finish and report its own failure reason.
func TestAgentDockerProvisioningWorstCaseFitsInsideThePanelBound(t *testing.T) {
	worst := time.Duration(2*agentDockerAPTTimeoutSeconds)*time.Second +
		time.Duration(agentDockerReadyPollAttempts*agentDockerReadyPollIntervalSeconds)*time.Second
	if worst >= agentDockerInstallTimeout {
		t.Fatalf("target-side worst case %s must stay strictly inside the Panel bound %s", worst, agentDockerInstallTimeout)
	}
}

type dockerProvisionFakeExec struct {
	status       remoteops.DockerStatus
	sudoCommands []string
	failScript   bool
}

func (f *dockerProvisionFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *dockerProvisionFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
	if strings.Contains(command.Command, "panel_docker_installed") {
		stdout := "panel_docker_installed=" + boolValue(f.status.Installed) + "\n" +
			"panel_docker_active=" + boolValue(f.status.Active) + "\n" +
			"panel_docker_ready=" + boolValue(f.status.Ready) + "\n"
		return sshx.CommandResult{Stdout: stdout, ExitCode: 0}, nil
	}
	if f.failScript {
		return sshx.CommandResult{ExitCode: 1}, errString("remote command failed")
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *dockerProvisionFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *dockerProvisionFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func boolValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestEnsureAgentDocker(t *testing.T) {
	base := Server{ID: "srv_1", Host: "10.0.0.1", Port: 22, DockerHost: agentcontract.DefaultDockerHost}

	t.Run("a custom docker host is operator-managed", func(t *testing.T) {
		exec := &dockerProvisionFakeExec{}
		svc, _, _ := testServerService(t, exec)
		srv := base
		srv.DockerHost = "tcp://10.0.0.5:2375"
		if err := svc.ensureAgentDocker(context.Background(), "task_1", srv, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		if len(exec.sudoCommands) != 0 {
			t.Fatalf("no command may run for a custom endpoint, got %#v", exec.sudoCommands)
		}
	})

	t.Run("a ready runtime is left alone", func(t *testing.T) {
		exec := &dockerProvisionFakeExec{status: remoteops.DockerStatus{Installed: true, Active: true, Ready: true}}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentDocker(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		if len(exec.sudoCommands) != 1 || !strings.Contains(exec.sudoCommands[0], "panel_docker_installed") {
			t.Fatalf("expected only the probe, got %#v", exec.sudoCommands)
		}
	})

	t.Run("a missing runtime is installed", func(t *testing.T) {
		exec := &dockerProvisionFakeExec{}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentDocker(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(exec.sudoCommands, "\n---\n")
		for _, want := range []string{"apt_get install -y docker.io", "systemctl enable --now docker"} {
			if !strings.Contains(commands, want) {
				t.Fatalf("expected %q in:\n%s", want, commands)
			}
		}
	})

	t.Run("an installed but stopped runtime is started without reinstalling", func(t *testing.T) {
		exec := &dockerProvisionFakeExec{status: remoteops.DockerStatus{Installed: true}}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentDocker(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(exec.sudoCommands, "\n---\n")
		if strings.Contains(commands, "apt_get install") {
			t.Fatalf("did not expect an install step:\n%s", commands)
		}
		if !strings.Contains(commands, "systemctl enable --now docker") {
			t.Fatalf("expected the service assertion:\n%s", commands)
		}
	})

	t.Run("a failing provisioning step is reported as agent_docker_failed", func(t *testing.T) {
		exec := &dockerProvisionFakeExec{failScript: true}
		svc, _, _ := testServerService(t, exec)
		err := svc.ensureAgentDocker(context.Background(), "task_1", base, remoteops.Runner{Exec: exec})
		if err == nil {
			t.Fatal("expected the provisioning failure to surface")
		}
		if !strings.Contains(err.Error(), "Docker") {
			t.Fatalf("expected the error to name Docker, got %v", err)
		}
	})
}

// TestAgentDeployProvisionsDockerAfterFirewall pins the ordering: the
// container runtime is provided after the firewall prerequisite and before the
// agent binary is uploaded.
func TestAgentDeployProvisionsDockerAfterFirewall(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealth(agentcontract.Version)
	writeAgentBundle(t)
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		switch {
		case strings.Contains(command, "panel_ufw_installed"):
			return sshx.CommandResult{Stdout: "panel_ufw_installed=false\nStatus: inactive\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_docker_installed"):
			return sshx.CommandResult{Stdout: "panel_docker_installed=false\npanel_docker_active=false\npanel_docker_ready=false\n", ExitCode: 0}, nil
		default:
			return sshx.CommandResult{ExitCode: 0}, nil
		}
	}

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy, got %#v (%s)", finished, finished.Error)
	}
	commands := strings.Join(exec.sudoCommands, "\n---\n")
	firewallIndex := strings.Index(commands, "ufw allow")
	dockerProbeIndex := strings.Index(commands, "panel_docker_installed")
	dockerInstallIndex := strings.Index(commands, "apt_get install -y docker.io")
	if firewallIndex < 0 || dockerProbeIndex < 0 || dockerInstallIndex < 0 {
		t.Fatalf("expected firewall, docker probe and docker install in:\n%s", commands)
	}
	if firewallIndex >= dockerProbeIndex || dockerProbeIndex >= dockerInstallIndex {
		t.Fatalf("expected firewall before docker provisioning:\n%s", commands)
	}
	if exec.uploads != 1 {
		t.Fatalf("expected the binary upload after provisioning, got %d", exec.uploads)
	}
}

// TestAgentDeployStopsBeforeBinaryWhenDockerProvisioningFails covers the
// failure contract: the prerequisite failure fails the deployment without
// touching the running agent.
func TestAgentDeployStopsBeforeBinaryWhenDockerProvisioningFails(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealth(agentcontract.Version)
	writeAgentBundle(t)
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		switch {
		case strings.Contains(command, "panel_ufw_installed"):
			return sshx.CommandResult{Stdout: "panel_ufw_installed=true\nStatus: active\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_docker_installed"):
			return sshx.CommandResult{Stdout: "panel_docker_installed=false\npanel_docker_active=false\npanel_docker_ready=false\n", ExitCode: 0}, nil
		case strings.Contains(command, "apt_get install -y docker.io"):
			return sshx.CommandResult{ExitCode: 1}, errString("remote command failed")
		default:
			return sshx.CommandResult{ExitCode: 0}, nil
		}
	}

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusFailed {
		t.Fatalf("expected the deployment to fail, got %#v", finished)
	}
	if exec.uploads != 0 {
		t.Fatalf("the agent binary must not be uploaded when provisioning fails, got %d uploads", exec.uploads)
	}
	if !strings.Contains(finished.Error, "Docker") {
		t.Fatalf("expected the failure to name Docker, got %q", finished.Error)
	}
}
