package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
)

// Firewall management is a prerequisite of agent deployment, not an optional
// extra. The Panel exposes the agent port (and the reverse proxy ports) on every
// node it manages, so a node whose firewall it cannot bring into the required
// state does not get an agent at all: an unmanaged firewall on a host we open
// ports on is worse than not deploying. That deliberately narrows the supported
// distributions to the ones with a UFW-capable adapter (Debian, Ubuntu), which
// is already the range of every other package-managing feature.

// agentFirewallRules returns the ports that must be open before the agent can be
// reached from the Panel.
//
// Application ports are deliberately absent: the agent opens those itself from
// each application spec's openFirewall flag during RuntimeReconcile, and UFW
// keeps rules while it is disabled, so rules written earlier take effect the
// moment this step enables the policy.
func agentFirewallRules(srv Server) []remoteops.UFWRule {
	rules := []remoteops.UFWRule{
		{Port: normalizedTCPPort(srv.Port), Protocol: "tcp"},
		{Port: agentControlPort(srv), Protocol: "tcp"},
	}
	if traitEnabled(srv.Traits[reverseProxyEnabledTrait]) {
		for _, port := range reverseProxyTCPPorts {
			rules = append(rules, remoteops.UFWRule{Port: port, Protocol: "tcp"})
		}
	}
	return uniqueUFWRules(rules)
}

// agentFirewallScript composes the remote script that brings the firewall into
// the required state: install UFW when it is missing, always re-assert the base
// rules (ufw allow is idempotent), and enable the default-deny policy only when
// it is not active yet. Enabling always allows the SSH and agent ports first --
// UFWEnableScript owns that ordering, and it is the single place allowed to
// touch the SSH port.
func agentFirewallScript(adapter linux.DistroAdapter, srv Server, needsInstall, needsEnable bool) (string, error) {
	lines := make([]string, 0, 4)
	if needsInstall {
		lines = append(lines, strings.TrimSpace(adapter.UFWInstallScript()))
	}
	rules := agentFirewallRules(srv)
	lines = append(lines, strings.TrimSpace(remoteops.MustUFWAllowScript(rules...)))
	if needsEnable {
		enable, err := remoteops.UFWEnableScript(normalizedTCPPort(srv.Port), agentControlPort(srv))
		if err != nil {
			return "", err
		}
		lines = append(lines, strings.TrimSpace(enable))
	}
	return strings.Join(lines, "\n"), nil
}

// ensureAgentFirewall brings the server firewall into the required state before
// anything is installed on the node. A failure stops the deployment with the
// agent left exactly as it was, so a node with a working agent never loses it
// because the firewall step could not complete.
func (s *Service) ensureAgentFirewall(ctx context.Context, taskID string, srv Server, runner remoteops.Runner) error {
	adapter, ok := linux.AdapterFor(srv.OS)
	if !ok && strings.TrimSpace(srv.OS.ID) == "" {
		// The distribution has not been recorded yet, which happens when a manual
		// deployment is started before the first information collection finished.
		// Probe it instead of rejecting the server for an empty record.
		if probed, probeErr := s.detectOS(ctx, srv, serverTarget(srv)); probeErr == nil {
			srv.OS = probed
			adapter, ok = linux.AdapterFor(probed)
		}
	}
	if !ok || !adapter.SupportsUFW() {
		return panelerr.Validation("agent_firewall_unsupported", "The Panel manages the firewall through UFW only, and this server's distribution is not supported")
	}
	status, err := s.fetchUFWStatusSSH(ctx, srv)
	if err != nil {
		return err
	}
	if status.Installed && status.Active {
		return nil
	}
	script, err := agentFirewallScript(adapter, srv, !status.Installed, !status.Active)
	if err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "securing the server firewall")
	_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("configuring the server firewall (ufw installed=%t active=%t, ports=%s)", status.Installed, status.Active, ufwRuleSummary(agentFirewallRules(srv))))
	if _, err := runner.RunSudoLogged(ctx, script, ufwInstallTimeout); err != nil {
		return err
	}
	return nil
}

// ufwRuleSummary renders the rule ports for the task log so an operator can see
// what was opened without reading the script.
func ufwRuleSummary(rules []remoteops.UFWRule) string {
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		protocol := rule.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		parts = append(parts, strconv.Itoa(rule.Port)+"/"+protocol)
	}
	return strings.Join(parts, ",")
}
