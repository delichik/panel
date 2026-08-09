package models

import "time"

// 协调库存量表模型：应用生命周期协调的事实与执行记录。
// 独立于 log 库（事件/任务日志），记录页直接聚合本库数据，不建投影表。

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
// Observed* 列是创建目标那一刻从服务器实例读到的“观测快照”（实际状态），
// 用于记录页展示“期望 vs 实际”。
type ApplicationLifecycleTarget struct {
	ID                 string    `orm:"primary_key"`
	OperationID        string    `orm:"not_null;references:application_lifecycle_operations(id);on_delete:CASCADE"`
	ApplicationID      string    `orm:"not_null"`
	ServerID           string    `orm:"not_null"`
	Action             string    `orm:"not_null;default:'apply'"`
	State              string    `orm:"not_null;default:'planned'"`
	Status             string    `orm:"not_null;default:'pending'"`
	TargetKey          string    `orm:"not_null;default:''"`
	DesiredState       string    `orm:"not_null;default:'running'"`
	DesiredGeneration  int       `orm:"not_null;default:0"`
	DesiredSpecHash    string    `orm:"not_null;default:''"`
	Priority           int       `orm:"not_null;default:0"`
	Attempt            int       `orm:"not_null;default:0"`
	NextRunAt          time.Time `orm:"not_null;default:''"`
	LeaseOwner         string    `orm:"not_null;default:''"`
	LeaseExpiresAt     time.Time `orm:"not_null;default:''"`
	ClaimedTaskID      string    `orm:"not_null;default:''"`
	InstanceID         string    `orm:"not_null;default:''"`
	ContainerName      string    `orm:"not_null;default:''"`
	ContainerID        string    `orm:"not_null;default:''"`
	Stage              string    `orm:"not_null;default:''"`
	Error              string    `orm:"not_null;default:''"`
	ErrorCode          string    `orm:"not_null;default:''"`
	ErrorMessage       string    `orm:"not_null;default:''"`
	ErrorDetail        string    `orm:"not_null;default:''"`
	ObservedState      string    `orm:"not_null;default:''"`
	ObservedExitCode   string    `orm:"not_null;default:''"`
	ObservedError      string    `orm:"not_null;default:''"`
	ObservedGeneration int       `orm:"not_null;default:0"`
	ObservedSpecHash   string    `orm:"not_null;default:''"`
	ObservedImage      string    `orm:"not_null;default:''"`
	ObservedAt         *time.Time
	CreatedAt          time.Time `orm:"not_null"`
	StartedAt          *time.Time
	FinishedAt         *time.Time
	UpdatedAt          time.Time `orm:"not_null"`
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

// ApplicationTargetStage 对应 application_target_stages（目标步骤日志）。
// 每一步一行：阶段、状态、开始/结束时间、详细内容（复制哪些文件、哪个容器等）。
type ApplicationTargetStage struct {
	ID            string `orm:"primary_key"`
	OperationID   string `orm:"not_null;references:application_lifecycle_operations(id);on_delete:CASCADE"`
	TargetID      string `orm:"not_null;references:application_lifecycle_targets(id);on_delete:CASCADE"`
	ApplicationID string `orm:"not_null;default:''"`
	ServerID      string `orm:"not_null;default:''"`
	Stage         string `orm:"not_null"`
	Status        string `orm:"not_null;default:'running'"`
	Detail        string `orm:"not_null;default:''"`
	StartedAt     *time.Time
	FinishedAt    *time.Time
	CreatedAt     time.Time `orm:"not_null"`
	UpdatedAt     time.Time `orm:"not_null"`
}

func (*ApplicationTargetStage) TableName() string { return "application_target_stages" }

// ExtraIndexDDL 返回 application_target_stages 的索引（同目标同阶段唯一，用于幂等 upsert）。
func (*ApplicationTargetStage) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"application_target_stages": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_application_target_stages_target_stage ON application_target_stages(target_id, stage)",
			"CREATE INDEX IF NOT EXISTS idx_application_target_stages_operation ON application_target_stages(operation_id)",
			"CREATE INDEX IF NOT EXISTS idx_application_target_stages_target ON application_target_stages(target_id)",
		},
	}
}
