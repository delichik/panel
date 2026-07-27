package applications

import (
	"time"

	"panel/internal/modules/applications/runtime"
)

const (
	TaskTypeTargetBatch = "application_target_batch"
	TaskTypeTargetApply = "application_target_apply"
	TaskTypeTargetStop  = "application_target_stop"
	TaskTypeTargetPurge = "application_target_purge"
	TaskTypeStop        = "application_stop"
	TaskTypeRestart     = "application_restart"
	TaskTypeRefresh     = "application_refresh"
	TaskTypeImageCheck  = "application_image_check"
	TaskTypeImageUpdate = "application_image_update"

	ApplicationKindUser     = "application"
	ApplicationKindFacility = "facility_application"

	DeploymentModeAll      = "all"
	DeploymentModeSelected = "selected"

	ReverseProxyTargetLocal     = "local"
	ReverseProxyTargetContainer = "container"
	HTTPRouteModeInherit        = "inherit"
	HTTPRouteModeOn             = "on"
	HTTPRouteModeOff            = "off"
	HTTPRouteWebSocketAuto      = "auto"

	ApplicationFileKindBinary   = "binary"
	ApplicationFileKindTemplate = "template"
	ApplicationFileKindArchive  = "archive"

	LifecycleTypeDeploy      = "deploy"
	LifecycleTypeRefresh     = "refresh"
	LifecycleTypeImageUpdate = "image_update"

	LifecycleStatusPending           = "pending"
	LifecycleStatusDeploying         = "deploying"
	LifecycleStatusDeployed          = "deployed"
	LifecycleStatusPartiallyDeployed = "partially_deployed"
	LifecycleStatusFailed            = "failed"
	LifecycleStatusSuperseded        = "superseded"

	LifecycleTargetStatusPending    = "pending"
	LifecycleTargetStatusPreparing  = "preparing"
	LifecycleTargetStatusDeploying  = "deploying"
	LifecycleTargetStatusRunning    = "running"
	LifecycleTargetStatusFailed     = "failed"
	LifecycleTargetStatusSuperseded = "superseded"

	LifecycleTargetActionApply = "apply"
	LifecycleTargetActionStop  = "stop"
	LifecycleTargetActionPurge = "purge"

	LifecycleTargetStatePlanned         = "planned"
	LifecycleTargetStateReady           = "ready"
	LifecycleTargetStateClaimed         = "claimed"
	LifecycleTargetStatePreparing       = "preparing"
	LifecycleTargetStateApplying        = "applying"
	LifecycleTargetStateStopping        = "stopping"
	LifecycleTargetStatePurging         = "purging"
	LifecycleTargetStateVerifying       = "verifying"
	LifecycleTargetStateSucceeded       = "succeeded"
	LifecycleTargetStateFailedRetryable = "failed_retryable"
	LifecycleTargetStateFailed          = "failed"
	LifecycleTargetStateSuperseded      = "superseded"
	LifecycleTargetStateCancelled       = "cancelled"
)

type Application struct {
	ID                   string              `json:"id"`
	Version              int                 `json:"version"`
	Kind                 string              `json:"kind"`
	Name                 string              `json:"name"`
	Enabled              bool                `json:"enabled"`
	DeletionRequested    bool                `json:"deletionRequested,omitempty"`
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

type ApplicationSummary struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Enabled              bool      `json:"enabled"`
	ImageReference       string    `json:"imageReference,omitempty"`
	JobID                string    `json:"jobId"`
	Namespace            string    `json:"namespace"`
	RuntimeStatus        string    `json:"runtimeStatus,omitempty"`
	ImageUpdateAvailable bool      `json:"imageUpdateAvailable"`
	LastError            string    `json:"lastError,omitempty"`
	UpdatedAt            time.Time `json:"updatedAt"`
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
	Domain          string             `json:"domain"`
	TargetType      string             `json:"targetType,omitempty"`
	TargetPort      int                `json:"targetPort"`
	OriginServerIDs []string           `json:"originServerIds"`
	AnyAccess       AnyAccessConfig    `json:"anyAccess"`
	Paths           []ReverseProxyPath `json:"paths"`
}

