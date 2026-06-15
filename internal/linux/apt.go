package linux

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"panel/internal/panelerr"
	"panel/internal/remoteops"
	"panel/internal/sshx"
)

type aptAdapter struct{}

const (
	packageListTimeout    = 3 * time.Minute
	packageUpgradeTimeout = time.Hour
)

func (aptAdapter) ReadStatus(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (SystemStatus, error) {
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

func (aptAdapter) CollectMetrics(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (MetricsSnapshot, error) {
	cmd := `net_sample() { ts=$(date +%s%N); awk -v ts="$ts" 'NR>2{iface=$1; sub(":", "", iface); if(iface=="lo") next; rx+=$2; tx+=$10} END{printf "%s %.0f %.0f\n", ts, rx, tx}' /proc/net/dev; }; awk '/^cpu /{idle=$5; total=0; for(i=2;i<=NF;i++) total+=$i; print total,idle}' /proc/stat; free -b | awk '/^Mem:/{print $2,$3}'; df -B1 / | awk 'NR==2{print $2,$3}'; net_sample; sleep 1; net_sample; hostname; uname -r; . /etc/os-release && echo "$PRETTY_NAME"; cut -d. -f1 /proc/uptime; cat /proc/loadavg`
	res, err := exec.Exec(ctx, target, sshx.CommandSpec{Command: cmd})
	if err != nil {
		return MetricsSnapshot{}, err
	}
	return ParseMetricsOutput(target.ServerID, res.Stdout)
}

func (aptAdapter) ListUpgradeable(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) ([]PackageUpdate, error) {
	res, err := exec.ExecSudo(ctx, target, sshx.CommandSpec{
		Command: "apt-get update >/dev/null && apt list --upgradable 2>/dev/null",
		Timeout: packageListTimeout,
	})
	if err != nil {
		return nil, err
	}
	return ParseAptListUpgradable(res.Stdout), nil
}

func (aptAdapter) UpgradeSelected(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, packages []string, log LogSink) error {
	if len(packages) == 0 {
		return panelerr.Validation("packages_required", "At least one package is required")
	}
	for _, p := range packages {
		if !regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`).MatchString(p) {
			return panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
		}
	}
	cmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade " + strings.Join(packages, " ")
	_, err := remoteops.Runner{Exec: exec, Target: target, Log: log}.RunSudoLogged(ctx, cmd, packageUpgradeTimeout)
	return err
}

func (aptAdapter) UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, log LogSink) error {
	_, err := remoteops.Runner{Exec: exec, Target: target, Log: log}.RunSudoLogged(ctx, "DEBIAN_FRONTEND=noninteractive apt-get dist-upgrade -y", packageUpgradeTimeout)
	return err
}

func (aptAdapter) SupportsUFW() bool {
	return true
}

func (aptAdapter) UFWInstallScript() string {
	return remoteops.MustAPTInstallScript("ufw") + "ufw --version\n"
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
	if len(lines) < 10 {
		return MetricsSnapshot{}, fmt.Errorf("metrics output has %d lines, expected at least 10", len(lines))
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
	rxRate, txRate := networkBytesPerSecond(ints(lines[3]), ints(lines[4]))
	uptime, _ := strconv.ParseInt(strings.TrimSpace(lines[8]), 10, 64)
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
		NetworkRxBytesRate: rxRate,
		NetworkTxBytesRate: txRate,
		Status:             SystemStatus{Hostname: lines[5], KernelVersion: lines[6], OSVersion: lines[7], UptimeSeconds: uptime, LoadAverage: lines[9]},
	}, nil
}

func networkBytesPerSecond(first, second []int64) (float64, float64) {
	if len(first) < 3 || len(second) < 3 {
		return 0, 0
	}
	elapsed := float64(second[0]-first[0]) / float64(time.Second)
	if elapsed <= 0 {
		return 0, 0
	}
	rx := second[1] - first[1]
	tx := second[2] - first[2]
	if rx < 0 {
		rx = 0
	}
	if tx < 0 {
		tx = 0
	}
	return float64(rx) / elapsed, float64(tx) / elapsed
}

func pick(v []int64, i int) int64 {
	if len(v) <= i {
		return 0
	}
	return v[i]
}
