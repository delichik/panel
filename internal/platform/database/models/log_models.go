package models

import "time"

// Log 库存量表模型。列名/类型/默认值/约束与
// internal/platform/database/migrations.go 的 task 段 CREATE TABLE 逐一对应；
// 复合/部分索引与 CHECK 约束无法用 orm tag 表达，见 models_test.go 与模块文档。

// Task 对应 tasks。
type Task struct {
	ID                  string         `orm:"primary_key"`
	OperationID         string         `orm:"not_null;default:''"`
	Type                string         `orm:"not_null"`
	ParentTaskID        string         `orm:"not_null;default:''"`
	ChildIndex          int            `orm:"not_null;default:0"`
	ChildCount          int            `orm:"not_null;default:0"`
	ExecutionMode       string         `orm:"not_null;default:''"`
	ConcurrencyKey      string         `orm:"not_null;default:''"`
	ScheduleKey         string         `orm:"not_null;default:''"`
	ServerID            string         `orm:"not_null;default:''"`
	NodeID              string         `orm:"not_null;default:''"`
	ResourceType        string         `orm:"not_null;default:''"`
	ResourceID          string         `orm:"not_null;default:''"`
	TriggerType         string         `orm:"not_null;default:''"`
	TriggerResourceType string         `orm:"not_null;default:''"`
	TriggerResourceID   string         `orm:"not_null;default:''"`
	TriggerTaskID       string         `orm:"not_null;default:''"`
	TriggeredBy         string         `orm:"not_null;default:''"`
	ParamsJSON          map[string]any `orm:"json;not_null;default:'{}'"`
	MetadataJSON        map[string]any `orm:"json;not_null;default:'{}'"`
	Status              string         `orm:"not_null"`
	Stage               string         `orm:"not_null;default:''"`
	Percentage          *float64
	Summary             string `orm:"not_null;default:''"`
	Error               string `orm:"not_null;default:''"`
	RetryCount          int    `orm:"not_null;default:0"`
	MaxRetries          int    `orm:"not_null;default:0"`
	NextRunAt           *time.Time
	CreatedAt           time.Time `orm:"not_null"`
	StartedAt           *time.Time
	FinishedAt          *time.Time
}

func (*Task) TableName() string { return "tasks" }

// ExtraIndexDDL 返回 tasks 表无法用 orm tag 表达的复合索引。
func (*Task) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"tasks": {
			"CREATE INDEX IF NOT EXISTS idx_tasks_type_status ON tasks(type, status)",
			"CREATE INDEX IF NOT EXISTS idx_tasks_concurrency_status_created ON tasks(concurrency_key, status, created_at)",
			"CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id, child_index)",
			"CREATE INDEX IF NOT EXISTS idx_tasks_status_next_run ON tasks(status, next_run_at)",
		},
	}
}

// TaskStep 对应 task_steps。
type TaskStep struct {
	ID           string         `orm:"primary_key"`
	TaskID       string         `orm:"not_null;references:tasks(id);on_delete:CASCADE"`
	Step         string         `orm:"not_null"`
	Status       string         `orm:"not_null"`
	Percentage   float64        `orm:"not_null;default:0"`
	MetadataJSON map[string]any `orm:"json;not_null;default:'{}'"`
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Error        string `orm:"not_null;default:''"`
}

func (*TaskStep) TableName() string { return "task_steps" }

// ExtraIndexDDL 返回 task_steps 表无法用 orm tag 表达的复合 UNIQUE。
func (*TaskStep) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"task_steps": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_task_steps_task_step ON task_steps(task_id, step)",
		},
	}
}

// TaskLog 对应 task_logs。
type TaskLog struct {
	ID     int64     `orm:"primary_key;auto_increment"`
	TaskID string    `orm:"not_null;references:tasks(id);on_delete:CASCADE"`
	Time   time.Time `orm:"not_null"`
	Stream string    `orm:"not_null"`
	Line   string    `orm:"not_null"`
}

func (*TaskLog) TableName() string { return "task_logs" }

// ExtraIndexDDL returns the task_logs index that orm tags cannot express.
func (*TaskLog) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"task_logs": {
			"CREATE INDEX IF NOT EXISTS idx_task_logs_task_id ON task_logs(task_id)",
		},
	}
}

// ApplicationRevision 对应 application_revisions。
type ApplicationRevision struct {
	ID            string         `orm:"primary_key"`
	ApplicationID string         `orm:"not_null"`
	Generation    int            `orm:"not_null"`
	SpecHash      string         `orm:"not_null"`
	SpecYAML      string         `orm:"not_null"`
	JobJSON       map[string]any `orm:"json;not_null"`
	CreatedAt     time.Time      `orm:"not_null"`
}

