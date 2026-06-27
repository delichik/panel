package applications

import (
	"time"

	"panel/internal/modules/applications/runtime"
)

const (
	TaskTypeDeploy      = "application_deploy"
	TaskTypeStop        = "application_stop"
	TaskTypeRestart     = "application_restart"
	TaskTypeRefresh     = "application_refresh"
	TaskTypeImageCheck  = "application_image_check"
	TaskTypeImageUpdate = "application_image_update"

	DeploymentModeAll      = "all"
	DeploymentModeSelected = "selected"

	LifecycleTypeDeploy      = "deploy"
	LifecycleTypeRefresh     = "refresh"
	LifecycleTypeImageUpdate = "image_update"

	LifecycleStatusPending           = "pending"
	LifecycleStatusDeploying         = "deploying"
	LifecycleStatusDeployed          = "deployed"
	LifecycleStatusPartiallyDeployed = "partially_deployed"
	LifecycleStatusFailed            = "failed"

	LifecycleTargetStatusPending   = "pending"
	LifecycleTargetStatusPreparing = "preparing"
	LifecycleTargetStatusDeploying = "deploying"
	LifecycleTargetStatusRunning   = "running"
	LifecycleTargetStatusFailed    = "failed"
)

type Application struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name"`
	Enabled              bool                `json:"enabled"`
	SpecYAML             string              `json:"specYaml"`
	Variables            map[string]string   `json:"variables"`
	ResolvedVariables    map[string]any      `json:"resolvedVariables,omitempty"`
	PersistentPath       string              `json:"persistentPath,omitempty"`
	DeploymentMode       string              `json:"deploymentMode"`
	DeploymentServers    []string            `json:"deploymentServers"`
	ReverseProxy         []ReverseProxyRule  `json:"reverseProxy"`
	Generation           int                 `json:"generation"`
	SpecHash             string              `json:"specHash"`
	ImageReference       string              `json:"imageReference,omitempty"`
	ImageDigest          string              `json:"imageDigest,omitempty"`
	ImageLatestDigest    string              `json:"imageLatestDigest,omitempty"`
	ImageCheckedAt       *time.Time          `json:"imageCheckedAt,omitempty"`
	ImageUpdateAvailable bool                `json:"imageUpdateAvailable"`
	ImageUpdateTargets   []ImageUpdateTarget `json:"imageUpdateTargets,omitempty"`
	ImageLastError       string              `json:"imageLastError,omitempty"`
	JobID                string              `json:"jobId"`
	Namespace            string              `json:"namespace"`
	LastEvalID           string              `json:"lastEvalId,omitempty"`
	LastDeploymentID     string              `json:"lastDeploymentId,omitempty"`
	LastError            string              `json:"lastError,omitempty"`
	RuntimeStatus        string              `json:"runtimeStatus,omitempty"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
}

type ImageUpdateTarget struct {
	ServerID        string     `json:"serverId"`
	ServerName      string     `json:"serverName,omitempty"`
	Reference       string     `json:"reference"`
	LocalDigest     string     `json:"localDigest,omitempty"`
	LatestDigest    string     `json:"latestDigest,omitempty"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
}

type ReverseProxyRule struct {
	Domain     string             `json:"domain"`
	TargetPort int                `json:"targetPort"`
	Paths      []ReverseProxyPath `json:"paths"`
}

type ReverseProxyPath struct {
	Path      string `json:"path"`
	WebSocket bool   `json:"webSocket"`
}

type ApplicationReverseProxyConfig struct {
	ApplicationID     string              `json:"applicationId"`
	ApplicationName   string              `json:"applicationName"`
	JobID             string              `json:"jobId"`
	DeploymentMode    string              `json:"deploymentMode"`
	DeploymentServers []string            `json:"deploymentServers"`
	Routes            []ReverseProxyRoute `json:"routes"`
}

type ReverseProxyRoute struct {
	Domain     string             `json:"domain"`
	TargetPort int                `json:"targetPort"`
	Paths      []ReverseProxyPath `json:"paths"`
}

