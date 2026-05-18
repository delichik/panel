package linux

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"panel/internal/panelerr"
	"panel/internal/sshx"
)

type DebianAdapter struct{}

func (DebianAdapter) ID() string { return "debian" }

func (DebianAdapter) Supports(info OSRelease) bool {
	return info.ID == "debian" && (info.VersionID == "12" || info.VersionID == "13")
}

func (DebianAdapter) ReadStatus(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (SystemStatus, error) {
	cmd := "printf '%s\\n' \"$(hostname)\" \"$(uname -r)\" \"$(. /etc/os-release && echo \"$PRETTY_NAME\")\" \"$(date -u +%s)\" \"$(cut -d. -f1 /proc/uptime)\" \"$(cat /proc/loadavg)\""
	res, err := exec.Exec(ctx, target, sshx.CommandSpec{Command: cmd})
	if err != nil {
		return SystemStatus{}, err
	}
	lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
	for len(lines) < 6 {
		lines = append(lines, "")
	}
	epoch, _ := strconv.ParseInt(strings.TrimSpace(lines[3]), 10, 64)
	uptime, _ := strconv.ParseInt(strings.TrimSpace(lines[4]), 10, 64)
	return SystemStatus{
		Hostname:      strings.TrimSpace(lines[0]),
		KernelVersion: strings.TrimSpace(lines[1]),
		OSVersion:     strings.TrimSpace(lines[2]),
		ServerTime:    time.Unix(epoch, 0).UTC(),
		UptimeSeconds: uptime,
		LoadAverage:   strings.TrimSpace(lines[5]),
	}, nil
}

func (DebianAdapter) CollectMetrics(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (MetricsSnapshot, error) {
	cmd := `awk '/^cpu /{idle=$5; total=0; for(i=2;i<=NF;i++) total+=$i; print total,idle}' /proc/stat; free -b | awk '/^Mem:/{print $2,$3}'; df -B1 / | awk 'NR==2{print $2,$3}'; awk 'NR>2{rx+=$2; tx+=$10} END{print rx,tx}' /proc/net/dev; hostname; uname -r; . /etc/os-release && echo "$PRETTY_NAME"; cut -d. -f1 /proc/uptime; cat /proc/loadavg`
	res, err := exec.Exec(ctx, target, sshx.CommandSpec{Command: cmd})
	if err != nil {
		return MetricsSnapshot{}, err
	}
	return ParseMetricsOutput(target.ServerID, res.Stdout)
}

func (DebianAdapter) ListUpgradeable(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) ([]PackageUpdate, error) {
	res, err := exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "apt-get update >/dev/null && apt list --upgradable 2>/dev/null"})
	if err != nil {
		return nil, err
	}
	return ParseAptListUpgradable(res.Stdout), nil
}

func (DebianAdapter) UpgradeSelected(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, packages []string, log LogSink) error {
	if len(packages) == 0 {
		return panelerr.Validation("packages_required", "At least one package is required")
	}
	for _, p := range packages {
		if !regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`).MatchString(p) {
			return panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
		}
	}
	cmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade " + strings.Join(packages, " ")
	return runLogged(ctx, exec, target, cmd, log)
}

func (DebianAdapter) UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, log LogSink) error {
	return runLogged(ctx, exec, target, "DEBIAN_FRONTEND=noninteractive apt-get dist-upgrade -y", log)
}

func runLogged(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, cmd string, log LogSink) error {
	res, err := exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: cmd})
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if strings.TrimSpace(line) != "" {
			_ = log.AppendLog(ctx, "stdout", line)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stderr), "\n") {
		if strings.TrimSpace(line) != "" {
			_ = log.AppendLog(ctx, "stderr", line)
		}
	}
	return err
}

func ParseAptListUpgradable(out string) []PackageUpdate {
	var updates []PackageUpdate
	re := regexp.MustCompile(`^([^/\s]+)/([^\s]+)\s+([^\s]+)\s+.*\[upgradable from:\s*([^\]]+)\]`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing...") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 5 {
			updates = append(updates, PackageUpdate{Name: m[1], Source: m[2], CandidateVersion: m[3], InstalledVersion: m[4]})
		}
	}
	return updates
}

func ParseMetricsOutput(serverID, out string) (MetricsSnapshot, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 9 {
		return MetricsSnapshot{}, fmt.Errorf("metrics output has %d lines, expected at least 9", len(lines))
	}
	ints := func(line string) []int64 {
		fields := strings.Fields(line)
		var vals []int64
		for _, f := range fields {
			n, _ := strconv.ParseInt(f, 10, 64)
			vals = append(vals, n)
		}
		return vals
	}
	cpu := ints(lines[0])
	mem := ints(lines[1])
	disk := ints(lines[2])
	netv := ints(lines[3])
	uptime, _ := strconv.ParseInt(strings.TrimSpace(lines[7]), 10, 64)
	usage := 0.0
	if len(cpu) >= 2 && cpu[0] > 0 {
		usage = 100 - (float64(cpu[1]) / float64(cpu[0]) * 100)
	}
	return MetricsSnapshot{
		ServerID:           serverID,
		Time:               time.Now().UTC(),
		CPUUsagePercent:    usage,
		MemoryTotalBytes:   pick(mem, 0),
		MemoryUsedBytes:    pick(mem, 1),
		DiskTotalBytes:     pick(disk, 0),
		DiskUsedBytes:      pick(disk, 1),
		NetworkRxBytesRate: float64(pick(netv, 0)),
		NetworkTxBytesRate: float64(pick(netv, 1)),
		Status:             SystemStatus{Hostname: lines[4], KernelVersion: lines[5], OSVersion: lines[6], UptimeSeconds: uptime, LoadAverage: lines[8]},
	}, nil
}

func pick(v []int64, i int) int64 {
	if len(v) <= i {
		return 0
	}
	return v[i]
}
