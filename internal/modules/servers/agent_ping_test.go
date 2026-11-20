package server

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux/remoteops"
	sshx "panel/internal/platform/ssh"
)

// pingProvisionFakeExec serves the read-only ping probe and records every sudo
// command so the test can assert whether an install step ran.
type pingProvisionFakeExec struct {
	present      bool
	failProbe    bool
	failInstall  bool
	sudoCommands []string
}

func (f *pingProvisionFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *pingProvisionFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
	if strings.Contains(command.Command, "apt_get update") {
		if f.failInstall {
			return sshx.CommandResult{ExitCode: 1}, errors.New("apt install failed")
		}
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "panel_agent_ping") {
		if f.failProbe {
			return sshx.CommandResult{ExitCode: 1}, errors.New("probe failed")
		}
		if f.present {
			return sshx.CommandResult{Stdout: "panel_agent_ping=present\n", ExitCode: 0}, nil
		}
		return sshx.CommandResult{Stdout: "panel_agent_ping=missing\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *pingProvisionFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *pingProvisionFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func TestAgentPingScripts(t *testing.T) {
	probe := agentPingStatusScript()
	if !strings.Contains(probe, "command -v ping") || !strings.Contains(probe, "panel_agent_ping=present") {
		t.Fatalf("probe script must check the ping binary and emit a marker:\n%s", probe)
	}
	install, err := agentPingInstallScript()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"apt_get update",
		"apt_get install -y " + agentPingPackageName,
		"command -v ping",
		`panel_agent_ping_ready package=` + agentPingPackageName,
		`panel_agent_ping_failed reason=binary_missing`,
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("expected %q in install script:\n%s", want, install)
		}
	}
}

// TestAgentPingProvisioningWorstCaseFitsInsideThePanelBound pins the same
// invariant the Docker prerequisite has: two bounded apt invocations must finish
// strictly inside the Panel-side bound so the script can report its own failure.
func TestAgentPingProvisioningWorstCaseFitsInsideThePanelBound(t *testing.T) {
	worst := 2 * time.Duration(agentPingAPTTimeoutSeconds) * time.Second
	if worst >= agentPingInstallTimeout {
		t.Fatalf("target-side worst case %s must stay strictly inside the Panel bound %s", worst, agentPingInstallTimeout)
	}
}

func TestAgentPingScriptIsSyntacticallyValid(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	install, err := agentPingInstallScript()
	if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"probe": agentPingStatusScript(), "install": install} {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s script failed bash -n: %v\n%s\n--- script ---\n%s", name, err, out, script)
		}
	}
}

func TestEnsureAgentPing(t *testing.T) {
	base := Server{ID: "srv_1", Host: "10.0.0.1", Port: 22}

	t.Run("an existing ping is left alone", func(t *testing.T) {
		exec := &pingProvisionFakeExec{present: true}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentPing(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		if len(exec.sudoCommands) != 1 || !strings.Contains(exec.sudoCommands[0], "panel_agent_ping=present") {
			t.Fatalf("expected only the read-only probe, got %#v", exec.sudoCommands)
		}
	})

	t.Run("a missing ping is installed", func(t *testing.T) {
		exec := &pingProvisionFakeExec{}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentPing(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(exec.sudoCommands, "\n---\n")
		for _, want := range []string{"panel_agent_ping=missing", "apt_get install -y " + agentPingPackageName} {
			if !strings.Contains(commands, want) {
				t.Fatalf("expected %q in:\n%s", want, commands)
			}
		}
	})

	t.Run("a failing probe surfaces", func(t *testing.T) {
		exec := &pingProvisionFakeExec{failProbe: true}
		svc, _, _ := testServerService(t, exec)
		if err := svc.ensureAgentPing(context.Background(), "task_1", base, remoteops.Runner{Exec: exec}); err == nil {
			t.Fatal("expected the probe failure to surface")
		}
	})

	t.Run("a failing install is reported as agent_ping_failed", func(t *testing.T) {
		exec := &pingProvisionFakeExec{failInstall: true}
		svc, _, _ := testServerService(t, exec)
		err := svc.ensureAgentPing(context.Background(), "task_1", base, remoteops.Runner{Exec: exec})
		if err == nil {
			t.Fatal("expected the provisioning failure to surface")
		}
		if !strings.Contains(err.Error(), "ping utility") {
			t.Fatalf("expected a ping provisioning error, got %v", err)
		}
	})
}

// TestAgentDeployProvisionsPingAfterDocker pins the prerequisite order: the
// network-probe facility relies on ping, so the deployment step must run after
// the firewall and the container runtime and still before touching the agent.
func TestAgentDeployProvisionsPingAfterDocker(t *testing.T) {
	exec := &trackingAgentDeployExec{agentArchFakeExec: agentArchFakeExec{arch: "x86_64"}}
	exec.onSudo = func(command string) (sshx.CommandResult, error) {
		switch {
		case strings.Contains(command, "panel_ufw_installed"):
			return sshx.CommandResult{Stdout: "panel_ufw_installed=true\nStatus: active\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_docker_installed"):
			return sshx.CommandResult{Stdout: "panel_docker_installed=true\npanel_docker_active=true\npanel_docker_ready=true\n", ExitCode: 0}, nil
		case strings.Contains(command, "panel_agent_ping"):
			return sshx.CommandResult{Stdout: "panel_agent_ping=present\n", ExitCode: 0}, nil
		}
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	svc, taskSvc, store := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerOS(t, store, srv.ID, "debian", "13")
	setServerTraits(t, store, srv.ID, map[string]string{
		"agent.enabled": "true",
		"agent.url":     "https://127.0.0.1:9786",
		"agent.status":  "compatible",
		"agent.version": agentcontract.Version,
	})
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)

	task, err := svc.DeployAgent(context.Background(), srv.ID)
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
	pingIndex := strings.Index(commands, "panel_agent_ping=present")
	if firewallIndex < 0 || dockerIndex < 0 || pingIndex < 0 {
		t.Fatalf("expected firewall, docker and ping probes in:\n%s", commands)
	}
	if firewallIndex >= dockerIndex || dockerIndex >= pingIndex {
		t.Fatalf("expected firewall < docker < ping order in:\n%s", commands)
	}
}
