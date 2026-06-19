package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	traits := map[string]string{
		"sys.cpu_cores":       strconv.Itoa(runtime.NumCPU()),
		"sys.architecture":    machineArchitecture(),
		"sys.memory_total_mb": strconv.FormatInt(readMemoryTotalBytes()/(1024*1024), 10),
		"sys.disk_total_gb":   strconv.FormatUint(readRootDiskTotalBytes()/(1024*1024*1024), 10),
		"sys.cpu_model":       readCPUModel(),
	}
	if hostname, err := os.Hostname(); err == nil {
		traits["sys.hostname"] = hostname
	}
	if nics := physicalNetworkInterfaces(); len(nics) > 0 {
		traits["sys.network_interfaces"] = strings.Join(nics, ", ")
	}
	status, err := (LocalCollector{}).UFWStatus(ctx)
	if err == nil {
		traits["sys.ufw_installed"] = strconv.FormatBool(status.Installed)
		traits["sys.ufw_active"] = strconv.FormatBool(status.Active)
	}
	return traits, nil
}

func (LocalCollector) MetricsSnapshot(ctx context.Context, serverID string) (linux.MetricsSnapshot, error) {
	firstCPU, err := readCPUStat()
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	firstNet, err := readNetworkTotals()
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return linux.MetricsSnapshot{}, ctx.Err()
	case <-timer.C:
	}
	secondCPU, err := readCPUStat()
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	secondNet, err := readNetworkTotals()
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	memTotal, memAvailable := readMemoryStats()
	diskTotal, diskUsed := readRootDiskUsage()
	hostname, _ := os.Hostname()
	osInfo, _ := (LocalCollector{}).OSRelease(ctx)
	kernel := readKernelVersion()
	uptime := readUptimeSeconds()
	load := readFirstLine("/proc/loadavg")
	load1, load5, load15 := parseLoadAverage(load)
	load = normalizedLoadAverage(load)
	return linux.MetricsSnapshot{
		ServerID:           serverID,
		Time:               time.Now().UTC(),
		CPUUsagePercent:    cpuUsage(firstCPU, secondCPU),
		MemoryTotalBytes:   memTotal,
		MemoryUsedBytes:    maxInt64(0, memTotal-memAvailable),
		DiskTotalBytes:     int64(diskTotal),
		DiskUsedBytes:      int64(diskUsed),
		NetworkRxBytesRate: float64(maxInt64(0, secondNet.rx-firstNet.rx)),
		NetworkTxBytesRate: float64(maxInt64(0, secondNet.tx-firstNet.tx)),
		Status: linux.SystemStatus{
			Hostname:      hostname,
			KernelVersion: kernel,
			OSVersion:     osInfo.PrettyName,
			ServerTime:    time.Now().UTC(),
			UptimeSeconds: uptime,
			LoadAverage:   load,
			Load1:         load1,
			Load5:         load5,
			Load15:        load15,
		},
	}, nil
}

func parseLoadAverage(raw string) (float64, float64, float64) {
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return 0, 0, 0
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15
}

func normalizedLoadAverage(raw string) string {
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return ""
	}
	return strings.Join(fields[:3], " ")
}

func (LocalCollector) UFWStatus(ctx context.Context) (remoteops.UFWStatus, error) {
	if _, err := exec.LookPath("ufw"); err != nil {
		return remoteops.UFWStatus{Installed: false, Status: "not_installed"}, nil
	}
	verbose, err := runCommand(ctx, time.Minute, "ufw", "status", "verbose")
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	numbered, err := runCommand(ctx, time.Minute, "ufw", "status", "numbered")
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	raw := "panel_ufw_installed=true\n" + verbose + "\npanel_ufw_numbered_begin\n" + numbered
	return remoteops.ParseUFWStatus(raw), nil
}

func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(runCtx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func machineArchitecture() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func readCPUModel() string {
	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "model name", "hardware", "processor":
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return "unknown"
}

func physicalNetworkInterfaces() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range interfaces {
		if isVirtualNetworkInterface(iface.Name) {
			continue
		}
		if _, err := os.Stat("/sys/class/net/" + iface.Name + "/device"); err != nil {
			continue
		}
		addresses, _ := iface.Addrs()
		if len(addresses) == 0 {
			out = append(out, iface.Name+"|link|")
			continue
		}
		for _, address := range addresses {
			family := "inet6"
			if ip, _, err := net.ParseCIDR(address.String()); err == nil && ip.To4() != nil {
				family = "inet"
			}
			out = append(out, iface.Name+"|"+family+"|"+address.String())
		}
	}
	return out
}

type cpuStat struct {
	total uint64
	idle  uint64
}

func readCPUStat() (cpuStat, error) {
	line := readFirstLine("/proc/stat")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuStat{}, errors.New("invalid /proc/stat cpu line")
	}
	var out cpuStat
	for index, field := range fields[1:] {
		value, _ := strconv.ParseUint(field, 10, 64)
		out.total += value
		if index == 3 || index == 4 {
			out.idle += value
		}
	}
	return out, nil
}

func cpuUsage(first, second cpuStat) float64 {
	total := second.total - first.total
	idle := second.idle - first.idle
	if total == 0 || idle > total {
		return 0
	}
	return 100 * float64(total-idle) / float64(total)
}

type networkTotals struct {
	rx int64
	tx int64
}

func readNetworkTotals() (networkTotals, error) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return networkTotals{}, err
	}
	defer file.Close()
	var out networkTotals
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, values, ok := strings.Cut(scanner.Text(), ":")
		if !ok || strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseInt(fields[0], 10, 64)
		tx, _ := strconv.ParseInt(fields[8], 10, 64)
		out.rx += rx
		out.tx += tx
	}
	return out, scanner.Err()
}

func readMemoryStats() (total, available int64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		kib, _ := strconv.ParseInt(fields[0], 10, 64)
		switch key {
		case "MemTotal":
			total = kib * 1024
		case "MemAvailable":
			available = kib * 1024
		}
	}
	return total, available
}

func readMemoryTotalBytes() int64 {
	total, _ := readMemoryStats()
	return total
}

func readRootDiskTotalBytes() uint64 {
	total, _ := readRootDiskUsage()
	return total
}

func readKernelVersion() string {
	return strings.TrimSpace(readFirstLine("/proc/sys/kernel/osrelease"))
}

func readUptimeSeconds() int64 {
	fields := strings.Fields(readFirstLine("/proc/uptime"))
	if len(fields) == 0 {
		return 0
	}
	value, _ := strconv.ParseFloat(fields[0], 64)
	return int64(value)
}

func readFirstLine(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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
