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

// A synchronized system clock is an environment prerequisite of agent
// deployment. The Panel judges an agent report stream stale against the
// sampleAt timestamps the agent attaches, so a node whose clock lags or leads
// the Panel by more than the stream timeout looks "offline" within seconds of
// connecting, even though the agent is running. Agent processes are not allowed
// to install packages, so the deployment pipeline provides chrony next to the
// firewall, the container runtime and ping. The step runs on both the full
// install and the restart-only path so existing servers converge on their next
// deployment. A node that is already installed, enabled/running and synchronized
// only pays for one read-only probe: no remote state is changed, and the
// distribution's chrony.conf is never overwritten. Only the Panel-managed
// sources file is written, so uninstalling chrony or restoring the node leaves a
// standard configuration behind. A failure fails the deployment with
// agent_time_sync_failed before anything on the node is touched, exactly like
// the other prerequisites.

const (
	// agentTimeSyncPackageName is the Debian/Ubuntu chrony package. Agent
	// deployment is already restricted to UFW-capable (apt-based) distributions
	// by the firewall prerequisite.
	agentTimeSyncPackageName = "chrony"

	// agentTimeSyncConfigPath is the Panel-managed sources file. The
	// distribution's own /etc/chrony/chrony.conf is never overwritten, only its
	// confdir include is added when the distribution lacks it.
	agentTimeSyncConfigPath = "/etc/chrony/conf.d/panel-timesync.conf"
	agentTimeSyncConfDir    = "/etc/chrony/conf.d"
	agentTimeSyncConfPath   = "/etc/chrony/chrony.conf"

	// agentTimeSyncMaxOffsetSeconds is the largest absolute system-time offset
	// the Panel still accepts as synchronized. A clock inside one second of the
	// reference cannot trip the report-stream timeout.
	agentTimeSyncMaxOffsetSeconds = 1.0

	// agentTimeSyncProbeTimeout bounds the read-only status probe.
	agentTimeSyncProbeTimeout = time.Minute

	// agentTimeSyncInstallTimeout bounds the whole provisioning step. The
	// target-side worst case is two bounded apt invocations, the synchronization
	// wait window and the cheap service operations, and must stay strictly
	// inside this bound: 2*180s + 20*3s + 15s = 435s < 600s.
	agentTimeSyncInstallTimeout        = 10 * time.Minute
	agentTimeSyncAPTTimeoutSeconds     = 180
	agentTimeSyncSyncPollAttempts      = 20
	agentTimeSyncSyncPollInterval      = 3 * time.Second
	agentTimeSyncServiceOperationBound = 15 * time.Second
)

// agentTimeSyncNTPServers are the public NTP sources the Panel-managed
// configuration asks chrony to use. They are reachable from the Panel's target
// deployments (Aliyun/Tencent regions) and independent of any single vendor.
var agentTimeSyncNTPServers = []string{"ntp.aliyun.com", "ntp.tencent.com"}

// agentTimeSyncSyncedAWK reads `chronyc tracking` from stdin and prints exactly
// "true" or "false". A clock counts as synchronized only when the leap status is
// Normal and the absolute system-time offset is at most one second; anything
// else (empty output, "Not synchronised", a non-numeric offset) becomes false.
// chronyc prints the offset as "0.000012345 seconds fast of NTP time", so the
// first whitespace-delimited token of the System time value is the offset.
// The program deliberately contains no single quotes because the callers embed
// it inside a single-quoted shell argument.
var agentTimeSyncSyncedAWK = `function trim(value) {
  gsub(/^[[:space:]]+/, "", value)
  gsub(/[[:space:]]+$/, "", value)
  return value
}
BEGIN { leap = ""; offset = "" }
/^Leap status/ {
  line = $0
  sub(/^Leap status[[:space:]]*:[[:space:]]*/, "", line)
  leap = trim(line)
}
/^System time/ {
  line = $0
  sub(/^System time[[:space:]]*:[[:space:]]*/, "", line)
  offset = trim(line)
  sub(/[[:space:]].*$/, "", offset)
}
END {
  synced = "false"
  if (tolower(trim(leap)) == "normal" && offset != "" && offset ~ /^[+-]?[0-9]+([.][0-9]+)?$/) {
    value = offset + 0
    if (value < 0) { value = -value }
    if (value <= ` + fmt.Sprintf("%.1f", agentTimeSyncMaxOffsetSeconds) + `) { synced = "true" }
  }
  print synced
}`