func (*ApplicationRevision) TableName() string { return "application_revisions" }

// ExtraIndexDDL 返回 application_revisions 表无法用 orm tag 表达的复合索引与复合 UNIQUE。
func (*ApplicationRevision) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_revisions": {
			"CREATE INDEX IF NOT EXISTS idx_application_revisions_app_created ON application_revisions(application_id, created_at)",
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_revisions_application_generation ON application_revisions(application_id, generation)",
		},
	}
}

// ApplicationLifecycleOperation 对应 application_lifecycle_operations。
type ApplicationLifecycleOperation struct {
	ID            string    `orm:"primary_key"`
	ApplicationID string    `orm:"not_null"`
	Type          string    `orm:"not_null"`
	Status        string    `orm:"not_null;default:'pending'"`
	TaskID        string    `orm:"not_null;default:''"`
	Generation    int       `orm:"not_null;default:0"`
	SpecHash      string    `orm:"not_null;default:''"`
	Trigger       string    `orm:"not_null;default:''"`
	Error         string    `orm:"not_null;default:''"`
	CreatedAt     time.Time `orm:"not_null"`
	StartedAt     *time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time `orm:"not_null"`
}

func (*ApplicationLifecycleOperation) TableName() string { return "application_lifecycle_operations" }

// ExtraIndexDDL 返回 application_lifecycle_operations 表无法用 orm tag 表达的复合索引。
func (*ApplicationLifecycleOperation) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_lifecycle_operations": {
			"CREATE INDEX IF NOT EXISTS idx_application_lifecycle_operations_app_created ON application_lifecycle_operations(application_id, created_at)",
		},
	}
}

// ApplicationLifecycleTarget 对应 application_lifecycle_targets。
type ApplicationLifecycleTarget struct {
	ID                string    `orm:"primary_key"`
	OperationID       string    `orm:"not_null;references:application_lifecycle_operations(id);on_delete:CASCADE"`
	ApplicationID     string    `orm:"not_null"`
	ServerID          string    `orm:"not_null"`
	Action            string    `orm:"not_null;default:'apply'"`
	State             string    `orm:"not_null;default:'planned'"`
	Status            string    `orm:"not_null;default:'pending'"`
	TargetKey         string    `orm:"not_null;default:''"`
	DesiredState      string    `orm:"not_null;default:'running'"`
	DesiredGeneration int       `orm:"not_null;default:0"`
	DesiredSpecHash   string    `orm:"not_null;default:''"`
	Priority          int       `orm:"not_null;default:0"`
	Attempt           int       `orm:"not_null;default:0"`
	NextRunAt         time.Time `orm:"not_null;default:''"`
	LeaseOwner        string    `orm:"not_null;default:''"`
	LeaseExpiresAt    time.Time `orm:"not_null;default:''"`
	ClaimedTaskID     string    `orm:"not_null;default:''"`
	InstanceID        string    `orm:"not_null;default:''"`
	ContainerName     string    `orm:"not_null;default:''"`
	ContainerID       string    `orm:"not_null;default:''"`
	Stage             string    `orm:"not_null;default:''"`
	Error             string    `orm:"not_null;default:''"`
	ErrorCode         string    `orm:"not_null;default:''"`
	ErrorMessage      string    `orm:"not_null;default:''"`
	ErrorDetail       string    `orm:"not_null;default:''"`
	CreatedAt         time.Time `orm:"not_null"`
	StartedAt         *time.Time
	FinishedAt        *time.Time
	UpdatedAt         time.Time `orm:"not_null"`
}

func (*ApplicationLifecycleTarget) TableName() string { return "application_lifecycle_targets" }

// ExtraIndexDDL 返回 application_lifecycle_targets 表无法用 orm tag 表达的
// 复合 UNIQUE、复合索引与部分唯一索引（活跃目标去重）。
func (*ApplicationLifecycleTarget) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_lifecycle_targets": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_lifecycle_targets_operation_server ON application_lifecycle_targets(operation_id, server_id)",
			"CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_state_due ON application_lifecycle_targets(state, next_run_at)",
			"CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_app_server ON application_lifecycle_targets(application_id, server_id, state)",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_application_lifecycle_targets_active_key ON application_lifecycle_targets(target_key) WHERE target_key <> '' AND state IN ('planned','ready','claimed','preparing','applying','stopping','purging','verifying','failed_retryable')",
		},
	}
}