type AnyAccessConfig struct {
	Enabled               bool   `json:"enabled"`
	Strategy              string `json:"strategy"`
	PrimaryOriginServerID string `json:"primaryOriginServerId,omitempty"`
}

type ReverseProxyPath struct {
	Path      string           `json:"path"`
	WebSocket bool             `json:"webSocket"`
	Options   HTTPRouteOptions `json:"options,omitempty"`
}

type HTTPHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HTTPRouteOptions struct {
	GzipMode              string       `json:"gzipMode,omitempty"`
	ClientMaxBodySizeMB   int          `json:"clientMaxBodySizeMb,omitempty"`
	ConnectTimeoutSeconds int          `json:"connectTimeoutSeconds,omitempty"`
	ReadTimeoutSeconds    int          `json:"readTimeoutSeconds,omitempty"`
	SendTimeoutSeconds    int          `json:"sendTimeoutSeconds,omitempty"`
	BufferingMode         string       `json:"bufferingMode,omitempty"`
	WebSocketMode         string       `json:"webSocketMode,omitempty"`
	RequestHeaders        []HTTPHeader `json:"requestHeaders,omitempty"`
	ResponseHeaders       []HTTPHeader `json:"responseHeaders,omitempty"`
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
	Domain          string             `json:"domain"`
	TargetType      string             `json:"targetType,omitempty"`
	TargetPort      int                `json:"targetPort"`
	TargetContainer string             `json:"targetContainer,omitempty"`
	OriginServerIDs []string           `json:"originServerIds"`
	AnyAccess       AnyAccessConfig    `json:"anyAccess"`
	Paths           []ReverseProxyPath `json:"paths"`
}

type ApplicationFile struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	ContentType   string    `json:"contentType"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	Content       []byte    `json:"-"`
	ContentBase64 string    `json:"contentBase64,omitempty"`
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

const (
	EditSessionStateActive     = "active"
	EditSessionStateValidating = "validating"
	EditSessionStatePreviewing = "previewing"
	EditSessionStateCommitting = "committing"
	EditSessionStateCommitted  = "committed"
	EditSessionStateConflict   = "conflict"
	EditSessionStateDiscarded  = "discarded"
	EditSessionStateExpired    = "expired"
)

type ResourceVersion struct {
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Diagnostic struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Field    string         `json:"field,omitempty"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

type PreviewToken struct {
	Value          string `json:"value"`
	Action         string `json:"action"`
	SubjectVersion string `json:"subjectVersion"`
}

type EditSessionFile struct {
	FileKey     string    `json:"fileKey"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type ApplicationEditSession struct {
	ID                  string            `json:"id"`
	ApplicationID       string            `json:"applicationId,omitempty"`
	ClientDraftKey      string            `json:"clientDraftKey,omitempty"`
	State               string            `json:"state"`
	BaseResourceVersion ResourceVersion   `json:"baseResourceVersion"`
	Draft               SaveInput         `json:"draft"`
	Revision            int               `json:"revision"`
	Files               []EditSessionFile `json:"files"`
	PreviewToken        *PreviewToken     `json:"previewToken,omitempty"`
	IdleExpiresAt       time.Time         `json:"idleExpiresAt"`
	AbsoluteExpiresAt   time.Time         `json:"absoluteExpiresAt"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
	CommittedAt         *time.Time        `json:"committedAt,omitempty"`
	CommitResult        *EditCommitResult `json:"commitResult,omitempty"`
}

type BeginEditSessionInput struct {
	ApplicationID  string     `json:"applicationId,omitempty"`
	ClientDraftKey string     `json:"clientDraftKey,omitempty"`
	Draft          *SaveInput `json:"draft,omitempty"`
}

type PatchEditSessionInput struct {
	Revision int       `json:"revision"`
	Draft    SaveInput `json:"draft"`
}

