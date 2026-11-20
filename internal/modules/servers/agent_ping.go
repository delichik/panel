package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	sshx "panel/internal/platform/ssh"
)

// A working `ping` is an environment prerequisite of agent deployment: the
// network-probe facility hands the agent a set of targets and the agent measures
// latency and packet loss with the node's own ping command. The agent itself is
// not allowed to install packages (that would mean shelling out from the agent
// process), so the deployment pipeline provides it next to the firewall and the
// container runtime. A node that already has ping is left untouched, and the
// step re-runs on the restart-only path so older servers converge on their next
// deployment.

const (
	// agentPingPackageName is the Debian/Ubuntu provider of /bin/ping. Agent
	// deployment is already restricted to UFW-capable (apt-based) distributions
	// by the firewall prerequisite.
	agentPingPackageName = "iputils-ping"

	// agentPingProbeTimeout bounds the read-only `command -v ping` probe.
	agentPingProbeTimeout = time.Minute

	// agentPingInstallTimeout bounds the whole provisioning step. The
	// target-side worst case is two bounded apt invocations:
	// 2*180s = 360s < 600s.
	agentPingInstallTimeout    = 10 * time.Minute
	agentPingAPTTimeoutSeconds = 180
)

// agentPingStatusScript probes for the ping binary without changing anything.
// The stdout marker is parsed by parseAgentPingStatus so deployment tests can
// assert the decision without a real SSH session.
func agentPingStatusScript() string {
	return `if command -v ping >/dev/null 2>&1; then
  echo "panel_agent_ping=present"
else
  echo "panel_agent_ping=missing"
fi`
}

// parseAgentPingStatus reports whether the probe found a usable ping command.
func parseAgentPingStatus(stdout string) bool {
	return strings.Contains(stdout, "panel_agent_ping=present")
}

// agentPingInstallScript installs the distribution ping package non-interactively
// and verifies the binary afterwards. Everything is bounded like the other
// package prerequisites, and the step is idempotent: re-running it on a node
// that already gained ping only re-installs the same package.
func agentPingInstallScript() (string, error) {
	commands, err := remoteops.APTInstallCommands([]string{agentPingPackageName})
	if err != nil {
		return "", err
	}
	lines := []string{
		strings.TrimSpace(remoteops.APTInstallPrelude(agentPingAPTTimeoutSeconds) + commands),
		`if ! command -v ping >/dev/null 2>&1; then`,
		`  echo "[panel] panel_agent_ping_failed reason=binary_missing" >&2`,
		`  exit 1`,
		`fi`,
		fmt.Sprintf(`echo "[panel] panel_agent_ping_ready package=%s"`, agentPingPackageName),
	}
	return strings.Join(lines, "\n"), nil
}

// ensureAgentPing makes sure `ping` is available before the agent is installed
// or restarted. A node that already has it incurs one read-only probe; a node
// without it gets the package installed. A failure stops the deployment with
// the agent left exactly as it was, same as the firewall and Docker steps.
func (s *Service) ensureAgentPing(ctx context.Context, taskID string, srv Server, runner remoteops.Runner) error {
	if s.exec == nil {
		return panelerr.Validation("remote_executor_unavailable", "Remote executor is unavailable")
	}
	res, err := s.exec.ExecSudo(ctx, serverTarget(srv), sshx.CommandSpec{Command: agentPingStatusScript(), Timeout: agentPingProbeTimeout})
	if err != nil {
		return err
	}
	if parseAgentPingStatus(res.Stdout) {
		return nil
	}
	script, err := agentPingInstallScript()
	if err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "installing the ping utility")
	_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("installing the ping utility (package=%s)", agentPingPackageName))
	if _, err := runner.RunSudoLogged(ctx, script, agentPingInstallTimeout); err != nil {
		return panelerr.BadGateway("agent_ping_failed", "Failed to install the ping utility on the target server: "+err.Error())
	}
	return nil
}
