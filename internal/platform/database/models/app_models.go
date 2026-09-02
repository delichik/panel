package models

import "time"

// App 库存量表模型。列名/类型/默认值/约束与
// internal/platform/database/migrations.go 的 app 段 CREATE TABLE 逐一对应；
// 复合/部分索引与 CHECK 约束无法用 orm tag 表达，见 models_test.go 与模块文档。

// Credential 对应 credentials。
type Credential struct {
	ID               string    `orm:"primary_key"`
	Name             string    `orm:"not_null"`
	Type             string    `orm:"not_null"`
	Username         string    `orm:"not_null"`
	SecretCiphertext string    `orm:"not_null;default:''"`
	PasswordSecret   string    `orm:"not_null;default:''"`
	PrivateKeyPath   string    `orm:"not_null;default:''"`
	PassphraseSecret string    `orm:"not_null;default:''"`
	CreatedAt        time.Time `orm:"not_null"`
	UpdatedAt        time.Time `orm:"not_null"`
}

func (*Credential) TableName() string { return "credentials" }

// TableConstraints 返回 credentials 表无法用 orm tag 表达的原始约束。
func (*Credential) TableConstraints() []string {
	return []string{"CHECK(type IN ('password','private_key'))"}
}

// Server 对应 servers。
type Server struct {
	ID                     string         `orm:"primary_key"`
	Name                   string         `orm:"not_null"`
	Host                   string         `orm:"not_null"`
	IPv4                   string         `orm:"not_null;default:'';column:ipv4"`
	IPv6                   string         `orm:"not_null;default:'';column:ipv6"`
	Port                   int            `orm:"not_null"`
	SSHUsername            string         `orm:"not_null;default:''"`
	CredentialID           string         `orm:"not_null;references:credentials(id)"`
	DockerHost             string         `orm:"not_null;default:'unix:///var/run/docker.sock'"`
	Traits                 map[string]any `orm:"json;not_null;default:'{}'"`
	VariablesJSON          map[string]any `orm:"json;not_null;default:'{}'"`
	Notes                  string         `orm:"not_null;default:''"`
	OSID                   string         `orm:"not_null;default:'';column:os_id"`
	OSVersionID            string         `orm:"not_null;default:''"`
	OSPrettyName           string         `orm:"not_null;default:''"`
	OSSupported            bool           `orm:"not_null;default:0"`
	ArchitectureOS         string         `orm:"not_null;default:''"`
	ArchitectureArch       string         `orm:"not_null;default:''"`
	ArchitectureMachine    string         `orm:"not_null;default:''"`
	Reachable              bool           `orm:"not_null;default:0"`
	HostKeyMismatch        bool           `orm:"not_null;default:0;column:host_key_mismatch"`
	SudoPasswordless       bool           `orm:"not_null;default:0"`
	SudoLastCheckedAt      *time.Time
	PrivilegeMode          string `orm:"not_null;default:''"`
	PrivilegeLastCheckedAt *time.Time
	LastCheckedAt          *time.Time
	LastError              string    `orm:"not_null;default:''"`
	CreatedAt              time.Time `orm:"not_null"`
	UpdatedAt              time.Time `orm:"not_null"`
}

func (*Server) TableName() string { return "servers" }

// PackageUpdate 对应 package_updates。
type PackageUpdate struct {
	ServerID         string    `orm:"primary_key;not_null;references:servers(id);on_delete:CASCADE"`
	Name             string    `orm:"primary_key;not_null"`
	InstalledVersion string    `orm:"not_null"`
	CandidateVersion string    `orm:"not_null"`
	Source           string    `orm:"not_null;default:''"`
	RefreshedAt      time.Time `orm:"not_null"`
}

func (*PackageUpdate) TableName() string { return "package_updates" }

// PackageRefresh 对应 package_refreshes。
type PackageRefresh struct {
	ServerID    string    `orm:"primary_key;references:servers(id);on_delete:CASCADE"`
	RefreshedAt time.Time `orm:"not_null"`
}

func (*PackageRefresh) TableName() string { return "package_refreshes" }

// Fail2banConfig 对应 fail2ban_configs。
type Fail2banConfig struct {
	ServerID   string    `orm:"primary_key;references:servers(id);on_delete:CASCADE"`
	ConfigYAML string    `orm:"not_null"`
	Managed    bool      `orm:"not_null;default:0"`
	UpdatedAt  time.Time `orm:"not_null"`
}

func (*Fail2banConfig) TableName() string { return "fail2ban_configs" }

// ImageUpdate 对应 image_updates。
type ImageUpdate struct {
	ServerID        string    `orm:"primary_key;not_null;references:servers(id);on_delete:CASCADE"`
	Reference       string    `orm:"primary_key;not_null"`
	LocalDigest     string    `orm:"not_null;default:''"`
	LatestDigest    string    `orm:"not_null;default:''"`
	UpdateAvailable bool      `orm:"not_null;default:0"`
	LastError       string    `orm:"not_null;default:''"`
	CheckedAt       time.Time `orm:"not_null"`
}

