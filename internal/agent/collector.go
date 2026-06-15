package agent

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"panel/internal/linux"
	"panel/internal/remoteops"
)

type LocalCollector struct{}

func (LocalCollector) OSRelease(_ context.Context) (linux.OSRelease, error) {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return linux.OSRelease{}, err
	}
	info := linux.ParseOSRelease(string(b))
	info.Supported = linux.Supported(info)
	return info, nil
}

func (LocalCollector) SystemTraits(ctx context.Context) (map[string]string, error) {
	out, err := runShell(ctx, systemTraitsScript(), 12*time.Second)
	if err != nil {
		return nil, err
	}
	return parseSystemTraits(out), nil
}

func (LocalCollector) MetricsSnapshot(ctx context.Context, serverID string) (linux.MetricsSnapshot, error) {
	out, err := runShell(ctx, metricsScript(), 15*time.Second)
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	snap, err := linux.ParseMetricsOutput(serverID, out)
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	snap.Time = time.Now().UTC()
	return snap, nil
}

func (LocalCollector) UFWStatus(ctx context.Context) (remoteops.UFWStatus, error) {
	out, err := runShell(ctx, remoteops.UFWStatusScript(), time.Minute)
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	return remoteops.ParseUFWStatus(out), nil
}

func runShell(ctx context.Context, script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-lc", script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func systemTraitsScript() string {
	return `echo "cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 1)"
echo "mem=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' | awk '{print int($1/1024)}' || echo 0)"
echo "disk=$(df -m / 2>/dev/null | awk 'NR==2{print $2}' | awk '{print int($1/1024)}' || echo 0)"
echo "hostname=$(hostname 2>/dev/null || echo unknown)"
echo "arch=$(uname -m 2>/dev/null || echo unknown)"
cpu_model=""
if command -v lscpu >/dev/null 2>&1; then
  cpu_model="$(lscpu | awk -F: '/Model name/{sub(/^[ \t]+/, "", $2); print $2; exit}')"
fi
if [ -z "$cpu_model" ] && [ -r /proc/cpuinfo ]; then
  cpu_model="$(awk -F: '/model name|Hardware|Processor/{sub(/^[ \t]+/, "", $2); print $2; exit}' /proc/cpuinfo)"
fi
echo "cpu_model=${cpu_model:-unknown}"
if command -v ip >/dev/null 2>&1; then
  ip -o addr show scope global | awk '{iface=$2; sub(/@.*/, "", iface); print iface "|" $3 "|" $4}' |
  while IFS='|' read -r iface family address; do
    [ -e "/sys/class/net/$iface/device" ] || continue
    case "$iface" in
      lo|docker*|veth*|br-*|virbr*|cni*|flannel*|cali*|tun*|tap*|wg*|tailscale*|zt*) continue ;;
    esac
    echo "nic=$iface|$family|$address"
  done
elif [ -r /proc/net/dev ]; then
  for iface_path in /sys/class/net/*; do
    [ -e "$iface_path/device" ] || continue
    iface="${iface_path##*/}"
    case "$iface" in
      lo|docker*|veth*|br-*|virbr*|cni*|flannel*|cali*|tun*|tap*|wg*|tailscale*|zt*) continue ;;
    esac
    echo "nic=$iface|link|"
  done
fi
if command -v ufw >/dev/null 2>&1; then
  echo "ufw_installed=true"
  if systemctl is-active --quiet ufw 2>/dev/null || ufw status 2>/dev/null | grep -qi "^Status: active"; then
    echo "ufw_active=true"
  else
    echo "ufw_active=false"
  fi
else
  echo "ufw_installed=false"
  echo "ufw_active=false"
fi`
}

func metricsScript() string {
	return `net_sample() { ts=$(date +%s%N); awk -v ts="$ts" 'NR>2{iface=$1; sub(":", "", iface); if(iface=="lo") next; rx+=$2; tx+=$10} END{printf "%s %.0f %.0f\n", ts, rx, tx}' /proc/net/dev; }; awk '/^cpu /{idle=$5; total=0; for(i=2;i<=NF;i++) total+=$i; print total,idle}' /proc/stat; free -b | awk '/^Mem:/{print $2,$3}'; df -B1 / | awk 'NR==2{print $2,$3}'; net_sample; sleep 1; net_sample; hostname; uname -r; . /etc/os-release && echo "$PRETTY_NAME"; cut -d. -f1 /proc/uptime; cat /proc/loadavg`
}

func parseSystemTraits(out string) map[string]string {
	traits := map[string]string{}
	nics := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "cores":
			traits["sys.cpu_cores"] = value
		case "mem":
			traits["sys.memory_total_mb"] = value
		case "disk":
			traits["sys.disk_total_gb"] = value
		case "hostname":
			traits["sys.hostname"] = value
		case "arch":
			traits["sys.architecture"] = value
		case "cpu_model":
			traits["sys.cpu_model"] = value
		case "nic":
			name, _, _ := strings.Cut(value, "|")
			if value != "" && !isVirtualNetworkInterface(name) {
				nics = append(nics, value)
			}
		case "ufw_installed":
			traits["sys.ufw_installed"] = value
		case "ufw_active":
			traits["sys.ufw_active"] = value
		}
	}
	if len(nics) > 0 {
		traits["sys.network_interfaces"] = strings.Join(nics, ", ")
	}
	return traits
}

func isVirtualNetworkInterface(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "lo" {
		return true
	}
	for _, prefix := range []string{"docker", "veth", "br-", "virbr", "cni", "flannel", "cali", "tun", "tap", "wg", "tailscale", "zt"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	if _, err := strconv.Atoi(name); err == nil {
		return true
	}
	return false
}
