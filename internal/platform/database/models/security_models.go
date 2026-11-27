package models

import "time"

// 安全日志库（security.db）模型。SSH 访问记录独立于业务库，避免高频登录
// 尝试写入污染 app 库，也便于按安全保留期单独清理。
//
// 会话与失败尝试都逐条保存：失败尝试保留精确时间、来源地址与 pid，不做
// 分钟聚合（产品要求来源和时间可追溯）。幂等键由 Agent 侧生成：
//   - 会话：sha256(server|user|tty|pid|login_unix)
//   - 尝试：sha256(server|user|host|addr|attempt_unix|pid)

// SSHAccessSession 对应 ssh_access_sessions（成功登录会话，记录上下线）。
type SSHAccessSession struct {
	ID            string    `orm:"primary_key"`
	ServerID      string    `orm:"not_null"`
	UserName      string    `orm:"not_null"`
	TTY           string    `orm:"not_null;default:''"`
	PID           int       `orm:"not_null;default:0"`
	SourceHost    string    `orm:"not_null;default:''"`
	SourceAddress string    `orm:"not_null;default:''"`
	Kind          string    `orm:"not_null;default:'ssh'"`
	LoginAt       time.Time `orm:"not_null"`
	LogoutAt      *time.Time
	// LogoutOrigin: wtmp（精确配对）/ live（utmp 消失时按最近采样推断）/ 空。
	LogoutOrigin string `orm:"not_null;default:''"`
	// DurationSeconds 在 logout_at 与 login_at 都有值时计算，否则为 NULL。
	DurationSeconds *int64
	// LastSeenAt 是最近一次出现在 Agent 快照中的时间；NULL 表示仅由回填得到。
	LastSeenAt *time.Time
	// Origin: live（实时上报）/ backfill（wtmp 回填）。
	Origin    string    `orm:"not_null;default:'live'"`
	CreatedAt time.Time `orm:"not_null"`
	UpdatedAt time.Time `orm:"not_null"`
}

func (*SSHAccessSession) TableName() string { return "ssh_access_sessions" }

// ExtraIndexDDL 返回会话表无法用 orm tag 表达的复合/部分索引：
//   - 列表按服务器与登录时间过滤；
//   - 在线会话扫描只看 logout_at IS NULL。
func (*SSHAccessSession) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"ssh_access_sessions": {
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_sessions_server_login ON ssh_access_sessions(server_id, login_at)",
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_sessions_open_login ON ssh_access_sessions(server_id, login_at) WHERE logout_at IS NULL",
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_sessions_server_logout ON ssh_access_sessions(server_id, logout_at)",
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_sessions_logout ON ssh_access_sessions(logout_at)",
		},
	}
}

// SSHAccessAttempt 对应 ssh_access_attempts（逐条失败登录尝试）。
type SSHAccessAttempt struct {
	ID            string    `orm:"primary_key"`
	ServerID      string    `orm:"not_null"`
	UserName      string    `orm:"not_null"`
	SourceHost    string    `orm:"not_null;default:''"`
	SourceAddress string    `orm:"not_null;default:''"`
	PID           int       `orm:"not_null;default:0"`
	AttemptAt     time.Time `orm:"not_null"`
	CreatedAt     time.Time `orm:"not_null"`
	UpdatedAt     time.Time `orm:"not_null"`
}

func (*SSHAccessAttempt) TableName() string { return "ssh_access_attempts" }

// ExtraIndexDDL 返回失败尝试表无法用 orm tag 表达的复合索引：列表按服务器与
// 发生时间分页，来源过滤使用 (server_id, source_address)。
func (*SSHAccessAttempt) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"ssh_access_attempts": {
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_attempts_server_time ON ssh_access_attempts(server_id, attempt_at)",
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_attempts_server_addr ON ssh_access_attempts(server_id, source_address, attempt_at)",
			"CREATE INDEX IF NOT EXISTS idx_ssh_access_attempts_time ON ssh_access_attempts(attempt_at)",
		},
	}
}
