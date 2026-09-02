package appruntime

import "time"

const (
	DesiredRunning = "running"
	DesiredStopped = "stopped"

	StatusPending   = "pending"
	StatusDeploying = "deploying"
	StatusRunning   = "running"
	StatusMissing   = "missing"
	StatusStopped   = "stopped"
	StatusFailed    = "failed"
	StatusUnknown   = "unknown"

	ManagedFileKindFile    = "file"
	ManagedFileKindArchive = "archive"

	UpdateModeReload   = "reload"
	UpdateModeRecreate = "recreate"
)

type ReloadStrategy struct {
	ValidateCommand []string `json:"validateCommand,omitempty"`
	ReloadCommand   []string `json:"reloadCommand,omitempty"`
}

type UpdatePlan struct {
	Mode     string          `json:"mode"`
	Reason   string          `json:"reason,omitempty"`
	Strategy *ReloadStrategy `json:"strategy,omitempty"`
}

type Spec struct {
	ID            string            `json:"id"`
	ApplicationID string            `json:"applicationId"`
	InstanceID    string            `json:"instanceId,omitempty"`
	ContainerName string            `json:"containerName,omitempty"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Command       []string          `json:"command,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Ports         []Port            `json:"ports,omitempty"`
	Resources     Resources         `json:"resources"`
	Privileged    bool              `json:"privileged,omitempty"`
	CapAdd        []string          `json:"capAdd,omitempty"`
	Mounts        []Mount           `json:"mounts,omitempty"`
	Files         []ManagedFile     `json:"files,omitempty"`
	Restart       Restart           `json:"restart"`
	Services      []Service         `json:"services,omitempty"`
	Checks        []Check           `json:"checks,omitempty"`
	Generation    int               `json:"generation"`
	SpecHash      string            `json:"specHash"`
}

type Port struct {
	Label         string `json:"label,omitempty"`
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

type Resources struct {
	CPU      int `json:"cpu,omitempty"`
	MemoryMB int `json:"memoryMb,omitempty"`
}

type Mount struct {
	Type     string `json:"type"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly,omitempty"`
	UID      *int   `json:"uid,omitempty"`
	GID      *int   `json:"gid,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type ManagedFile struct {
	Kind    string `json:"kind,omitempty"`
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Mode    string `json:"mode,omitempty"`
	UID     *int   `json:"uid,omitempty"`
	GID     *int   `json:"gid,omitempty"`
}

type Restart struct {
	Policy          string `json:"policy"`
	Attempts        int    `json:"attempts,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds,omitempty"`
	DelaySeconds    int    `json:"delaySeconds,omitempty"`
	Mode            string `json:"mode,omitempty"`
}

type Service struct {
	Name string   `json:"name"`
	Port string   `json:"port"`
	Tags []string `json:"tags,omitempty"`
}

type Check struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Port            string `json:"port,omitempty"`
	Path            string `json:"path,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds,omitempty"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
	Command         string `json:"command,omitempty"`
}

type PlanResponse struct {
	InstanceCount int      `json:"instanceCount"`
	TargetServers []string `json:"targetServers"`
	Warnings      []string `json:"warnings,omitempty"`
}

type Instance struct {
	ID                     string    `json:"id"`
	ApplicationID          string    `json:"applicationId"`
	ServerID               string    `json:"serverId"`
	ContainerName          string    `json:"containerName"`
	ContainerID            string    `json:"containerId,omitempty"`
	DesiredState           string    `json:"desiredState"`
	Status                 string    `json:"status"`
	RuntimeSpec            Spec      `json:"runtimeSpec"`
	LastDeployedGeneration int       `json:"lastDeployedGeneration"`
	LastError              string    `json:"lastError,omitempty"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type InstanceStatus struct {
	InstanceID    string    `json:"instanceId"`
	ServerID      string    `json:"serverId"`
	ServerName    string    `json:"serverName,omitempty"`
	ContainerName string    `json:"containerName"`
	ContainerID   string    `json:"containerId,omitempty"`
	Status        string    `json:"status"`
	DesiredState  string    `json:"desiredState"`
	Stage         string    `json:"stage,omitempty"`
	Image         string    `json:"image,omitempty"`
	StartedAt     string    `json:"startedAt,omitempty"`
	FinishedAt    string    `json:"finishedAt,omitempty"`
	ExitCode      int       `json:"exitCode,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	ObservedAt    time.Time `json:"observedAt"`
}
