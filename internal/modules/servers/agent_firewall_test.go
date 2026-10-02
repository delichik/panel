package server

import (
	"context"
	"strconv"
	"strings"
	"testing"

	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
)

func TestAgentFirewallRulesCoverEveryManagedPort(t *testing.T) {
	base := Server{ID: "srv_1", Host: "10.0.0.1", Port: 22022}

	plain := agentFirewallRules(base)
	want := []int{22022, defaultAgentPort}
	if len(plain) != len(want) {
		t.Fatalf("expected %d rules, got %#v", len(want), plain)
	}
	for index, port := range want {
		if plain[index].Port != port || plain[index].Protocol != "tcp" {
			t.Fatalf("expected rule %d/%d, got %#v", port, index, plain[index])
		}
	}

	// The agent always listens on the internal default port, so the rule is the
	// same whether or not agent.url is configured. The port is now derived
	// through agentControlPort -- shared with the rest of the UFW code instead of
	// duplicated as a literal -- rather than through a hard-coded constant.
	configured := base
	configured.Traits = map[string]string{
		"agent.enabled": "true",
		"agent.url":     "https://10.0.0.1:" + strconv.Itoa(defaultAgentPort),
	}
	configuredRules := agentFirewallRules(configured)
	if len(configuredRules) != 2 || configuredRules[1].Port != defaultAgentPort {
		t.Fatalf("expected the internal agent port, got %#v", configuredRules)
	}

	// The reverse proxy ports join only when the facility marks this node.
	proxy := configured
	proxy.Traits = map[string]string{
		"agent.enabled":          "true",
		"agent.url":              "https://10.0.0.1:" + strconv.Itoa(defaultAgentPort),
		reverseProxyEnabledTrait: "true",
	}
	ports := map[int]bool{}
	for _, rule := range agentFirewallRules(proxy) {
		ports[rule.Port] = true
	}
	for _, port := range []int{22022, defaultAgentPort, 80, 443} {
		if !ports[port] {
			t.Fatalf("expected port %d in %#v", port, agentFirewallRules(proxy))
		}
	}
	// A quiet node must not get the reverse proxy ports.
	if len(agentFirewallRules(base)) != 2 {
		t.Fatalf("expected only the SSH and agent rules without the facility trait, got %#v", agentFirewallRules(base))
	}
}

func TestAgentFirewallScriptOrdersInstallAllowAndEnable(t *testing.T) {
	adapter, ok := linux.AdapterFor(linux.OSRelease{ID: "debian", VersionID: "13"})
	if !ok {
		t.Fatal("expected the debian adapter")
	}
	srv := Server{ID: "srv_1", Host: "10.0.0.1", Port: 22022}

	// Nothing installed and nothing enabled: install, allow, then enable.
	full, err := agentFirewallScript(adapter, srv, true, true)
	if err != nil {
		t.Fatal(err)
	}
	installIndex := strings.Index(full, "apt_get install -y ufw")
	allowSSHIndex := strings.Index(full, "ufw allow 22022/tcp")
	allowAgentIndex := strings.Index(full, "ufw allow "+strconv.Itoa(defaultAgentPort)+"/tcp")
	enableIndex := strings.Index(full, "ufw --force enable")
	if installIndex < 0 || allowSSHIndex < 0 || allowAgentIndex < 0 || enableIndex < 0 {
		t.Fatalf("expected install, both allows and enable in:\n%s", full)
	}
	if installIndex >= allowSSHIndex || allowSSHIndex >= enableIndex || allowAgentIndex >= enableIndex {
		t.Fatalf("install and allows must precede enable:\n%s", full)
	}
	// The enable step is the one place allowed to change the running policy, and
	// it may only do so after the SSH port is allowed back (UFW-API-006). The
	// generic "no destructive verbs" assertion cannot be used here because this
	// script is supposed to enable; it is applied to the rules-only variant below.
	if strings.Index(full, "ufw --force enable") < strings.Index(full, "ensuring SSH access before enabling UFW") {
		t.Fatalf("SSH access must be ensured before enabling:\n%s", full)
	}

	// Already installed but not enabled: no apt step, still enabled.
	enableOnly, err := agentFirewallScript(adapter, srv, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enableOnly, "apt_get install") {
		t.Fatalf("did not expect an install step:\n%s", enableOnly)
	}
	if !strings.Contains(enableOnly, "ufw --force enable") {
		t.Fatalf("expected the enable step:\n%s", enableOnly)
	}

	// Installed and already enabled: the script must only re-assert rules and
	// must never touch the running policy.
	rulesOnly, err := agentFirewallScript(adapter, srv, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rulesOnly, "apt_get install") || strings.Contains(rulesOnly, "ufw --force enable") {
		t.Fatalf("expected rules only:\n%s", rulesOnly)
	}
	if !strings.Contains(rulesOnly, "ufw allow 22022/tcp") {
		t.Fatalf("expected the SSH rule to be re-asserted:\n%s", rulesOnly)
	}
	// Nothing destructive may run when the firewall is already in place.
	assertNoDestructiveUFWCommands(t, rulesOnly)
}

