package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	sshx "panel/internal/platform/ssh"
)

// Docker is the second prerequisite of agent deployment, next to the firewall:
// the agent reports Docker health and the Panel only treats an agent as
// compatible when the runtime answers on the configured endpoint, so a node
// that is missing Docker gets it installed during the same deployment. The
// step runs through SSH before anything on the node is touched, exactly like
// the firewall prerequisite, and it deliberately limits itself to the default
// local socket: a custom Docker host is an operator-managed endpoint the Panel
// must not overwrite with a local install.

const (
	agentDockerPackageName = "docker.io"

	// agentDockerProbeTimeout bounds the read-only status probe.
	agentDockerProbeTimeout = time.Minute

	// agentDockerInstallTimeout bounds the whole provisioning step. The
	// target-side worst case is two bounded apt invocations (update, install)
	// plus the readiness wait below, and must stay strictly inside this bound:
	// 2*180s + 90*2s = 540s < 600s.
	agentDockerInstallTimeout    = 10 * time.Minute
	agentDockerAPTTimeoutSeconds = 180

	// agentDockerReadyPollIntervalSeconds and agentDockerReadyPollAttempts
	// bound the wait for the API socket after the service was started.
	agentDockerReadyPollIntervalSeconds = 2
	agentDockerReadyPollAttempts        = 90
)

// agentDockerScript composes the remote script that provides the container
// runtime: install the distribution package only when the daemon is missing,
// make sure the service is enabled and running, then wait until the API socket
// answers. The step is idempotent and additive: an installed daemon is never
// reinstalled, a running daemon is never restarted, and no Docker resource is
// ever removed.
func agentDockerScript(needsInstall bool) (string, error) {
	lines := []string{"set -eu"}
	if needsInstall {
		commands, err := remoteops.APTInstallCommands([]string{agentDockerPackageName})
		if err != nil {
			return "", err
		}
		lines = append(lines, strings.TrimSpace(remoteops.APTInstallPrelude(agentDockerAPTTimeoutSeconds)+commands))
		lines = append(lines, `echo "[panel] panel_agent_docker installed package=`+agentDockerPackageName)
	}
	lines = append(lines,
		`echo "[panel] panel_agent_docker starting the docker service"`,
		`systemctl enable --now docker`,
		`i=0`,
		fmt.Sprintf(`while [ "$i" -lt %d ]; do`, agentDockerReadyPollAttempts),
		`  if [ -S /var/run/docker.sock ] && systemctl is-active --quiet docker; then`,
		`    echo "[panel] panel_agent_docker_ready"`,
		`    exit 0`,
		`  fi`,
		`  i=$((i + 1))`,
		fmt.Sprintf(`  sleep %d`, agentDockerReadyPollIntervalSeconds),
		`done`,
		`echo "[panel] panel_agent_docker_failed reason=not_ready" >&2`,
		`exit 1`,
	)
	return strings.Join(lines, "\n"), nil
}

// ensureAgentDocker provides the container runtime before the agent is
// touched. A runtime that is already usable is left alone; a custom Docker
// host is operator-managed and never gets a local install behind it. A failure
// stops the deployment with the agent left exactly as it was.
func (s *Service) ensureAgentDocker(ctx context.Context, taskID string, srv Server, runner remoteops.Runner) error {
	if normalizeDockerHost(srv.DockerHost) != agentcontract.DefaultDockerHost {
		return nil
	}
	status, err := s.fetchDockerStatusSSH(ctx, srv)
	if err != nil {
		return err
	}
	if status.Installed && status.Active && status.Ready {
		return nil
	}
	script, err := agentDockerScript(!status.Installed)
	if err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "installing the container runtime")
	_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("configuring the container runtime (docker installed=%t active=%t ready=%t, host=%s)", status.Installed, status.Active, status.Ready, agentcontract.DefaultDockerHost))
	if _, err := runner.RunSudoLogged(ctx, script, agentDockerInstallTimeout); err != nil {
		return panelerr.BadGateway("agent_docker_failed", "Failed to install or start Docker on the target server: "+err.Error())
	}
	return nil
}

func (s *Service) fetchDockerStatusSSH(ctx context.Context, srv Server) (remoteops.DockerStatus, error) {
	res, err := s.exec.ExecSudo(ctx, serverTarget(srv), sshx.CommandSpec{Command: remoteops.DockerStatusScript(), Timeout: agentDockerProbeTimeout})
	if err != nil {
		return remoteops.DockerStatus{}, err
	}
	return remoteops.ParseDockerStatus(res.Stdout), nil
}
