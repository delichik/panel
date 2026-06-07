package linux

import (
	"context"
	"time"

	"panel/internal/sshx"
)

type OSRelease struct {
	ID         string `json:"id"`
	VersionID  string `json:"versionId"`
	PrettyName string `json:"prettyName"`
	Supported  bool   `json:"supported"`
}

type SystemStatus struct {
	Hostname      string
	KernelVersion string
	OSVersion     string
	ServerTime    time.Time
	UptimeSeconds int64
	LoadAverage   string
}

type MetricsSnapshot struct {
	ServerID           string
	Time               time.Time
	CPUUsagePercent    float64
	MemoryUsedBytes    int64
	MemoryTotalBytes   int64
	DiskUsedBytes      int64
	DiskTotalBytes     int64
	NetworkRxBytesRate float64
	NetworkTxBytesRate float64
	Status             SystemStatus
}

type PackageUpdate struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installedVersion"`
	CandidateVersion string `json:"candidateVersion"`
	Source           string `json:"source"`
}

type LogSink interface {
	AppendLog(ctx context.Context, stream, line string) error
}

type DistroAdapter interface {
	ID() string
	Supports(info OSRelease) bool
	ReadStatus(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (SystemStatus, error)
	CollectMetrics(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) (MetricsSnapshot, error)
	ListUpgradeable(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) ([]PackageUpdate, error)
	UpgradeSelected(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, packages []string, log LogSink) error
	UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, log LogSink) error
	NomadInstallScript() string
	NomadRuntimePrereqsScript() string
	NomadServiceRestartScript() string
	NomadServiceStopScript() string
}
