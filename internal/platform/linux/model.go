package linux

import (
	"context"
	"time"

	"panel/internal/platform/ssh"
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
	Load1         float64
	Load5         float64
	Load15        float64
}

type MetricsSnapshot struct {
	ServerID           string
	Time               time.Time
	CPUUsagePercent    float64
	MemoryUsedBytes    int64
	MemoryTotalBytes   int64
	DiskUsedBytes      int64
	DiskTotalBytes     int64
	DiskUsageSampled   bool
	DiskIOAvailable    bool
	DiskReadBytesRate  float64
	DiskWriteBytesRate float64
	NetworkRxBytesRate float64
	NetworkTxBytesRate float64
	Status             SystemStatus
}

type PackageUpdate struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installedVersion"`
	CandidateVersion string `json:"candidateVersion"`
	Source           string `json:"source"`
	// Security 是 Agent 基于系统 apt 元数据与 unattended-upgrades 策略得到的
	// 权威安全更新判定（apt-cache policy 的 Candidate 来源 + Allowed-Origins），
	// 经由 Agent proto 传输并持久化在 package_updates.security。
	// SecurityKnown 为 false 时 Security 没有意义（旧 Agent 不填这两个字段、
	// apt-cache policy 失败/超时、Candidate 来源不可解析），Panel 必须保持
	// unknown 并跳过，绝不能用 Source 字符串等启发式猜测。
	Security bool `json:"security"`
	// SecurityKnown 标记 Security 是否来自 Agent 的权威判定。
	SecurityKnown bool `json:"securityKnown"`
}

type LogSink interface {
	AppendLog(ctx context.Context, stream, line string) error
}

type DistroAdapter interface {
	ID() string
	Supports(info OSRelease) bool
	ListUpgradeable(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) ([]PackageUpdate, error)
	UpgradeSelected(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, packages []string, log LogSink) error
	UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, log LogSink) error
	SupportsUFW() bool
	UFWInstallScript() string
}
