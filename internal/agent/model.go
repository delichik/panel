package agent

import (
	"context"
	"time"

	"panel/internal/appruntime"
	"panel/internal/buildinfo"
	"panel/internal/linux"
	"panel/internal/remoteops"
)

const (
	TraitEnabled = "agent.enabled"
	TraitURL     = "agent.url"

	TraitStatus            = "agent.status"
	TraitVersion           = "agent.version"
	TraitLastChecked       = "agent.last_checked_at"
	TraitLastError         = "agent.last_error"
	TraitAutoDeployBlocked = "agent.auto_deploy_blocked"

	TraitCertificateFingerprint = "agent.certificate.fingerprint"
	TraitCertificateNotBefore   = "agent.certificate.not_before"
	TraitCertificateNotAfter    = "agent.certificate.not_after"

	StatusCompatible   = "compatible"
	StatusIncompatible = "incompatible"
	StatusUnavailable  = "unavailable"
	StatusUndeployable = "undeployable"

	DefaultDockerHost = "unix:///var/run/docker.sock"
)

var (
	Version              = buildinfo.NormalizedVersion()
	RequiredCapabilities = []string{"health", "os-release", "system-traits", "metrics-snapshot", "ufw-status", "runtime-deploy", "runtime-status", "runtime-logs", "runtime-stop", "runtime-restart", "docker-containers", "docker-images", "docker-networks", "docker-volumes"}
)

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
	Status       string           `json:"status"`
	Time         string           `json:"time"`
	Version      string           `json:"version"`
	Capabilities []string         `json:"capabilities"`
	Docker       DockerHealth     `json:"docker"`
	Certificate  *CertificateInfo `json:"-"`
}

type DockerHealth struct {
	Host   string `json:"host"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
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

type RuntimeDeployRequest struct {
	ServerID string          `json:"serverId"`
	Spec     appruntime.Spec `json:"spec"`
}

type RuntimeStopRequest struct {
	InstanceID string `json:"instanceId"`
	Purge      bool   `json:"purge"`
}

type RuntimeRestartRequest struct {
	InstanceID string `json:"instanceId"`
}

type RuntimeInstanceResponse struct {
	InstanceID    string    `json:"instanceId"`
	ContainerName string    `json:"containerName"`
	ContainerID   string    `json:"containerId,omitempty"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	ObservedAt    time.Time `json:"observedAt"`
}

type RuntimeStatusResponse struct {
	appruntime.InstanceStatus
}

type RuntimeLogsResponse struct {
	InstanceID string `json:"instanceId"`
	Logs       string `json:"logs"`
}

type DockerContainer struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	ImageID string            `json:"imageId"`
	Command string            `json:"command"`
	Created int64             `json:"created"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Ports   []DockerPort      `json:"ports"`
	Labels  map[string]string `json:"labels"`
	Mounts  []DockerMount     `json:"mounts"`
}

type DockerPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

type DockerMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Driver      string `json:"driver,omitempty"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw"`
}

type DockerImage struct {
	ID          string   `json:"id"`
	ParentID    string   `json:"parentId,omitempty"`
	RepoTags    []string `json:"repoTags"`
	RepoDigests []string `json:"repoDigests"`
	Created     int64    `json:"created"`
	Size        int64    `json:"size"`
	Containers  int      `json:"containers"`
}

type DockerNetwork struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Driver   string            `json:"driver"`
	Scope    string            `json:"scope"`
	Created  string            `json:"created,omitempty"`
	Internal bool              `json:"internal"`
	Labels   map[string]string `json:"labels"`
}

type DockerVolume struct {
	Name           string             `json:"name"`
	Driver         string             `json:"driver"`
	Mountpoint     string             `json:"mountpoint"`
	CreatedAt      string             `json:"createdAt,omitempty"`
	Labels         map[string]string  `json:"labels"`
	UsageData      *DockerVolumeUsage `json:"usageData,omitempty"`
	InUse          bool               `json:"inUse"`
	ContainerCount int                `json:"containerCount"`
}

type DockerVolumeUsage struct {
	Size     int64 `json:"size"`
	RefCount int64 `json:"refCount"`
}

type DockerContainersResponse struct {
	Items []DockerContainer `json:"items"`
}

type DockerImagesResponse struct {
	Items []DockerImage `json:"items"`
}

type DockerNetworksResponse struct {
	Items []DockerNetwork `json:"items"`
}

type DockerVolumesResponse struct {
	Items []DockerVolume `json:"items"`
}

type DockerImagePullRequest struct {
	Reference string `json:"reference"`
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