func (*ImageUpdate) TableName() string { return "image_updates" }

// ImageRefresh 对应 image_refreshes。
type ImageRefresh struct {
	ServerID    string    `orm:"primary_key;references:servers(id);on_delete:CASCADE"`
	RefreshedAt time.Time `orm:"not_null"`
}

func (*ImageRefresh) TableName() string { return "image_refreshes" }

// ApplicationReconcileState 对应 application_reconcile_states。
type ApplicationReconcileState struct {
	InstanceID             string    `orm:"primary_key"`
	ApplicationID          string    `orm:"not_null;references:applications(id);on_delete:CASCADE"`
	ServerID               string    `orm:"not_null;references:servers(id);on_delete:CASCADE"`
	ObservedAt             time.Time `orm:"not_null"`
	ReconcileFailures      int       `orm:"not_null;default:0"`
	ReconcileNextRunAt     *time.Time
	ReconcileSuccessStreak int `orm:"not_null;default:0"`
}

func (*ApplicationReconcileState) TableName() string { return "application_reconcile_states" }

// ExtraIndexDDL returns the application_reconcile_states index that orm tags cannot express.
func (*ApplicationReconcileState) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_reconcile_states": {
			"CREATE INDEX IF NOT EXISTS idx_application_reconcile_states_application ON application_reconcile_states(application_id)",
		},
	}
}

// ApplicationRevisionV3 对应 AppDB 中不可变的应用运行时修订。
//
// application_revisions 曾经位于 LogDB。新的控制面必须在同一个数据库
// 边界内保存 revision 与 desired state，worker 执行时只读取这份不可变快照。
// 旧 LogDB 表暂时由兼容迁移保留，但新写入统一落到 AppDB。
type ApplicationRevisionV3 struct {
	ID string `orm:"primary_key"`
	// The legacy LogDB compatibility model uses the same table name without a
	// cross-database foreign key. Keep the revision row self-contained here;
	// application deletion is finalized by the orchestrator rather than by a
	// database cascade.
	ApplicationID       string           `orm:"not_null"`
	Generation          int              `orm:"not_null"`
	SpecHash            string           `orm:"not_null"`
	RenderedRuntimeSpec map[string]any   `orm:"json;not_null;default:'{}'"`
	ManagedFileManifest []map[string]any `orm:"json;not_null;default:'[]'"`
	ImageReference      string           `orm:"not_null;default:''"`
	ResolvedImageDigest string           `orm:"not_null;default:''"`
	SpecYAML            string           `orm:"not_null;default:''"`
	JobJSON             map[string]any   `orm:"json;not_null;default:'{}'"`
	CreatedAt           time.Time        `orm:"not_null"`
}

func (*ApplicationRevisionV3) TableName() string { return "application_revisions" }

// ExtraIndexDDL 返回 revision 表无法用 orm tag 表达的复合索引与唯一约束。
func (*ApplicationRevisionV3) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_revisions": {
			"CREATE INDEX IF NOT EXISTS idx_application_revisions_app_created ON application_revisions(application_id, created_at)",
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_revisions_application_generation ON application_revisions(application_id, generation)",
		},
	}
}

// Job 对应应用/服务器冲突域当前唯一的协调工作。
// 任务中心的 task 行是兼容投影，不能替代本表作为部署事实来源。
type Job struct {
	ID                  string         `orm:"primary_key"`
	ApplicationID       string         `orm:"not_null;references:applications(id);on_delete:RESTRICT"`
	ServerID            string         `orm:"not_null;references:servers(id);on_delete:RESTRICT"`
	InstanceID          string         `orm:"not_null;default:''"`
	Action              string         `orm:"not_null;default:'apply'"`
	DesiredGeneration   int            `orm:"not_null;default:0"`
	DesiredSpecHash     string         `orm:"not_null;default:''"`
	DesiredRevisionID   string         `orm:"not_null;default:''"`
	DesiredSpecJSON     map[string]any `orm:"json;not_null;default:'{}'"`
	RemoveData          bool           `orm:"not_null;default:0"`
	ForceNonce          int64          `orm:"not_null;default:0"`
	State               string         `orm:"not_null;default:'pending'"`
	Priority            int            `orm:"not_null;default:0"`
	Attempts            int            `orm:"not_null;default:0"`
	NextRunAt           *time.Time
	LeaseOwner          string `orm:"not_null;default:''"`
	LeaseToken          string `orm:"not_null;default:''"`
	LeaseExpiresAt      *time.Time
	ExecutionID         string           `orm:"not_null;default:''"`
	IntentID            string           `orm:"not_null;default:''"`
	TriggerType         string           `orm:"not_null;default:''"`
	TriggerResourceType string           `orm:"not_null;default:''"`
	TriggerResourceID   string           `orm:"not_null;default:''"`
	Reason              string           `orm:"not_null;default:''"`
	IdempotencyKey      string           `orm:"not_null;default:''"`
	LastStage           string           `orm:"not_null;default:''"`
	LastStepsJSON       []map[string]any `orm:"json;not_null;default:'[]'"`
	ErrorCode           string           `orm:"not_null;default:''"`
	ErrorClass          string           `orm:"not_null;default:''"`
	ErrorMessage        string           `orm:"not_null;default:''"`
	ErrorDetail         string           `orm:"not_null;default:''"`
	CreatedAt           time.Time        `orm:"not_null"`
	StartedAt           *time.Time
	FinishedAt          *time.Time
	UpdatedAt           time.Time `orm:"not_null"`
}

