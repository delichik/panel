package models

// Event 对应 AppDB events：一次过程的索引（状态渠道）；日志正文在 JSONL 文件。
type Event struct {
	ID            string `orm:"primary_key;column:id"`
	Module        string `orm:"not_null"`
	Level         string `orm:"not_null;default:'info'"`
	Origin        string `orm:"not_null;default:'process'"`
	OperationID   string `orm:"not_null;default:'';column:operation_id"`
	ExecutionID   string `orm:"not_null;default:'';column:execution_id"`
	RequestID     string `orm:"not_null;default:'';column:request_id"`
	Title         string `orm:"not_null;default:''"`
	Summary       string `orm:"not_null;default:''"`
	State         string `orm:"not_null;default:'running'"`
	HadError      bool   `orm:"not_null;default:0;column:had_error"`
	ActorJSON     string `orm:"not_null;default:'';column:actor_json"`
	ResourcesJSON string `orm:"not_null;default:'';column:resources_json"`
	LastSourceSeq int64  `orm:"not_null;default:0;column:last_source_seq"`
	LogFile       string `orm:"not_null;default:'';column:log_file"`
	StartedAt     string `orm:"not_null;default:'';column:started_at"`
	UpdatedAt     string `orm:"not_null;default:'';column:updated_at"`
	FinishedAt    string `orm:"not_null;default:'';column:finished_at"`
}

func (*Event) TableName() string { return "events" }

func (*Event) TableConstraints() []string {
	return []string{"CHECK(origin IN ('user','system','process'))"}
}

func (*Event) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"events": {
			"CREATE INDEX IF NOT EXISTS idx_events_operation_id ON events(operation_id)",
			"CREATE INDEX IF NOT EXISTS idx_events_origin_updated_at ON events(origin, updated_at)",
			"CREATE INDEX IF NOT EXISTS idx_events_state_updated_at ON events(state, updated_at)",
		},
	}
}