// agentTimeSyncSyncedFunction is the shell function shared by the probe and the
// install script so both judge "synchronized" identically.
func agentTimeSyncSyncedFunction() string {
	return strings.Join([]string{
		`panel_chrony_synced() {`,
		`  chronyc tracking 2>/dev/null | awk '` + agentTimeSyncSyncedAWK + `'`,
		`}`,
	}, "\n")
}

// agentTimeSyncStatusScript probes the clock state without changing anything.
// stdout carries stable markers parsed by parseAgentTimeSyncStatus so the
// deployment decision is testable without a real SSH session.
func agentTimeSyncStatusScript() string {
	return strings.Join([]string{
		`if command -v chronyd >/dev/null 2>&1 || command -v chronyc >/dev/null 2>&1 || [ -f ` + agentTimeSyncConfPath + ` ]; then`,
		`  echo "panel_agent_chrony=present"`,
		`else`,
		`  echo "panel_agent_chrony=missing"`,
		`  echo "panel_agent_chrony_service=inactive"`,
		`  echo "panel_agent_chrony_synced=false"`,
		`  exit 0`,
		`fi`,
		agentTimeSyncSyncedFunction(),
		`if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet chrony 2>/dev/null; then`,
		`  echo "panel_agent_chrony_service=active"`,
		`else`,
		`  echo "panel_agent_chrony_service=inactive"`,
		`fi`,
		`synced=false`,
		`if command -v chronyc >/dev/null 2>&1; then`,
		`  synced="$(panel_chrony_synced)"`,
		`fi`,
		`echo "panel_agent_chrony_synced=$synced"`,
	}, "\n")
}

// agentTimeSyncStatus is the deployment decision one probe produces.
type agentTimeSyncStatus struct {
	Installed bool
	Active    bool
	Synced    bool
	Raw       string
}

// parseAgentTimeSyncStatus reads the marker lines agentTimeSyncStatusScript
// emits. Missing markers degrade to the zero value, which makes the caller run
// the provisioning step instead of trusting an unparsable answer.
func parseAgentTimeSyncStatus(stdout string) agentTimeSyncStatus {
	status := agentTimeSyncStatus{Raw: stdout}
	for _, raw := range strings.Split(stdout, "\n") {
		switch strings.TrimSpace(raw) {
		case "panel_agent_chrony=present":
			status.Installed = true
		case "panel_agent_chrony=missing":
			status.Installed = false
		case "panel_agent_chrony_service=active":
			status.Active = true
		case "panel_agent_chrony_service=inactive":
			status.Active = false
		case "panel_agent_chrony_synced=true":
			status.Synced = true
		case "panel_agent_chrony_synced=false":
			status.Synced = false
		}
	}
	return status
}

// agentTimeSyncConfigContent is the static Panel-managed sources file. It only
// adds NTP servers and a bounded makestep: an obviously wrong clock (the case
// this prerequisite exists for) is stepped on the first updates instead of
// taking hours to slew. The distribution's default chrony.conf and files under
// /etc/chrony/sources.d are left alone.
func agentTimeSyncConfigContent() string {
	lines := []string{
		"# Managed by Seamark Panel: the Panel requires a synchronized system clock",
		"# so agent report timestamps are comparable. Do not edit by hand.",
	}
	for _, source := range agentTimeSyncNTPServers {
		lines = append(lines, "server "+source+" iburst")
	}
	lines = append(lines, "makestep 1.0 3")
	return strings.Join(lines, "\n") + "\n"
}

