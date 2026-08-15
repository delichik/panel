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

	TraitStatus                = "agent.status"
	TraitVersion               = "agent.version"
	TraitLastChecked           = "agent.last_checked_at"
	TraitLastError             = "agent.last_error"
	TraitAutoDeployBlocked     = "agent.auto_deploy_blocked"
	TraitAutoDeployFailures    = "agent.auto_deploy_failures"
	TraitAutoDeployLastFailure = "agent.auto_deploy_last_failure_at"
	TraitHealthSuccessStreak   = "agent.health_success_streak"

	TraitCertificateFingerprint = "agent.certificate.fingerprint"
	TraitCertificateNotBefore   = "agent.certificate.not_before"
	TraitCertificateNotAfter    = "agent.certificate.not_after"

	TraitReportStatus        = "agent.report.status"
	TraitReportLastMessageAt = "agent.report.last_message_at"
	TraitReportLastError     = "agent.report.last_error"

	StatusCompatible   = "compatible"
	StatusIncompatible = "incompatible"
	StatusUnavailable  = "unavailable"
	StatusUndeployable = "undeployable"

	ReportStatusConnected    = "connected"
	ReportStatusDisconnected = "disconnected"

	DefaultDockerHost = "unix:///var/run/docker.sock"

	CapabilityPrepareRestart = "prepare-restart"

	PrepareRestartStateHoldOn = "holdon"
	PrepareRestartStateReady  = "ready"
)

var (
	Version              = buildinfo.NormalizedVersion()
	RequiredCapabilities = []string{"health", "os-release", "system-traits", "metrics-snapshot", "packages-list", "packages-upgrade", "ufw-status", "ufw-write", "fail2ban-status", "fail2ban-write", "fail2ban-release", "system-restart", "runtime-write-files", "runtime-reload", "runtime-create-container", "runtime-status", "runtime-logs", "runtime-persistent-archive", "runtime-stop", "runtime-restart", "runtime-container-name", "docker-containers", "docker-container-logs", "docker-images", "docker-networks", "docker-volumes"}
)

type Client interface {
	Health(ctx context.Context, url string) (HealthResponse, error)
	OSRelease(ctx context.Context, url string) (linux.OSRelease, error)
	SystemTraits(ctx context.Context, url string) (map[string]string, error)
	MetricsSnapshot(ctx context.Context, url string, serverID string) (linux.MetricsSnapshot, error)
	UFWStatus(ctx context.Context, url string) (remoteops.UFWStatus, error)
}

// RestartReadinessClient coordinates with a running agent before Panel stops or
// restarts it. PrepareRestart returns nil once the agent confirms it is safe to
// restart (state "ready"); it blocks while the agent reports "holdon" and
// returns when the stream ends or the context is cancelled.
type RestartReadinessClient interface {
	PrepareRestart(ctx context.Context, url string) error
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

type MetricsSnapshotRequest struct {
	ServerID string `json:"serverId"`
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

type RuntimeReloadRequest struct {
	Spec            appruntime.Spec `json:"spec"`
	ContainerName   string          `json:"containerName"`
	ValidateCommand []string        `json:"validateCommand"`
	ReloadCommand   []string        `json:"reloadCommand"`
}

type RuntimeReloadResponse struct {
	Reloaded bool   `json:"reloaded"`
	Phase    string `json:"phase"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
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

type RuntimeStatusRequest struct {
	InstanceID    string `json:"instanceId"`
	ContainerName string `json:"containerName,omitempty"`
}

type RuntimeLogsRequest struct {
	InstanceID    string `json:"instanceId"`
	ContainerName string `json:"containerName,omitempty"`
	Tail          int    `json:"tail,omitempty"`
}

type RuntimePersistentArchiveRequest struct {
	ApplicationID string `json:"applicationId"`
}

type DockerContainerLogsRequest struct {
	ID   string `json:"id"`
	Tail int    `json:"tail,omitempty"`
}

type DockerContainerActionRequest struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type DockerContainerDeleteRequest struct {
	ID string `json:"id"`
}

type DockerImageDeleteRequest struct {
	ID string `json:"id"`
}

type DockerVolumeDeleteRequest struct {
	Name string `json:"name"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

// CapabilityStorageShare 表示 Agent 支持存储共享设施（NFS 导出/挂载状态等）。
const CapabilityStorageShare = "agent.storage.share"

// StorageExportStatus 是一台存储服务器上 NFS 导出的生效状态。
type StorageExportStatus struct {
	ServerInstalled bool
	RootExists      bool
	ExportLive      bool
	Detail          string
}

// StorageMountStatus 是应用节点上一个 NFS 卷挂载的生效状态。
type StorageMountStatus struct {
	VolumeExists bool
	Mountpoint   string
	Mounted      bool
	Writable     bool
	Detail       string
}
