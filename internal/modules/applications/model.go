package applications

import (
	"time"

	"panel/internal/modules/applications/runtime"
)

const (
	TaskTypeStop        = "application_stop"
	TaskTypeRestart     = "application_restart"
	TaskTypeRefresh     = "application_refresh"
	TaskTypeImageCheck  = "application_image_check"
	TaskTypeImageUpdate = "application_image_update"

	ApplicationKindUser     = "application"
	ApplicationKindFacility = "facility_application"

	DeploymentModeAll      = "all"
	DeploymentModeSelected = "selected"

	HTTPRouteModeInherit   = "inherit"
	HTTPRouteModeOn        = "on"
	HTTPRouteModeOff       = "off"
	HTTPRouteWebSocketAuto = "auto"

	ReconcileStopAfterFailures = 10

	// FacilityProxyApplicationID 是入口代理设施应用 ID，镜像
	// facilityapps.proxyApplicationID。applications 包不能反向依赖
	// facilityapps，这里仅用于判断"刚验证成功的目标是否属于入口代理自身"，
	// 避免代理自己同步完成后再次触发自己。
	FacilityProxyApplicationID = "facility-reverse-proxy"

	ApplicationFileKindBinary   = "binary"
	ApplicationFileKindTemplate = "template"
	ApplicationFileKindArchive  = "archive"
)

type Application struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// AutoUpdateEnabled 是对象级“允许自动更新”开关（默认开）。它只决定
	// 定时自动更新任务是否纳入本应用，不影响手动检查/立即更新/批量升级。
	AutoUpdateEnabled    bool                     `json:"autoUpdateEnabled"`
	ReconcileStopped     bool                     `json:"reconcileStopped,omitempty"`
	DeletionRequested    bool                     `json:"deletionRequested,omitempty"`
	SpecYAML             string                   `json:"specYaml"`
	PersistentPath       string                   `json:"persistentPath,omitempty"`
	PersistentServers    []string                 `json:"persistentServers"`
	HasPersistentData    bool                     `json:"hasPersistentData"`
	DeploymentMode       string                   `json:"deploymentMode"`
	DeploymentServers    []string                 `json:"deploymentServers"`
	ReverseProxy         []ReverseProxyRule       `json:"reverseProxy"`
	Generation           int                      `json:"generation"`
	SpecHash             string                   `json:"specHash"`
	ImageReference       string                   `json:"imageReference,omitempty"`
	ImageDigest          string                   `json:"imageDigest,omitempty"`
	ImageLatestDigest    string                   `json:"imageLatestDigest,omitempty"`
	ImageCheckedAt       *time.Time               `json:"imageCheckedAt,omitempty"`
	ImageUpdateAvailable bool                     `json:"imageUpdateAvailable"`
	ImageUpdateTargets   []ImageUpdateTarget      `json:"imageUpdateTargets,omitempty"`
	ImageLastError       string                   `json:"imageLastError,omitempty"`
	JobID                string                   `json:"jobId"`
	Namespace            string                   `json:"namespace"`
	LastEvalID           string                   `json:"lastEvalId,omitempty"`
	LastDeploymentID     string                   `json:"lastDeploymentId,omitempty"`
	LastError            string                   `json:"lastError,omitempty"`
	PlanningError        *DeploymentPlanningError `json:"planningError,omitempty"`
	RuntimeStatus        string                   `json:"runtimeStatus,omitempty"`
	CreatedAt            time.Time                `json:"createdAt"`
	UpdatedAt            time.Time                `json:"updatedAt"`
}