func (*Job) TableName() string { return "jobs" }

// ExtraIndexDDL 返回 jobs 的活跃冲突域、到期扫描和 intent 查询索引。
func (*Job) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"jobs": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_jobs_active_app_server ON jobs(application_id, server_id) WHERE state IN ('pending','running','failed_retryable')",
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_jobs_idempotency ON jobs(application_id, idempotency_key) WHERE idempotency_key <> ''",
			"CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs(state, next_run_at, priority, created_at)",
			"CREATE INDEX IF NOT EXISTS idx_jobs_intent ON jobs(intent_id, created_at)",
		},
	}
}

// ContainerObservation 对应 container_observations。
type ContainerObservation struct {
	ServerID      string         `orm:"primary_key;not_null;references:servers(id);on_delete:CASCADE"`
	ContainerID   string         `orm:"primary_key;not_null"`
	SampleAt      time.Time      `orm:"not_null"`
	ContainerJSON map[string]any `orm:"json;not_null"`
	SummaryJSON   map[string]any `orm:"json;not_null;default:'{}'"`
	Managed       bool           `orm:"not_null;default:0"`
	ApplicationID string         `orm:"not_null;default:''"`
	InstanceID    string         `orm:"not_null;default:''"`
	UpdatedAt     time.Time      `orm:"not_null"`
}

func (*ContainerObservation) TableName() string { return "container_observations" }

// DockerResourceSnapshot 对应 docker_resource_snapshots。
type DockerResourceSnapshot struct {
	ServerID     string           `orm:"primary_key;not_null;references:servers(id);on_delete:CASCADE"`
	ResourceType string           `orm:"primary_key;not_null"`
	ItemsJSON    []map[string]any `orm:"json;not_null;default:'[]'"`
	ObservedAt   time.Time        `orm:"not_null"`
}

func (*DockerResourceSnapshot) TableName() string { return "docker_resource_snapshots" }

// DNSRecordSnapshot 对应 dns_record_snapshots。
type DNSRecordSnapshot struct {
	DomainID    string           `orm:"primary_key;references:dns_domains(id);on_delete:CASCADE"`
	RecordsJSON []map[string]any `orm:"json;not_null;default:'[]'"`
	ObservedAt  time.Time        `orm:"not_null"`
	LastError   string           `orm:"not_null;default:''"`
}

func (*DNSRecordSnapshot) TableName() string { return "dns_record_snapshots" }

// Application 对应 applications。
type Application struct {
	ID                      string   `orm:"primary_key"`
	Version                 int      `orm:"not_null;default:1"`
	Kind                    string   `orm:"not_null;default:'application'"`
	Name                    string   `orm:"not_null;unique"`
	Enabled                 bool     `orm:"not_null;default:0"`
	DeletionRequested       bool     `orm:"not_null;default:0"`
	ReconcileStopped        bool     `orm:"not_null;default:0"`
	SpecYAML                string   `orm:"not_null"`
	DeploymentMode          string   `orm:"not_null;default:'all'"`
	DeploymentServerIDsJSON []string `orm:"json;not_null;default:'[]';column:deployment_server_ids_json"`

	Generation           int    `orm:"not_null;default:1"`
	SpecHash             string `orm:"not_null;default:''"`
	ImageReference       string `orm:"not_null;default:''"`
	ImageDigest          string `orm:"not_null;default:''"`
	ImageLatestDigest    string `orm:"not_null;default:''"`
	ImageCheckedAt       *time.Time
	ImageUpdateAvailable bool      `orm:"not_null;default:0"`
	ImageLastError       string    `orm:"not_null;default:''"`
	JobID                string    `orm:"not_null"`
	Namespace            string    `orm:"not_null;default:'default'"`
	LastEvalID           string    `orm:"not_null;default:''"`
	LastDeploymentID     string    `orm:"not_null;default:''"`
	LastError            string    `orm:"not_null;default:''"`
	CreatedAt            time.Time `orm:"not_null"`
	UpdatedAt            time.Time `orm:"not_null"`
}

