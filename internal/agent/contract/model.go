package contract

import (
	"context"
	"time"

	"panel/internal/modules/applications/runtime"
	"panel/internal/platform/buildinfo"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
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
	RequiredCapabilities = []string{"health", "os-release", "system-traits", "metrics-snapshot", "packages-list", "packages-upgrade", "ufw-status", "ufw-write", "fail2ban-status", "fail2ban-write", "fail2ban-release", "system-restart", "runtime-write-files", "runtime-create-container", "runtime-status", "runtime-logs", "runtime-persistent-archive", "runtime-stop", "runtime-restart", "runtime-container-name", "docker-containers", "docker-container-logs", "docker-images", "docker-networks", "docker-volumes"}
)

type Client interface {
	Health(ctx context.Context, url string) (HealthResponse, error)
	OSRelease(ctx context.Context, url string) (linux.OSRelease, error)
	SystemTraits(ctx context.Context, url string) (map[string]string, error)
	MetricsSnapshot(ctx context.Context, url string, serverID string) (linux.MetricsSnapshot, error)
	UFWStatus(ctx context.Context, url string) (remoteops.UFWStatus, error)
}

type MaintenanceClient interface {
	PackageUpdates(ctx context.Context, url string) ([]linux.PackageUpdate, error)
	UpgradePackages(ctx context.Context, url string, req PackageUpgradeRequest) (CommandResponse, error)
	UFWInstall(ctx context.Context, url string, req UFWInstallRequest) (remoteops.UFWStatus, error)
	UFWEnable(ctx context.Context, url string, req UFWEnableRequest) (remoteops.UFWStatus, error)
	UFWAllow(ctx context.Context, url string, req UFWAllowRequest) (remoteops.UFWStatus, error)
	UFWDelete(ctx context.Context, url string, req UFWDeleteRequest) (remoteops.UFWStatus, error)
	Fail2BanStatus(ctx context.Context, url string) (Fail2BanStatusResponse, error)
	ApplyFail2Ban(ctx context.Context, url string, req Fail2BanApplyRequest) (Fail2BanStatusResponse, error)
	ReleaseFail2Ban(ctx context.Context, url string) (Fail2BanStatusResponse, error)
	RestartSystem(ctx context.Context, url string) error
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Status       string           `json:"status"`
	Time         string           `json:"time"`
	Version      string           `json:"version"`
	Capabilities []string         `json:"capabilities"`
	ContractHash string           `json:"contractHash"`
	Docker       DockerHealth     `json:"docker"`
	Certificate  *CertificateInfo `json:"-"`
}

type CertificateInfo struct {
	Fingerprint string
	CommonName  string
	NotBefore   time.Time
	NotAfter    time.Time
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
		Load1         float64   `json:"load1"`
		Load5         float64   `json:"load5"`
		Load15        float64   `json:"load15"`
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

type PackageUpdatesResponse struct {
	Items []linux.PackageUpdate `json:"items"`
}

type PackageUpgradeRequest struct {
	Names []string `json:"names,omitempty"`
	All   bool     `json:"all"`
}

type CommandResponse struct {
	Output string `json:"output"`
}

type UFWInstallRequest struct {
	Rules []remoteops.UFWRule `json:"rules"`
}

type UFWEnableRequest struct {
	SSHPort int `json:"sshPort"`
}

type UFWAllowRequest struct {
	Rule remoteops.UFWRule `json:"rule"`
}

type UFWDeleteRequest struct {
	Number int `json:"number"`
}

type Fail2BanConfig struct {
	Jails []Fail2BanJail `json:"jails" yaml:"jails"`
}

type Fail2BanJail struct {
	Name     string            `json:"name" yaml:"name"`
	Enabled  bool              `json:"enabled" yaml:"enabled"`
	Preset   string            `json:"preset,omitempty" yaml:"preset,omitempty"`
	Filter   string            `json:"filter,omitempty" yaml:"filter,omitempty"`
	LogPath  string            `json:"logpath,omitempty" yaml:"logpath,omitempty"`
	Backend  string            `json:"backend,omitempty" yaml:"backend,omitempty"`
	Port     string            `json:"port,omitempty" yaml:"port,omitempty"`
	Protocol string            `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Action   string            `json:"action,omitempty" yaml:"action,omitempty"`
	MaxRetry int               `json:"maxretry,omitempty" yaml:"maxretry,omitempty"`
	FindTime string            `json:"findtime,omitempty" yaml:"findtime,omitempty"`
	BanTime  string            `json:"bantime,omitempty" yaml:"bantime,omitempty"`
	IgnoreIP []string          `json:"ignoreip,omitempty" yaml:"ignoreip,omitempty"`
	Options  map[string]string `json:"options,omitempty" yaml:"options,omitempty"`
}

type Fail2BanStatusResponse struct {
	Installed          bool     `json:"installed"`
	Active             bool     `json:"active"`
	PanelConfigPresent bool     `json:"panelConfigPresent"`
	Jails              []string `json:"jails"`
	Raw                string   `json:"raw"`
}

type Fail2BanApplyRequest struct {
	Config Fail2BanConfig `json:"config"`
}

type RuntimeWriteFilesRequest struct {
	Spec appruntime.Spec `json:"spec"`
}

type RuntimeCreateContainerRequest struct {
	ServerID string          `json:"serverId"`
	Spec     appruntime.Spec `json:"spec"`
}

type RuntimeCreateContainerResponse struct {
	ContainerID string `json:"containerId"`
}

type RuntimeStopRequest struct {
	ApplicationID         string `json:"applicationId,omitempty"`
	InstanceID            string `json:"instanceId"`
	ContainerName         string `json:"containerName,omitempty"`
	Purge                 bool   `json:"purge"`
	RemoveApplicationData bool   `json:"removeApplicationData,omitempty"`
}

type RuntimeRestartRequest struct {
	InstanceID    string `json:"instanceId"`
	ContainerName string `json:"containerName,omitempty"`
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

type RuntimePersistentArchiveResponse struct {
	ApplicationID string `json:"applicationId"`
	Filename      string `json:"filename"`
	ContentBase64 string `json:"contentBase64"`
}

type RuntimePersistentRestoreRequest struct {
	ApplicationID string `json:"applicationId"`
	ContentBase64 string `json:"contentBase64"`
}

type RuntimePersistentRestoreResponse struct {
	ApplicationID string `json:"applicationId"`
	Restored      bool   `json:"restored"`
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

type DockerContainerLogsResponse struct {
	ContainerID string `json:"containerId"`
	Logs        string `json:"logs"`
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
	out.Status.Load1 = s.Status.Load1
	out.Status.Load5 = s.Status.Load5
	out.Status.Load15 = s.Status.Load15
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
			Load1:         r.Status.Load1,
			Load5:         r.Status.Load5,
			Load15:        r.Status.Load15,
		},
	}
}

func UFWStatusResponseFromStatus(s remoteops.UFWStatus) UFWStatusResponse {
	return UFWStatusResponse{Installed: s.Installed, Active: s.Active, Status: s.Status, Default: s.Default, Rules: s.Rules, Raw: s.Raw}
}

func UFWStatusFromResponse(r UFWStatusResponse) remoteops.UFWStatus {
	return remoteops.UFWStatus{Installed: r.Installed, Active: r.Active, Status: r.Status, Default: r.Default, Rules: r.Rules, Raw: r.Raw}
}