// agentTimeSyncInstallScript composes the idempotent remote script that
// provides chrony: install the package non-interactively, make sure the Panel
// sources directory is included, write the managed sources file, enable and
// restart the service, then wait (bounded) until the clock reports synchronized.
// A failure reports a stable reason marker and exits non-zero so the deployment
// can map it to agent_time_sync_failed. The script never removes files, never
// disables or masks a service, and never overwrites the distribution
// chrony.conf (only appends the confdir include when it is missing).
func agentTimeSyncInstallScript() (string, error) {
	commands, err := remoteops.APTInstallCommands([]string{agentTimeSyncPackageName})
	if err != nil {
		return "", err
	}
	lines := []string{
		"set -eu",
		`panel_fail() {`,
		`  echo "[panel] panel_agent_chrony_failed reason=$1" >&2`,
		`  exit 1`,
		`}`,
		strings.TrimSpace(remoteops.APTInstallPrelude(agentTimeSyncAPTTimeoutSeconds)),
	}
	// Wrap each generated apt invocation so a failed update/install still exits
	// through the stable reason marker instead of a bare non-zero status. The
	// fixed non-interactive flags stay exactly as remoteops defines them.
	for _, command := range strings.Split(strings.TrimSpace(commands), "\n") {
		lines = append(lines, strings.TrimSpace(command)+" || panel_fail apt_failed")
	}
	lines = append(lines,
		agentTimeSyncSyncedFunction(),
		`install -d -m 0755 `+agentTimeSyncConfDir,
		`if [ -f `+agentTimeSyncConfPath+` ]; then`,
		`  if ! grep -Eq '^[[:space:]]*confdir[[:space:]]+`+strings.ReplaceAll(agentTimeSyncConfDir, ".", "[.]")+`([[:space:]]|$)' `+agentTimeSyncConfPath+` 2>/dev/null; then`,
		`    printf '\nconfdir `+agentTimeSyncConfDir+`\n' >> `+agentTimeSyncConfPath,
		`    echo "[panel] panel_agent_chrony_confdir_added path=`+agentTimeSyncConfPath+`"`,
		`  fi`,
		`fi`,
		`cat > `+agentTimeSyncConfigPath+` <<'EOF'`,
		strings.TrimRight(agentTimeSyncConfigContent(), "\n"),
		`EOF`,
		`systemctl enable `+agentTimeSyncPackageName+` >/dev/null 2>&1 || panel_fail service_enable_failed`,
		`systemctl restart `+agentTimeSyncPackageName+` >/dev/null 2>&1 || panel_fail service_restart_failed`,
		`i=0`,
		fmt.Sprintf(`while [ "$i" -lt %d ]; do`, agentTimeSyncSyncPollAttempts),
		`  if [ "$(panel_chrony_synced)" = "true" ]; then`,
		`    echo "[panel] panel_agent_chrony_ready"`,
		`    exit 0`,
		`  fi`,
		`  i=$((i + 1))`,
		fmt.Sprintf(`  sleep %d`, int(agentTimeSyncSyncPollInterval/time.Second)),
		`done`,
		`panel_fail not_synchronised`,
	)
	return strings.Join(lines, "\n"), nil
}

// ensureAgentTimeSync makes sure the target clock is synchronized before the
// agent is installed or restarted. A node that is already installed, active and
// synchronized only incurs one read-only probe; anything else gets the bounded
// provisioning script. A failure stops the deployment with the running agent
// left exactly as it was.
func (s *Service) ensureAgentTimeSync(ctx context.Context, taskID string, srv Server, runner remoteops.Runner) error {
	if s.exec == nil {
		return panelerr.Validation("remote_executor_unavailable", "Remote executor is unavailable")
	}
	res, err := s.exec.ExecSudo(ctx, serverTarget(srv), sshx.CommandSpec{Command: agentTimeSyncStatusScript(), Timeout: agentTimeSyncProbeTimeout})
	if err != nil {
		return err
	}
	status := parseAgentTimeSyncStatus(res.Stdout)
	if status.Installed && status.Active && status.Synced {
		return nil
	}
	script, err := agentTimeSyncInstallScript()
	if err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "preparing", "synchronizing the server clock")
	_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("configuring the system clock (chrony installed=%t active=%t synced=%t, config=%s)", status.Installed, status.Active, status.Synced, agentTimeSyncConfigPath))
	if _, err := runner.RunSudoLogged(ctx, script, agentTimeSyncInstallTimeout); err != nil {
		return panelerr.BadGateway("agent_time_sync_failed", "Failed to synchronize the target server clock: "+err.Error())
	}
	return nil
}