func (*Application) TableName() string { return "applications" }

// ApplicationEditSession 对应 application_edit_sessions。
type ApplicationEditSession struct {
	ID                    string `orm:"primary_key"`
	ApplicationID         string `orm:"not_null;default:''"`
	OwnerID               string `orm:"not_null"`
	ClientDraftKey        string `orm:"not_null;default:''"`
	State                 string `orm:"not_null"`
	BaseResourceVersion   int    `orm:"not_null;default:0"`
	BaseResourceUpdatedAt *time.Time
	DraftJSON             map[string]any `orm:"json;not_null;default:'{}'"`
	Revision              int            `orm:"not_null;default:1"`
	PreviewToken          string         `orm:"not_null;default:''"`
	PreviewRevision       int            `orm:"not_null;default:0"`
	PreviewExpiresAt      *time.Time
	CommitLeaseOwner      string `orm:"not_null;default:''"`
	CommitLeaseExpiresAt  *time.Time
	CommitIdempotencyKey  string         `orm:"not_null;default:''"`
	CommitApplicationID   string         `orm:"not_null;default:''"`
	CommitResultJSON      map[string]any `orm:"json;not_null;default:''"`
	ConflictJSON          map[string]any `orm:"json;not_null;default:''"`
	IdleExpiresAt         time.Time      `orm:"not_null"`
	AbsoluteExpiresAt     time.Time      `orm:"not_null"`
	CreatedAt             time.Time      `orm:"not_null"`
	UpdatedAt             time.Time      `orm:"not_null"`
	CommittedAt           *time.Time
}

func (*ApplicationEditSession) TableName() string { return "application_edit_sessions" }

// ExtraIndexDDL 返回 application_edit_sessions 表无法用 orm tag 表达的复合索引。
func (*ApplicationEditSession) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_edit_sessions": {
			"CREATE INDEX IF NOT EXISTS idx_application_edit_sessions_expiry ON application_edit_sessions(state, idle_expires_at, absolute_expires_at)",
		},
	}
}

// ApplicationEditSessionFile 对应 application_edit_session_files。
type ApplicationEditSessionFile struct {
	SessionID   string    `orm:"primary_key;not_null;references:application_edit_sessions(id);on_delete:CASCADE"`
	FileKey     string    `orm:"primary_key;not_null"`
	Name        string    `orm:"not_null"`
	Kind        string    `orm:"not_null"`
	ContentType string    `orm:"not_null;default:''"`
	Size        int       `orm:"not_null;default:0"`
	SHA256      string    `orm:"not_null;default:''"`
	BlobPath    string    `orm:"not_null"`
	State       string    `orm:"not_null;default:'ready'"`
	CreatedAt   time.Time `orm:"not_null"`
	UpdatedAt   time.Time `orm:"not_null"`
}

func (*ApplicationEditSessionFile) TableName() string { return "application_edit_session_files" }

// TableConstraints 返回 application_edit_session_files 表无法用 orm tag 表达的原始约束。
func (*ApplicationEditSessionFile) TableConstraints() []string {
	return []string{"CHECK(kind IN ('binary','template','archive'))"}
}

// ExtraIndexDDL 返回 application_edit_session_files 表无法用 orm tag 表达的复合 UNIQUE。
func (*ApplicationEditSessionFile) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_edit_session_files": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_edit_session_files_session_name ON application_edit_session_files(session_id, name)",
		},
	}
}

// ApplicationEditSessionOperation 对应 application_edit_session_operations。
type ApplicationEditSessionOperation struct {
	SessionID         string         `orm:"primary_key;not_null;references:application_edit_sessions(id);on_delete:CASCADE"`
	ClientOperationID string         `orm:"primary_key;not_null"`
	IdempotencyKey    string         `orm:"not_null"`
	RequestHash       string         `orm:"not_null"`
	ResultJSON        map[string]any `orm:"json;not_null;default:''"`
	CreatedAt         time.Time      `orm:"not_null"`
}

func (*ApplicationEditSessionOperation) TableName() string {
	return "application_edit_session_operations"
}

// ExtraIndexDDL 返回 application_edit_session_operations 表无法用 orm tag 表达的复合 UNIQUE。
func (*ApplicationEditSessionOperation) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_edit_session_operations": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_edit_session_operations_session_idempotency ON application_edit_session_operations(session_id, idempotency_key)",
		},
	}
}

// ApplicationFile 对应 application_files。
type ApplicationFile struct {
	ID            string `orm:"primary_key"`
	ApplicationID string `orm:"not_null;references:applications(id);on_delete:CASCADE"`
	Name          string `orm:"not_null"`
	Kind          string `orm:"not_null"`
	ContentType   string `orm:"not_null;default:''"`
	Size          int    `orm:"not_null;default:0"`
	SHA256        string `orm:"not_null;default:''"`
	Content       []byte
	CreatedAt     time.Time `orm:"not_null"`
	UpdatedAt     time.Time `orm:"not_null"`
}