type ApplicationSummary struct {
	ID                     string                   `json:"id"`
	Name                   string                   `json:"name"`
	Enabled                bool                     `json:"enabled"`
	AutoUpdateEnabled      bool                     `json:"autoUpdateEnabled"`
	ReconcileStopped       bool                     `json:"reconcileStopped,omitempty"`
	ImageReference         string                   `json:"imageReference,omitempty"`
	InstanceCount          int                      `json:"instanceCount"`
	HealthyInstanceCount   int                      `json:"healthyInstanceCount"`
	AbnormalInstanceCount  int                      `json:"abnormalInstanceCount"`
	DeployingInstanceCount int                      `json:"deployingInstanceCount"`
	JobID                  string                   `json:"jobId"`
	Namespace              string                   `json:"namespace"`
	RuntimeStatus          string                   `json:"runtimeStatus,omitempty"`
	ImageUpdateAvailable   bool                     `json:"imageUpdateAvailable"`
	LastError              string                   `json:"lastError,omitempty"`
	PlanningError          *DeploymentPlanningError `json:"planningError,omitempty"`
	UpdatedAt              time.Time                `json:"updatedAt"`
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
	TargetPort      int                `json:"targetPort"`
	OriginServerIDs []string           `json:"originServerIds"`
	AnyAccess       AnyAccessConfig    `json:"anyAccess"`
	Paths           []ReverseProxyPath `json:"paths"`
	// 每域名传输开关（FAC-RP-017）。nil 表示客户端未提交，服务端归一化为
	// 默认值：HTTPS/HTTP2 开、HTTP3 关。
	HTTPSEnabled *bool `json:"httpsEnabled,omitempty"`
	HTTP2Enabled *bool `json:"http2Enabled,omitempty"`
	HTTP3Enabled *bool `json:"http3Enabled,omitempty"`
	// CustomServerDirectives 是该域名 server 块的自定义 nginx 单行指令
	// （FAC-RP-017）；同名指令覆盖面板生成项，其余追加。
	CustomServerDirectives []string `json:"customServerDirectives,omitempty"`
	// HTTPPortMode 是该域名的 80 端口策略覆盖：nil/空=继承设施全局设置，
	// serve=80 直接服务，redirect=有证书且 HTTPS 开启时跳转 443。
	HTTPPortMode *string `json:"httpPortMode,omitempty"`
	// HSTSEnabled 是该域名的 HSTS 覆盖：nil=继承设施全局 HSTS，true=启用，
	// false=关闭；maxAge/includeSubDomains 仍取设施全局设置。
	HSTSEnabled *bool `json:"hstsEnabled,omitempty"`
	// ClientMaxBodySize 是该域名的请求体上限覆盖（nginx size 语法，1k..1024m）；
	// nil/空=继承设施全局 clientMaxBodySize。
	ClientMaxBodySize *string `json:"clientMaxBodySize,omitempty"`
}

type AnyAccessConfig struct {
	Enabled               bool     `json:"enabled"`
	Strategy              string   `json:"strategy"`
	PrimaryOriginServerID string   `json:"primaryOriginServerId,omitempty"`
	RelayServerIDs        []string `json:"relayServerIds,omitempty"`
	// OriginPriority holds the user-arranged order of the origin servers for
	// the primary_backup strategy; the first entry is the primary origin.
	// Application reverse-proxy rules resolve the origin set from the global
	// gateway nodes, so the client only provides this ordering.
	OriginPriority []string `json:"originPriority,omitempty"`
}

type ReverseProxyPath struct {
	Path    string           `json:"path"`
	Options HTTPRouteOptions `json:"options,omitempty"`
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
	TargetPort      int                `json:"targetPort"`
	TargetContainer string             `json:"targetContainer,omitempty"`
	OriginServerIDs []string           `json:"originServerIds"`
	AnyAccess       AnyAccessConfig    `json:"anyAccess"`
	Paths           []ReverseProxyPath `json:"paths"`
	// 解析后的每域名传输开关与自定义 server 指令（FAC-RP-017/FAC-RP-018）。
	// 渲染/跨模块消费方直接使用这些已解析值，不再区分“未提交”。
	HTTPSEnabled           bool     `json:"httpsEnabled"`
	HTTP2Enabled           bool     `json:"http2Enabled"`
	HTTP3Enabled           bool     `json:"http3Enabled"`
	CustomServerDirectives []string `json:"customServerDirectives,omitempty"`
	// 解析后的 80 端口策略覆盖（空=继承设施全局）。
	HTTPPortMode string `json:"httpPortMode,omitempty"`
	// 解析后的 HSTS 覆盖（nil=继承设施全局）与请求体上限覆盖（空=继承）。
	HSTSEnabled       *bool  `json:"hstsEnabled,omitempty"`
	ClientMaxBodySize string `json:"clientMaxBodySize,omitempty"`
}

