package docker

import (
	"context"
	"time"

	"panel/internal/sshx"
)

type DockerCapability struct {
	ServerID         string     `json:"serverId"`
	DockerInstalled  bool       `json:"dockerInstalled"`
	DockerVersion    string     `json:"dockerVersion"`
	ComposeInstalled bool       `json:"composeInstalled"`
	ComposeVersion   string     `json:"composeVersion"`
	Supported        bool       `json:"supported"`
	LastCheckedAt    *time.Time `json:"lastCheckedAt"`
	LastError        string     `json:"lastError,omitempty"`
	Stale            bool       `json:"stale"`
	Pending          bool       `json:"pending"`
	TaskID           string     `json:"taskId,omitempty"`
}

type ComposeProject struct {
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	ConfigFiles string            `json:"configFiles,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type ComposeStatus struct {
	Project   string           `json:"project"`
	State     string           `json:"state"`
	Services  []RuntimeService `json:"services"`
	CheckedAt time.Time        `json:"checkedAt"`
}

type RuntimeService struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Command   string            `json:"command,omitempty"`
	State     string            `json:"state"`
	Status    string            `json:"status"`
	Ports     string            `json:"ports,omitempty"`
	Project   string            `json:"project,omitempty"`
	Service   string            `json:"service,omitempty"`
	CreatedAt string            `json:"createdAt,omitempty"`
	Labels    map[string]string `json:"labels"`
	Managed   bool              `json:"managed"`
}

type RuntimeNetwork struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Driver   string            `json:"driver"`
	Scope    string            `json:"scope"`
	Internal bool              `json:"internal"`
	Labels   map[string]string `json:"labels"`
	Managed  bool              `json:"managed"`
}

type RuntimeVolume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	Labels     map[string]string `json:"labels"`
	Managed    bool              `json:"managed"`
}

type RuntimeImage struct {
	ID         string            `json:"id"`
	Repository string            `json:"repository"`
	Tag        string            `json:"tag"`
	Digest     string            `json:"digest,omitempty"`
	Size       string            `json:"size"`
	CreatedAt  string            `json:"createdAt,omitempty"`
	Labels     map[string]string `json:"labels"`
	Managed    bool              `json:"managed"`
	Update     *ImageUpdate      `json:"update,omitempty"`
}

type ImageUpdate struct {
	ImageID         string     `json:"imageId"`
	Repository      string     `json:"repository"`
	Tag             string     `json:"tag"`
	CurrentDigest   string     `json:"currentDigest,omitempty"`
	LatestDigest    string     `json:"latestDigest,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
}

type RuntimeList[T any] struct {
	ServerID        string     `json:"serverId"`
	LastRefreshedAt *time.Time `json:"lastRefreshedAt,omitempty"`
	Items           []T        `json:"items"`
}

type ContainerRuntime interface {
	Detect(ctx context.Context, target sshx.Target) (DockerCapability, error)
	ListComposeProjects(ctx context.Context, target sshx.Target) ([]ComposeProject, error)
	ListServices(ctx context.Context, target sshx.Target) ([]RuntimeService, error)
	ListNetworks(ctx context.Context, target sshx.Target) ([]RuntimeNetwork, error)
	ListVolumes(ctx context.Context, target sshx.Target) ([]RuntimeVolume, error)
	ListImages(ctx context.Context, target sshx.Target) ([]RuntimeImage, error)
	ReadComposeStatus(ctx context.Context, target sshx.Target, project string) (ComposeStatus, error)
	StartContainer(ctx context.Context, target sshx.Target, containerID string) error
	StopContainer(ctx context.Context, target sshx.Target, containerID string) error
	DeleteContainer(ctx context.Context, target sshx.Target, containerID string) error
	DeleteNetwork(ctx context.Context, target sshx.Target, networkID string) error
	DeleteVolume(ctx context.Context, target sshx.Target, volumeID string) error
	DeleteImage(ctx context.Context, target sshx.Target, imageID string) error
	PruneNetworks(ctx context.Context, target sshx.Target) error
	PruneVolumes(ctx context.Context, target sshx.Target) error
	PruneImages(ctx context.Context, target sshx.Target) error
	CheckImageUpdate(ctx context.Context, target sshx.Target, image RuntimeImage) (ImageUpdate, error)
	PullImage(ctx context.Context, target sshx.Target, repository, tag string) error
}