func (*ApplicationFile) TableName() string { return "application_files" }

// TableConstraints 返回 application_files 表无法用 orm tag 表达的原始约束。
func (*ApplicationFile) TableConstraints() []string {
	return []string{"CHECK(kind IN ('binary','template','archive'))"}
}

// ExtraIndexDDL 返回 application_files 表无法用 orm tag 表达的复合 UNIQUE。
func (*ApplicationFile) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_files": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_files_application_name ON application_files(application_id, name)",
		},
	}
}

// ApplicationInstance 对应 application_instances。
type ApplicationInstance struct {
	ID                     string         `orm:"primary_key"`
	ApplicationID          string         `orm:"not_null;references:applications(id);on_delete:CASCADE"`
	ServerID               string         `orm:"not_null;references:servers(id);on_delete:CASCADE"`
	ContainerName          string         `orm:"not_null"`
	ContainerID            string         `orm:"not_null;default:''"`
	DesiredState           string         `orm:"not_null;default:'running'"`
	DesiredGeneration      int            `orm:"not_null;default:0"`
	DesiredSpecHash        string         `orm:"not_null;default:''"`
	DesiredRevisionID      string         `orm:"not_null;default:''"`
	DesiredSpecJSON        map[string]any `orm:"json;not_null;default:'{}'"`
	Status                 string         `orm:"not_null;default:'pending'"`
	RuntimeSpecJSON        map[string]any `orm:"json;not_null;default:'{}'"`
	LastDeployedGeneration int            `orm:"not_null;default:0"`
	ObservedState          string         `orm:"not_null;default:'unknown'"`
	ObservedContainerName  string         `orm:"not_null;default:''"`
	ObservedContainerID    string         `orm:"not_null;default:''"`
	ObservedGeneration     int            `orm:"not_null;default:0"`
	ObservedSpecHash       string         `orm:"not_null;default:''"`
	ObservedImageDigest    string         `orm:"not_null;default:''"`
	ObservedAt             *time.Time
	ObservedSequence       int64  `orm:"not_null;default:0"`
	ObservedSource         string `orm:"not_null;default:''"`
	LastReconcileJobID     string `orm:"not_null;default:''"`
	LastErrorCode          string `orm:"not_null;default:''"`
	LastErrorClass         string `orm:"not_null;default:''"`
	LastErrorMessage       string `orm:"not_null;default:''"`
	LastErrorDetail        string `orm:"not_null;default:''"`
	LastErrorAt            *time.Time
	LastError              string    `orm:"not_null;default:''"`
	CreatedAt              time.Time `orm:"not_null"`
	UpdatedAt              time.Time `orm:"not_null"`
}

func (*ApplicationInstance) TableName() string { return "application_instances" }

// ExtraIndexDDL 返回 application_instances 表无法用 orm tag 表达的复合 UNIQUE。
func (*ApplicationInstance) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_instances": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_instances_application_server ON application_instances(application_id, server_id)",
			"CREATE INDEX IF NOT EXISTS idx_application_instances_server_desired ON application_instances(server_id, desired_state)",
		},
	}
}

// FacilityAppConfig 对应 facility_app_configs。
type FacilityAppConfig struct {
	ID                      string         `orm:"primary_key"`
	Version                 int            `orm:"not_null;default:1"`
	DeploymentServerIDsJSON []string       `orm:"json;not_null;default:'[]';column:deployment_server_ids_json"`

	DNSSyncJSON map[string]any `orm:"json;not_null;default:'{}';column:dns_sync_json"`
	LastError   string         `orm:"not_null;default:''"`
	UpdatedAt   time.Time      `orm:"not_null"`
}

func (*FacilityAppConfig) TableName() string { return "facility_app_configs" }

// ReverseProxyRoute 对应 reverse_proxy_routes。domain 全局唯一，app_id 为所属
// 应用 id；设施代理自身的路由使用 facility-reverse-proxy 作为 app_id。
type ReverseProxyRoute struct {
	Domain          string           `orm:"primary_key;not_null"`
	AppID           string           `orm:"not_null;column:app_id"`
	OriginServerIDs []string         `orm:"json;not_null;default:'[]';column:origin_server_ids"`
	AnyAccessJSON   map[string]any   `orm:"json;not_null;default:'{}';column:any_access_json"`
	TargetPort      int              `orm:"not_null;default:0;column:target_port"`
	PathsJSON       []map[string]any `orm:"json;not_null;default:'[]';column:paths_json"`
	CreatedAt       time.Time        `orm:"not_null"`
	UpdatedAt       time.Time        `orm:"not_null"`
}

func (*ReverseProxyRoute) TableName() string { return "reverse_proxy_routes" }