type ApplicationFile struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	ContentType   string    `json:"contentType"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	Content       []byte    `json:"content,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type FileSaveInput struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	ContentType   string `json:"contentType"`
	ContentBase64 string `json:"contentBase64"`
}

type FileDeleteInput struct {
	Path string `json:"path"`
}

type FileArchiveInput struct {
	BasePath string
	Kind     string
	FileName string
	Content  []byte
}

type BeginSaveSessionInput struct {
	ApplicationID string    `json:"applicationId,omitempty"`
	Save          SaveInput `json:"save"`
}

type SaveSessionResult struct {
	ID            string            `json:"id"`
	ApplicationID string            `json:"applicationId,omitempty"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	Files         []ApplicationFile `json:"files"`
}

type ApplicationRevision struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Generation    int       `json:"generation"`
	SpecHash      string    `json:"specHash"`
	SpecYAML      string    `json:"specYaml"`
	JobJSON       string    `json:"jobJson"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Runtime struct {
	ApplicationID string                      `json:"applicationId"`
	RuntimeID     string                      `json:"runtimeId"`
	Status        string                      `json:"status"`
	Operation     *LifecycleOperation         `json:"operation,omitempty"`
	Instances     []appruntime.InstanceStatus `json:"instances"`
	ObservedAt    time.Time                   `json:"observedAt"`
}

type LifecycleOperation struct {
	ID            string            `json:"id"`
	ApplicationID string            `json:"applicationId"`
	Type          string            `json:"type"`
	Status        string            `json:"status"`
	TaskID        string            `json:"taskId,omitempty"`
	Generation    int               `json:"generation"`
	SpecHash      string            `json:"specHash,omitempty"`
	Trigger       string            `json:"trigger,omitempty"`
	Error         string            `json:"error,omitempty"`
	Targets       []LifecycleTarget `json:"targets,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	StartedAt     *time.Time        `json:"startedAt,omitempty"`
	FinishedAt    *time.Time        `json:"finishedAt,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type LifecycleTarget struct {
	ID            string     `json:"id"`
	OperationID   string     `json:"operationId"`
	ApplicationID string     `json:"applicationId"`
	ServerID      string     `json:"serverId"`
	ServerName    string     `json:"serverName,omitempty"`
	Status        string     `json:"status"`
	DesiredState  string     `json:"desiredState"`
	InstanceID    string     `json:"instanceId,omitempty"`
	ContainerName string     `json:"containerName,omitempty"`
	ContainerID   string     `json:"containerId,omitempty"`
	Stage         string     `json:"stage,omitempty"`
	Error         string     `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type SaveInput struct {
	Name              string             `json:"name"`
	Enabled           bool               `json:"enabled"`
	SpecYAML          string             `json:"specYaml"`
	Variables         map[string]string  `json:"variables"`
	PersistentPath    string             `json:"persistentPath"`
	DeploymentMode    string             `json:"deploymentMode"`
	DeploymentServers []string           `json:"deploymentServers"`
	ReverseProxy      []ReverseProxyRule `json:"reverseProxy"`
}

type OperationResult struct {
	TaskID             string      `json:"taskId,omitempty"`
	EvalID             string      `json:"evalId,omitempty"`
	DeploymentID       string      `json:"deploymentId,omitempty"`
	Application        Application `json:"application"`
	ApplicationRuntime *Runtime    `json:"runtime,omitempty"`
}

type MigrationInput struct {
	SourceServerID string `json:"sourceServerId"`
	TargetServerID string `json:"targetServerId"`
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}

type TemplateVariableDefinition struct {
	Key                string `json:"key"`
	Category           string `json:"category"`
	SpecExpression     string `json:"specExpression"`
	TemplateExpression string `json:"templateExpression"`
}

type PanelFileDefinition struct {
	ID           string `json:"id"`
	ResourceID   string `json:"resourceId"`
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Source       string `json:"source"`
}

type TemplateCatalog struct {
	Variables  []TemplateVariableDefinition `json:"variables"`
	PanelFiles []PanelFileDefinition        `json:"panelFiles"`
}
