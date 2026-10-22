package models

// LogContext 对应 AppDB log_contexts：统一进程日志流的索引元数据。
// 时间列存 RFC3339 文本，便于跨进程只读扫描与迁移。
type LogContext struct {
	StreamID      string `orm:"primary_key;column:stream_id"`
	Module        string `orm:"not_null"`
	Origin        string `orm:"not_null;default:'process'"`
	OperationID   string `orm:"not_null;default:'';column:operation_id"`
	ExecutionID   string `orm:"not_null;default:'';column:execution_id"`
	RequestID     string `orm:"not_null;default:'';column:request_id"`
	Title         string `orm:"not_null;default:''"`
	Summary       string `orm:"not_null;default:''"`
	Result        string `orm:"not_null;default:''"`
	HadError      bool   `orm:"not_null;default:0;column:had_error"`
	ActorJSON     string `orm:"not_null;default:'';column:actor_json"`
	ResourcesJSON string `orm:"not_null;default:'';column:resources_json"`
	LastSourceSeq int64  `orm:"not_null;default:0;column:last_source_seq"`
	LogFile       string `orm:"not_null;default:'';column:log_file"`
	StartedAt     string `orm:"not_null;default:'';column:started_at"`
	UpdatedAt     string `orm:"not_null;default:'';column:updated_at"`
	FinishedAt    string `orm:"not_null;default:'';column:finished_at"`
}

func (*LogContext) TableName() string { return "log_contexts" }

// TableConstraints 限制 origin 枚举。
func (*LogContext) TableConstraints() []string {
	return []string{"CHECK(origin IN ('user','system','process'))"}
}

// ExtraIndexDDL 返回 log_contexts 表无法用 orm tag 表达的复合/单列索引。
func (*LogContext) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"log_contexts": {
			"CREATE INDEX IF NOT EXISTS idx_log_contexts_operation_id ON log_contexts(operation_id)",
			"CREATE INDEX IF NOT EXISTS idx_log_contexts_origin_updated_at ON log_contexts(origin, updated_at)",
		},
	}
}