type EditSessionFileInput struct {
	Revision          int    `json:"revision"`
	ClientOperationID string `json:"clientOperationId"`
	Path              string `json:"path"`
	Kind              string `json:"kind"`
	ContentType       string `json:"contentType"`
	ContentBase64     string `json:"contentBase64"`
}

type EditSessionArchiveInput struct {
	Revision          int
	ClientOperationID string
	FileKey           string
	BasePath          string
	Kind              string
	FileName          string
	Content           []byte
}

type EditSessionMutationInput struct {
	Revision          int    `json:"revision"`
	ClientOperationID string `json:"clientOperationId"`
}

type EditSessionValidationResult struct {
	Valid       bool         `json:"valid"`
	Revision    int          `json:"revision"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type EditSessionPreviewResult struct {
	Revision    int          `json:"revision"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Token       PreviewToken `json:"token"`
	ExpiresAt   time.Time    `json:"expiresAt"`
}

type CommitEditSessionInput struct {
	Revision                 int      `json:"revision"`
	BaseResourceVersion      string   `json:"baseResourceVersion"`
	PreviewToken             string   `json:"previewToken"`
	ConfirmedDiagnosticCodes []string `json:"confirmedDiagnosticCodes,omitempty"`
}

type EditCommitResult struct {
	Application     Application     `json:"application"`
	ResourceVersion ResourceVersion `json:"resourceVersion"`
	ApplyRequested  bool            `json:"applyRequested"`
	Diagnostics     []Diagnostic    `json:"diagnostics,omitempty"`
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
	ID                string     `json:"id"`
	OperationID       string     `json:"operationId"`
	ApplicationID     string     `json:"applicationId"`
	ServerID          string     `json:"serverId"`
	ServerName        string     `json:"serverName,omitempty"`
	Action            string     `json:"action,omitempty"`
	State             string     `json:"state,omitempty"`
	Status            string     `json:"status"`
	TargetKey         string     `json:"targetKey,omitempty"`
	DesiredState      string     `json:"desiredState"`
	DesiredGeneration int        `json:"desiredGeneration,omitempty"`
	DesiredSpecHash   string     `json:"desiredSpecHash,omitempty"`
	Priority          int        `json:"priority,omitempty"`
	Attempt           int        `json:"attempt,omitempty"`
	NextRunAt         *time.Time `json:"nextRunAt,omitempty"`
	LeaseOwner        string     `json:"leaseOwner,omitempty"`
	LeaseExpiresAt    *time.Time `json:"leaseExpiresAt,omitempty"`
	ClaimedTaskID     string     `json:"claimedTaskId,omitempty"`
	InstanceID        string     `json:"instanceId,omitempty"`
	ContainerName     string     `json:"containerName,omitempty"`
	ContainerID       string     `json:"containerId,omitempty"`
	Stage             string     `json:"stage,omitempty"`
	Error             string     `json:"error,omitempty"`
	ErrorCode         string     `json:"errorCode,omitempty"`
	ErrorMessage      string     `json:"errorMessage,omitempty"`
	ErrorDetail       string     `json:"errorDetail,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type SaveInput struct {
	Name              string             `json:"name"`
	Enabled           bool               `json:"enabled"`
	SpecYAML          string             `json:"specYaml"`
	Variables         map[string]string  `json:"variables"`
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

type DeploymentPlanRequest struct {
	ApplicationID        string
	ServerIDs            []string
	StopServers          []string
	Purge                bool
	Force                bool
	ObservedRuntimeDrift bool
	Manual               bool
	TriggerType          string
	TriggerResourceType  string
	TriggerResourceID    string
	Reason               string
}

type DeploymentPlanResult struct {
	OperationIDs        []string
	CreatedTargetIDs    []string
	ReusedTargetIDs     []string
	SupersededTargetIDs []string
	BlockedTargetIDs    []string
	CreatedTargets      []LifecycleTarget
	ReusedTargets       []LifecycleTarget
	SupersededTargets   []LifecycleTarget
	BlockedTargets      []LifecycleTarget
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
