package agent

import (
	"context"
	"time"

	"panel/internal/linux"
	"panel/internal/remoteops"
)

const (
	TraitEnabled = "agent.enabled"
	TraitURL     = "agent.url"

	TraitStatus      = "agent.status"
	TraitVersion     = "agent.version"
	TraitLastChecked = "agent.last_checked_at"
	TraitLastError   = "agent.last_error"

	StatusCompatible   = "compatible"
	StatusIncompatible = "incompatible"
	StatusUnavailable  = "unavailable"

	Version = "1.0.0"
)

var RequiredCapabilities = []string{"health", "os-release", "system-traits", "metrics-snapshot", "ufw-status"}

type Client interface {
	Health(ctx context.Context, url string) (HealthResponse, error)
	OSRelease(ctx context.Context, url string) (linux.OSRelease, error)
	SystemTraits(ctx context.Context, url string) (map[string]string, error)
	MetricsSnapshot(ctx context.Context, url string, serverID string) (linux.MetricsSnapshot, error)
	UFWStatus(ctx context.Context, url string) (remoteops.UFWStatus, error)
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Status       string   `json:"status"`
	Time         string   `json:"time"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type OSReleaseResponse struct {
	linux.OSRelease
}

type SystemTraitsResponse struct {
	Traits map[string]string `json:"traits"`
}

type MetricsSnapshotResponse struct {
	ServerID           string    `json:"serverId"`
	Time               time.Time `json:"time"`
	CPUUsagePercent    float64   `json:"cpuUsagePercent"`
	MemoryUsedBytes    int64     `json:"memoryUsedBytes"`
	MemoryTotalBytes   int64     `json:"memoryTotalBytes"`
	DiskUsedBytes      int64     `json:"diskUsedBytes"`
	DiskTotalBytes     int64     `json:"diskTotalBytes"`
	NetworkRxBytesRate float64   `json:"networkRxBytesRate"`
	NetworkTxBytesRate float64   `json:"networkTxBytesRate"`
	Status             struct {
		Hostname      string    `json:"hostname"`
		KernelVersion string    `json:"kernelVersion"`
		OSVersion     string    `json:"osVersion"`
		ServerTime    time.Time `json:"serverTime"`
		UptimeSeconds int64     `json:"uptimeSeconds"`
		LoadAverage   string    `json:"loadAverage"`
	} `json:"status"`
}

type UFWStatusResponse struct {
	Installed bool                      `json:"installed"`
	Active    bool                      `json:"active"`
	Status    string                    `json:"status"`
	Default   string                    `json:"default"`
	Rules     []remoteops.UFWRuleStatus `json:"rules"`
	Raw       string                    `json:"raw"`
}

func SnapshotResponse(s linux.MetricsSnapshot) MetricsSnapshotResponse {
	out := MetricsSnapshotResponse{
		ServerID:           s.ServerID,
		Time:               s.Time,
		CPUUsagePercent:    s.CPUUsagePercent,
		MemoryUsedBytes:    s.MemoryUsedBytes,
		MemoryTotalBytes:   s.MemoryTotalBytes,
		DiskUsedBytes:      s.DiskUsedBytes,
		DiskTotalBytes:     s.DiskTotalBytes,
		NetworkRxBytesRate: s.NetworkRxBytesRate,
		NetworkTxBytesRate: s.NetworkTxBytesRate,
	}
	out.Status.Hostname = s.Status.Hostname
	out.Status.KernelVersion = s.Status.KernelVersion
	out.Status.OSVersion = s.Status.OSVersion
	out.Status.ServerTime = s.Status.ServerTime
	out.Status.UptimeSeconds = s.Status.UptimeSeconds
	out.Status.LoadAverage = s.Status.LoadAverage
	return out
}

func SnapshotFromResponse(r MetricsSnapshotResponse) linux.MetricsSnapshot {
	return linux.MetricsSnapshot{
		ServerID:           r.ServerID,
		Time:               r.Time,
		CPUUsagePercent:    r.CPUUsagePercent,
		MemoryUsedBytes:    r.MemoryUsedBytes,
		MemoryTotalBytes:   r.MemoryTotalBytes,
		DiskUsedBytes:      r.DiskUsedBytes,
		DiskTotalBytes:     r.DiskTotalBytes,
		NetworkRxBytesRate: r.NetworkRxBytesRate,
		NetworkTxBytesRate: r.NetworkTxBytesRate,
		Status: linux.SystemStatus{
			Hostname:      r.Status.Hostname,
			KernelVersion: r.Status.KernelVersion,
			OSVersion:     r.Status.OSVersion,
			ServerTime:    r.Status.ServerTime,
			UptimeSeconds: r.Status.UptimeSeconds,
			LoadAverage:   r.Status.LoadAverage,
		},
	}
}

func UFWStatusResponseFromStatus(s remoteops.UFWStatus) UFWStatusResponse {
	return UFWStatusResponse{Installed: s.Installed, Active: s.Active, Status: s.Status, Default: s.Default, Rules: s.Rules, Raw: s.Raw}
}

func UFWStatusFromResponse(r UFWStatusResponse) remoteops.UFWStatus {
	return remoteops.UFWStatus{Installed: r.Installed, Active: r.Active, Status: r.Status, Default: r.Default, Rules: r.Rules, Raw: r.Raw}
}