// TestEnsureAgentFirewallExemptsAndRefuses covers the two decisions around the
// prerequisite: NAT servers are exempt because their public ports belong to the
// provider, and an unsupported distribution is refused rather than silently
// deployed without a firewall.
func TestEnsureAgentFirewallExemptsAndRefuses(t *testing.T) {
	debian := linux.OSRelease{ID: "debian", VersionID: "13", Supported: true}

	t.Run("NAT servers are exempt", func(t *testing.T) {
		exec := &ufwInstallFakeExec{}
		svc, _, _ := testServerService(t, exec)
		srv := Server{ID: "srv_nat", Host: "10.0.0.1", Port: 22, Kind: ServerKindNAT, OS: debian}
		if err := svc.ensureAgentFirewall(context.Background(), "task_1", srv, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatalf("expected the NAT server to be exempt, got %v", err)
		}
		if exec.installCommand != "" {
			t.Fatalf("no firewall command may run on a NAT server, got %q", exec.installCommand)
		}
	})

	t.Run("unsupported distributions are refused", func(t *testing.T) {
		exec := &ufwInstallFakeExec{}
		svc, _, _ := testServerService(t, exec)
		srv := Server{ID: "srv_rhel", Host: "10.0.0.1", Port: 22, OS: linux.OSRelease{ID: "rhel", VersionID: "9"}}
		err := svc.ensureAgentFirewall(context.Background(), "task_1", srv, remoteops.Runner{Exec: exec})
		if err == nil || !strings.Contains(err.Error(), "UFW only") {
			t.Fatalf("expected agent_firewall_unsupported, got %v", err)
		}
		if exec.installCommand != "" {
			t.Fatalf("no firewall command may run on an unsupported distribution, got %q", exec.installCommand)
		}
	})

	t.Run("an installed and active firewall is left alone", func(t *testing.T) {
		exec := &ufwEnableFakeExec{installed: true, active: true}
		svc, _, _ := testServerService(t, exec)
		srv := Server{ID: "srv_ok", Host: "10.0.0.1", Port: 22, OS: debian}
		if err := svc.ensureAgentFirewall(context.Background(), "task_1", srv, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		if len(exec.commands) != 0 {
			t.Fatalf("expected no mutation when the firewall is already in place, got %#v", exec.commands)
		}
	})

	t.Run("a missing firewall is installed then enabled", func(t *testing.T) {
		exec := &ufwEnableFakeExec{}
		svc, _, _ := testServerService(t, exec)
		srv := Server{ID: "srv_new", Host: "10.0.0.1", Port: 22022, OS: debian}
		if err := svc.ensureAgentFirewall(context.Background(), "task_1", srv, remoteops.Runner{Exec: exec}); err != nil {
			t.Fatal(err)
		}
		commands := strings.Join(exec.commands, "\n---\n")
		for _, want := range []string{"apt_get install -y ufw", "ufw allow 22022/tcp", "ufw allow " + strconv.Itoa(defaultAgentPort) + "/tcp", "ufw --force enable"} {
			if !strings.Contains(commands, want) {
				t.Fatalf("expected %q in:\n%s", want, commands)
			}
		}
	})
}

// TestAgentDeployRunsFirewallBeforeTouchingTheAgent pins the ordering: the
// firewall is a prerequisite, and a failing prerequisite must not disturb an
// agent that is already running.
func TestAgentDeployRunsFirewallBeforeTouchingTheAgent(t *testing.T) {
	exec := &trackingAgentDeployExec{agentArchFakeExec: agentArchFakeExec{arch: "x86_64"}}
	svc, taskSvc, store := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	// An unsupported distribution: the prerequisite cannot be met.
	setServerOS(t, store, srv.ID, "rhel", "9")
	setServerTraits(t, store, srv.ID, map[string]string{})
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
	if finished.Status != tasks.StatusFailed {
		t.Fatalf("expected the deployment to fail without a usable firewall, got %#v", finished)
	}
	if exec.uploads != 0 {
		t.Fatalf("the agent binary must not be uploaded when the prerequisite fails, got %d uploads", exec.uploads)
	}
	if !strings.Contains(finished.Error, "UFW only") {
		t.Fatalf("expected the failure to name the firewall prerequisite, got %q", finished.Error)
	}
}