type ApplicationFile struct {
	// ID is retained for runtime allocation and storage migration only. The
	// public file identity is the application-scoped name.
	ID            string    `json:"-"`
	ApplicationID string    `json:"applicationId"`
	Name          string    `json:"name"`
	Path          string    `json:"-"` // Deprecated compatibility alias for name.
	Kind          string    `json:"kind"`
	ContentType   string    `json:"contentType"`
	Filename      string    `json:"-"` // on-disk basename under content/; defaults to sanitized Name
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	BlobPath      string    `json:"-"` // alpha dual-read absolute path; empty means derive from id+filename
	BlobDir       string    `json:"-"` // edit-session workspace dir; empty means unread source_file_id
	SourceFileID  string    `json:"-"` // committed file id when BlobDir is empty in a session
	Content       []byte    `json:"-"`
	ContentBase64 string    `json:"contentBase64,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type FileSaveInput struct {
	Name          string `json:"name"`
	Path          string `json:"path,omitempty"` // Deprecated compatibility input alias for name.
	Kind          string `json:"kind"`
	ContentType   string `json:"contentType"`
	ContentBase64 string `json:"contentBase64"`
}

type FileDeleteInput struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"` // Deprecated compatibility input alias for name.
}

type FileArchiveInput struct {
	Name     string
	BasePath string // Deprecated compatibility alias for name.
	Kind     string
	FileName string
	Content  []byte
}

const (
	EditSessionStateActive     = "active"
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
	FileKey     string    `json:"-"`
	Name        string    `json:"name"`
	Path        string    `json:"-"` // Deprecated compatibility alias for name.
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
	Name              string `json:"name"`
	Path              string `json:"path,omitempty"` // Deprecated compatibility input alias for name.
	Kind              string `json:"kind"`
	ContentType       string `json:"contentType"`
	ContentBase64     string `json:"contentBase64"`
}

type EditSessionArchiveInput struct {
	Revision          int
	ClientOperationID string
	FileKey           string
	Name              string
	BasePath          string // Deprecated compatibility alias for name.
	Kind              string
	FileName          string
	Content           []byte
}