// RuntimeEvent 对应 runtime_events。
type RuntimeEvent struct {
	ID              string    `orm:"primary_key"`
	EventType       string    `orm:"not_null"`
	Category        string    `orm:"not_null"`
	SubjectType     string    `orm:"not_null;default:''"`
	SubjectID       string    `orm:"not_null;default:''"`
	OperationID     string    `orm:"not_null;default:''"`
	Severity        string    `orm:"not_null;default:'info'"`
	Source          string    `orm:"not_null;default:''"`
	SourceModule    string    `orm:"not_null;default:''"`
	SourceType      string    `orm:"not_null;default:''"`
	SourceID        string    `orm:"not_null;default:''"`
	DedupeKey       string    `orm:"not_null;default:''"`
	Summary         string    `orm:"not_null;default:''"`
	OccurredAt      time.Time `orm:"not_null"`
	DetailAvailable bool      `orm:"not_null;default:0"`
	DetailPrunedAt  *time.Time
	CreatedAt       time.Time `orm:"not_null"`
}

func (*RuntimeEvent) TableName() string { return "runtime_events" }

// ExtraIndexDDL 返回 runtime_events 表无法用 orm tag 表达的部分唯一与复合索引。
func (*RuntimeEvent) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"runtime_events": {
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_runtime_events_dedupe ON runtime_events(dedupe_key) WHERE dedupe_key <> ''",
			"CREATE INDEX IF NOT EXISTS idx_runtime_events_category_time ON runtime_events(category, occurred_at)",
			"CREATE INDEX IF NOT EXISTS idx_runtime_events_subject_time ON runtime_events(subject_type, subject_id, occurred_at)",
			"CREATE INDEX IF NOT EXISTS idx_runtime_events_operation_time ON runtime_events(operation_id, occurred_at)",
		},
	}
}

// RuntimeEventDetail 对应 runtime_event_details。
type RuntimeEventDetail struct {
	EventID    string           `orm:"primary_key;references:runtime_events(id);on_delete:CASCADE"`
	Payload    map[string]any   `orm:"json;not_null;default:'{}'"`
	Error      string           `orm:"not_null;default:''"`
	LogRefs    []map[string]any `orm:"json;not_null;default:'[]'"`
	TaskRefs   []map[string]any `orm:"json;not_null;default:'[]'"`
	TargetRefs []map[string]any `orm:"json;not_null;default:'[]'"`
	CreatedAt  time.Time        `orm:"not_null"`
	PrunedAt   *time.Time
}

func (*RuntimeEventDetail) TableName() string { return "runtime_event_details" }

// ApplicationOperationRecord 对应 application_operation_records。
type ApplicationOperationRecord struct {
	OperationID             string `orm:"primary_key"`
	ApplicationID           string `orm:"not_null"`
	ApplicationNameSnapshot string `orm:"not_null;default:''"`
	Action                  string `orm:"not_null;default:''"`
	Source                  string `orm:"not_null;default:''"`
	TriggeredBy             string `orm:"not_null;default:''"`
	TriggerReason           string `orm:"not_null;default:''"`
	Status                  string `orm:"not_null;default:'queued'"`
	StartedAt               *time.Time
	FinishedAt              *time.Time
	TargetTotal             int       `orm:"not_null;default:0"`
	TargetSucceeded         int       `orm:"not_null;default:0"`
	TargetFailed            int       `orm:"not_null;default:0"`
	LatestEventAt           time.Time `orm:"not_null"`
	DetailAvailable         bool      `orm:"not_null;default:0"`
	FailureSummary          string    `orm:"not_null;default:''"`
	CreatedAt               time.Time `orm:"not_null"`
	UpdatedAt               time.Time `orm:"not_null"`
}

func (*ApplicationOperationRecord) TableName() string { return "application_operation_records" }

// ExtraIndexDDL 返回 application_operation_records 表无法用 orm tag 表达的复合索引。
func (*ApplicationOperationRecord) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_operation_records": {
			"CREATE INDEX IF NOT EXISTS idx_application_operation_records_app_time ON application_operation_records(application_id, latest_event_at)",
			"CREATE INDEX IF NOT EXISTS idx_application_operation_records_status_time ON application_operation_records(status, latest_event_at)",
		},
	}
}

// KeyAssetExport 对应 key_asset_exports。
type KeyAssetExport struct {
	TaskID    string    `orm:"primary_key"`
	Filename  string    `orm:"not_null"`
	FilePath  string    `orm:"not_null"`
	ExpiresAt time.Time `orm:"not_null;index"`
	CreatedAt time.Time `orm:"not_null"`
	UpdatedAt time.Time `orm:"not_null"`
}

func (*KeyAssetExport) TableName() string { return "key_asset_exports" }