// ExtraIndexDDL 返回 reverse_proxy_routes 无法用 orm tag 表达的索引。
func (*ReverseProxyRoute) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"reverse_proxy_routes": {
			"CREATE INDEX IF NOT EXISTS idx_reverse_proxy_routes_app_id ON reverse_proxy_routes(app_id)",
		},
	}
}

// FacilityStaticAsset 对应 facility_static_assets。
type FacilityStaticAsset struct {
	ID          string    `orm:"primary_key"`
	Name        string    `orm:"not_null;unique"`
	Kind        string    `orm:"not_null"`
	ContentMode string    `orm:"not_null;default:'binary'"`
	Filename    string    `orm:"not_null;default:''"`
	Size        int       `orm:"not_null;default:0"`
	SHA256      string    `orm:"not_null;default:''"`
	CreatedAt   time.Time `orm:"not_null"`
	UpdatedAt   time.Time `orm:"not_null"`
}

func (*FacilityStaticAsset) TableName() string { return "facility_static_assets" }

// TableConstraints 返回 facility_static_assets 表无法用 orm tag 表达的原始约束。
func (*FacilityStaticAsset) TableConstraints() []string {
	return []string{
		"CHECK(kind IN ('uploaded_file','uploaded_bundle'))",
		"CHECK(content_mode IN ('text','binary'))",
	}
}

// FacilityEditSession 对应 facility_edit_sessions。
type FacilityEditSession struct {
	ID                   string         `orm:"primary_key"`
	OwnerID              string         `orm:"not_null"`
	ClientDraftKey       string         `orm:"not_null;default:''"`
	State                string         `orm:"not_null"`
	BaseResourceVersion  int            `orm:"not_null;default:0"`
	DraftJSON            map[string]any `orm:"json;not_null;default:'{}'"`
	Revision             int            `orm:"not_null;default:1"`
	PreviewToken         string         `orm:"not_null;default:''"`
	PreviewRevision      int            `orm:"not_null;default:0"`
	PreviewExpiresAt     *time.Time
	CommitLeaseOwner     string `orm:"not_null;default:''"`
	CommitLeaseExpiresAt *time.Time
	CommitIdempotencyKey string         `orm:"not_null;default:''"`
	CommitResultJSON     map[string]any `orm:"json;not_null;default:''"`
	ManifestPath         string         `orm:"not_null;default:''"`
	ConflictJSON         map[string]any `orm:"json;not_null;default:''"`
	IdleExpiresAt        time.Time      `orm:"not_null"`
	AbsoluteExpiresAt    time.Time      `orm:"not_null"`
	CreatedAt            time.Time      `orm:"not_null"`
	UpdatedAt            time.Time      `orm:"not_null"`
	CommittedAt          *time.Time
}

func (*FacilityEditSession) TableName() string { return "facility_edit_sessions" }

// ExtraIndexDDL 返回 facility_edit_sessions 表无法用 orm tag 表达的复合索引。
func (*FacilityEditSession) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"facility_edit_sessions": {
			"CREATE INDEX IF NOT EXISTS idx_facility_edit_sessions_expiry ON facility_edit_sessions(state, idle_expires_at, absolute_expires_at)",
		},
	}
}

// FacilityEditSessionAsset 对应 facility_edit_session_assets。
type FacilityEditSessionAsset struct {
	SessionID     string    `orm:"primary_key;not_null;references:facility_edit_sessions(id);on_delete:CASCADE"`
	AssetKey      string    `orm:"primary_key;not_null"`
	SourceAssetID string    `orm:"not_null;default:''"`
	Name          string    `orm:"not_null"`
	Kind          string    `orm:"not_null"`
	ContentMode   string    `orm:"not_null;default:'binary'"`
	Filename      string    `orm:"not_null;default:''"`
	Size          int       `orm:"not_null;default:0"`
	SHA256        string    `orm:"not_null;default:''"`
	ContentSHA256 string    `orm:"not_null;default:''"`
	BlobDir       string    `orm:"not_null;default:''"`
	State         string    `orm:"not_null;default:'ready'"`
	CreatedAt     time.Time `orm:"not_null"`
	UpdatedAt     time.Time `orm:"not_null"`
}

func (*FacilityEditSessionAsset) TableName() string { return "facility_edit_session_assets" }

// TableConstraints 返回 facility_edit_session_assets 表无法用 orm tag 表达的原始约束。
func (*FacilityEditSessionAsset) TableConstraints() []string {
	return []string{
		"CHECK(kind IN ('uploaded_file','uploaded_bundle'))",
		"CHECK(content_mode IN ('text','binary'))",
	}
}

// ExtraIndexDDL 返回 facility_edit_session_assets 表无法用 orm tag 表达的复合 UNIQUE 索引。
func (*FacilityEditSessionAsset) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"facility_edit_session_assets": {
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_facility_edit_session_assets_name ON facility_edit_session_assets(session_id, name)",
		},
	}
}