type EditSessionBinaryInput struct {
	Revision          int
	ClientOperationID string
	Name              string
	Path              string `json:"-"` // Deprecated compatibility alias for name.
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

const (
	ApplyActionNone   = ""
	ApplyActionDeploy = "deploy"
	ApplyActionStop   = "stop"
)

// ApplyHint tells callers whether a follow-up deploy/stop is required after a
// configuration persist. Commit itself does not request coordination.
type ApplyHint struct {
	Needed bool
	Action string
}

type EditCommitResult struct {
	Application     Application     `json:"application"`
	ResourceVersion ResourceVersion `json:"resourceVersion"`
	ApplyRequested  bool            `json:"applyRequested"`
	ApplyNeeded     bool            `json:"applyNeeded"`
	ApplyAction     string          `json:"applyAction,omitempty"`
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
	ApplicationID          string                   `json:"applicationId"`
	RuntimeID              string                   `json:"runtimeId"`
	Status                 string                   `json:"status"`
	Operation              *LifecycleOperation      `json:"operation,omitempty"`
	PlanningError          *DeploymentPlanningError `json:"planningError,omitempty"`
	HealthyInstanceCount   int                      `json:"healthyInstanceCount"`
	AbnormalInstanceCount  int                      `json:"abnormalInstanceCount"`
	DeployingInstanceCount int                      `json:"deployingInstanceCount"`
	Instances              []RuntimeInstance        `json:"instances"`
	ObservedAt             time.Time                `json:"observedAt"`
}

// RuntimeInstance is the panel runtime projection for one app↔server node.
// Observed container fields come from appruntime.InstanceStatus; Job stage and
// errors hang on Operation so Agent RuntimeStatusResponse stays unchanged.
type RuntimeInstance struct {
	appruntime.InstanceStatus
	Operation *LifecycleOperation `json:"operation,omitempty"`
}

type LifecycleOperation struct {
	ID                 string            `json:"id"`
	OperationID        string            `json:"operationId,omitempty"`
	ExecutionID        string            `json:"executionId,omitempty"`
	ApplicationID      string            `json:"applicationId"`
	Type               string            `json:"type"`
	Status             string            `json:"status"`
	TaskID             string            `json:"taskId,omitempty"`
	Generation         int               `json:"generation"`
	SpecHash           string            `json:"specHash,omitempty"`
	Trigger            string            `json:"trigger,omitempty"`
	Stage              string            `json:"stage,omitempty"`
	Attempt            int               `json:"attempt,omitempty"`
	NextRunAt          *time.Time        `json:"nextRunAt,omitempty"`
	Error              string            `json:"error,omitempty"`
	ErrorCode          string            `json:"errorCode,omitempty"`
	ErrorClass         string            `json:"errorClass,omitempty"`
	ErrorDetail        string            `json:"errorDetail,omitempty"`
	MonitorReasonCode  string            `json:"monitorReasonCode,omitempty"`
	EvidenceDeadlineAt *time.Time        `json:"evidenceDeadlineAt,omitempty"`
	GraceDeadlineAt    *time.Time        `json:"graceDeadlineAt,omitempty"`
	Targets            []LifecycleTarget `json:"targets,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	StartedAt          *time.Time        `json:"startedAt,omitempty"`
	FinishedAt         *time.Time        `json:"finishedAt,omitempty"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type LifecycleTarget struct {
	ID                 string     `json:"id"`
	OperationID        string     `json:"operationId"`
	ApplicationID      string     `json:"applicationId"`
	ServerID           string     `json:"serverId"`
	ServerName         string     `json:"serverName,omitempty"`
	Action             string     `json:"action,omitempty"`
	State              string     `json:"state,omitempty"`
	Status             string     `json:"status"`
	TargetKey          string     `json:"targetKey,omitempty"`
	DesiredState       string     `json:"desiredState"`
	DesiredGeneration  int        `json:"desiredGeneration,omitempty"`
	DesiredSpecHash    string     `json:"desiredSpecHash,omitempty"`
	Priority           int        `json:"priority,omitempty"`
	Attempt            int        `json:"attempt,omitempty"`
	NextRunAt          *time.Time `json:"nextRunAt,omitempty"`
	LeaseOwner         string     `json:"leaseOwner,omitempty"`
	LeaseExpiresAt     *time.Time `json:"leaseExpiresAt,omitempty"`
	ClaimedTaskID      string     `json:"claimedTaskId,omitempty"`
	InstanceID         string     `json:"instanceId,omitempty"`
	ContainerName      string     `json:"containerName,omitempty"`
	ContainerID        string     `json:"containerId,omitempty"`
	Stage              string     `json:"stage,omitempty"`
	Error              string     `json:"error,omitempty"`
	ErrorCode          string     `json:"errorCode,omitempty"`
	ErrorMessage       string     `json:"errorMessage,omitempty"`
	ErrorDetail        string     `json:"errorDetail,omitempty"`
	ObservedState      string     `json:"observedState,omitempty"`
	ObservedExitCode   string     `json:"observedExitCode,omitempty"`
	ObservedError      string     `json:"observedError,omitempty"`
	ObservedGeneration int        `json:"observedGeneration,omitempty"`
	ObservedSpecHash   string     `json:"observedSpecHash,omitempty"`
	ObservedImage      string     `json:"observedImage,omitempty"`
	ObservedAt         *time.Time `json:"observedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	FinishedAt         *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type SaveInput struct {
	Name              string             `json:"name"`
	Enabled           bool               `json:"enabled"`
	SpecYAML          string             `json:"specYaml"`
	DeploymentMode    string             `json:"deploymentMode"`
	DeploymentServers []string           `json:"deploymentServers"`
	ReverseProxy      []ReverseProxyRule `json:"reverseProxy"`
	// AutoUpdateEnabled 的对象级开关默认开；nil 表示调用方未提交（创建默认
	// true、更新保留当前值），避免旧客户端整笔保存时把开关静默关掉。
	AutoUpdateEnabled *bool `json:"autoUpdateEnabled,omitempty"`
}

type OperationResult struct {
	TaskID             string      `json:"taskId,omitempty"`
	EvalID             string      `json:"evalId,omitempty"`
	DeploymentID       string      `json:"deploymentId,omitempty"`
	NoChange           bool        `json:"noChange,omitempty"`
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
	JobIDs            []string
	CreatedJobIDs     []string
	planningValidated bool
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
	ResourceID         string `json:"resourceId,omitempty"`
	ResourceName       string `json:"resourceName,omitempty"`
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