// FacilityEditSessionOperation 对应 facility_edit_session_operations。
type FacilityEditSessionOperation struct {
	SessionID         string         `orm:"primary_key;not_null;references:facility_edit_sessions(id);on_delete:CASCADE"`
	ClientOperationID string         `orm:"primary_key;not_null"`
	IdempotencyKey    string         `orm:"not_null"`
	RequestHash       string         `orm:"not_null"`
	ResultJSON        map[string]any `orm:"json;not_null;default:''"`
	CreatedAt         time.Time      `orm:"not_null"`
}

func (*FacilityEditSessionOperation) TableName() string { return "facility_edit_session_operations" }

// ExtraIndexDDL 返回 facility_edit_session_operations 表无法用 orm tag 表达的复合 UNIQUE。
func (*FacilityEditSessionOperation) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"facility_edit_session_operations": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_facility_edit_session_operations_session_idempotency ON facility_edit_session_operations(session_id, idempotency_key)",
		},
	}
}

// DNSDomain 对应 dns_domains。
type DNSDomain struct {
	ID                       string         `orm:"primary_key"`
	Name                     string         `orm:"not_null;unique"`
	Provider                 string         `orm:"not_null"`
	ProviderConfigJSON       map[string]any `orm:"json;not_null;default:'{}'"`
	ProviderSecretCiphertext string         `orm:"not_null;default:''"`
	CreatedAt                time.Time      `orm:"not_null"`
	UpdatedAt                time.Time      `orm:"not_null"`
}

func (*DNSDomain) TableName() string { return "dns_domains" }

// TableConstraints 返回 dns_domains 表无法用 orm tag 表达的原始约束。
func (*DNSDomain) TableConstraints() []string {
	return []string{"CHECK(provider IN ('cloudflare'))"}
}

// Certificate 对应 certificates。
type Certificate struct {
	ID              string   `orm:"primary_key"`
	Name            string   `orm:"not_null"`
	DomainID        string   `orm:"not_null;default:'';references:dns_domains(id)"`
	Domain          string   `orm:"not_null"`
	Prefix          string   `orm:"not_null;default:'@'"`
	Scope           string   `orm:"not_null"`
	DomainsJSON     []string `orm:"json;not_null;default:'[]'"`
	VariableName    string   `orm:"not_null;unique"`
	CertificatePath string   `orm:"not_null"`
	PrivateKeyPath  string   `orm:"not_null"`
	Issuer          string   `orm:"not_null;default:''"`
	Status          string   `orm:"not_null;default:'pending'"`
	LastError       string   `orm:"not_null;default:''"`
	AutoRenew       bool     `orm:"not_null;default:1"`
	NextRenewAt     *time.Time
	NotBefore       *time.Time
	NotAfter        *time.Time
	CreatedAt       time.Time `orm:"not_null"`
	UpdatedAt       time.Time `orm:"not_null"`
}

func (*Certificate) TableName() string { return "certificates" }

// TableConstraints 返回 certificates 表无法用 orm tag 表达的原始约束。
func (*Certificate) TableConstraints() []string {
	return []string{"CHECK(scope IN ('single','wildcard','prefixes'))"}
}

// SelfSignedCertificate 对应 self_signed_certificates。
type SelfSignedCertificate struct {
	ID              string   `orm:"primary_key"`
	ParentCAID      string   `orm:"not_null;default:'';column:parent_ca_id"`
	Kind            string   `orm:"not_null"`
	Name            string   `orm:"not_null"`
	CommonName      string   `orm:"not_null"`
	DNSNamesJSON    []string `orm:"json;not_null;default:'[]'"`
	IPAddressesJSON []string `orm:"json;not_null;default:'[]'"`
	CertificatePath string   `orm:"not_null"`
	PrivateKeyPath  string   `orm:"not_null"`
	PublicKeyPath   string   `orm:"not_null"`
	Fingerprint     string   `orm:"not_null;default:''"`
	NotBefore       *time.Time
	NotAfter        *time.Time
	CreatedAt       time.Time `orm:"not_null"`
	UpdatedAt       time.Time `orm:"not_null"`
}

func (*SelfSignedCertificate) TableName() string { return "self_signed_certificates" }

// TableConstraints 返回 self_signed_certificates 表无法用 orm tag 表达的原始约束。
func (*SelfSignedCertificate) TableConstraints() []string {
	return []string{"CHECK(kind IN ('ca','leaf'))"}
}

// KeyAsset 对应 key_assets。
type KeyAsset struct {
	ID                    string         `orm:"primary_key"`
	Type                  string         `orm:"not_null;index"`
	Name                  string         `orm:"not_null;unique"`
	ParentAssetID         string         `orm:"not_null;default:'';index"`
	Algorithm             string         `orm:"not_null;default:''"`
	KeySize               int            `orm:"not_null;default:0"`
	CommonName            string         `orm:"not_null;default:''"`
	DNSNamesJSON          []string       `orm:"json;not_null;default:'[]'"`
	IPAddressesJSON       []string       `orm:"json;not_null;default:'[]'"`
	Fingerprint           string         `orm:"not_null;default:''"`
	CertificateCiphertext string         `orm:"not_null;default:''"`
	PrivateKeyCiphertext  string         `orm:"not_null;default:''"`
	PublicKey             string         `orm:"not_null;default:''"`
	MetadataJSON          map[string]any `orm:"json;not_null;default:'{}'"`
	NotBefore             *time.Time
	NotAfter              *time.Time
	CreatedAt             time.Time `orm:"not_null"`
	UpdatedAt             time.Time `orm:"not_null"`
}

func (*KeyAsset) TableName() string { return "key_assets" }

// TableConstraints 返回 key_assets 表无法用 orm tag 表达的原始约束。
func (*KeyAsset) TableConstraints() []string {
	return []string{"CHECK(type IN ('ca_certificate','tls_certificate','ssh_key_pair'))"}
}

// OverviewCardConfiguration 对应 overview_card_configurations。
type OverviewCardConfiguration struct {
	ID        string           `orm:"primary_key"`
	CardsJSON []map[string]any `orm:"json;not_null;default:'[]'"`
	UpdatedAt time.Time        `orm:"not_null"`
}

func (*OverviewCardConfiguration) TableName() string { return "overview_card_configurations" }

// TableConstraints 返回 overview_card_configurations 表无法用 orm tag 表达的原始约束。
func (*OverviewCardConfiguration) TableConstraints() []string {
	return []string{"CHECK(id = 'default')"}
}

// RuntimeSetting 对应 runtime_settings。
type RuntimeSetting struct {
	Key       string    `orm:"primary_key"`
	Value     string    `orm:"not_null"`
	UpdatedAt time.Time `orm:"not_null"`
}

func (*RuntimeSetting) TableName() string { return "runtime_settings" }

// AuthState 对应 auth_state。
type AuthState struct {
	Key       string    `orm:"primary_key"`
	Value     string    `orm:"not_null"`
	UpdatedAt time.Time `orm:"not_null"`
}

func (*AuthState) TableName() string { return "auth_state" }

// AuthAccount 对应 auth_accounts。
type AuthAccount struct {
	ID                     string    `orm:"primary_key"`
	Username               string    `orm:"not_null"`
	PasswordHash           string    `orm:"not_null"`
	PasswordChangeRequired bool      `orm:"not_null;default:1"`
	CreatedAt              time.Time `orm:"not_null"`
	UpdatedAt              time.Time `orm:"not_null"`
}

func (*AuthAccount) TableName() string { return "auth_accounts" }

// TableConstraints 返回 auth_accounts 表无法用 orm tag 表达的原始约束。
func (*AuthAccount) TableConstraints() []string {
	return []string{"CHECK(id = 'admin')"}
}

// StorageShareConfig 对应 storage_share_configs（存储共享设施配置）。
// 每台存储服务器各自保存自己的根目录（servers_json）。
type StorageShareConfig struct {
	ID          string           `orm:"primary_key"`
	Version     int              `orm:"not_null;default:1"`
	ServersJSON []map[string]any `orm:"json;not_null;default:'[]';column:servers_json"`
	LastError   string           `orm:"not_null;default:''"`
	UpdatedAt   time.Time        `orm:"not_null"`
}

func (*StorageShareConfig) TableName() string { return "storage_share_configs" }

// StorageSharePartition 对应 storage_share_partitions（存储共享按应用/服务器分配的分区记录）。
type StorageSharePartition struct {
	ID                string    `orm:"primary_key"`
	ApplicationID     string    `orm:"not_null"`
	ApplicationName   string    `orm:"not_null;default:''"`
	ServerID          string    `orm:"not_null"`
	ServerName        string    `orm:"not_null;default:''"`
	StorageServerID   string    `orm:"not_null;default:''"`
	StorageServerName string    `orm:"not_null;default:''"`
	Path              string    `orm:"not_null"`
	Target            string    `orm:"not_null;default:''"`
	VolumeName        string    `orm:"not_null;default:''"`
	CreatedAt         time.Time `orm:"not_null"`
	UpdatedAt         time.Time `orm:"not_null"`
}

func (*StorageSharePartition) TableName() string { return "storage_share_partitions" }

// ExtraIndexDDL 返回 storage_share_partitions 表无法用 orm tag 表达的复合 UNIQUE。
func (*StorageSharePartition) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"storage_share_partitions": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_storage_share_partitions_storage_application_server_target ON storage_share_partitions(storage_server_id, application_id, server_id, target)",
		},
	}
}
