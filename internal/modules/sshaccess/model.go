// Package sshaccess 在安全库（security.db）中保存 SSH 登录会话与失败尝试。
// 会话来自 Agent 的 utmp 快照与 wtmp 回填，失败尝试来自 btmp；两者都按确定性
// 主键幂等写入，Panel 重启或 Agent 重连不会产生重复记录。
package sshaccess

import "time"

// Session 是 API 返回的成功登录会话。
type Session struct {
	ID              string     `json:"id"`
	User            string     `json:"user"`
	TTY             string     `json:"tty"`
	SourceHost      string     `json:"sourceHost"`
	SourceAddress   string     `json:"sourceAddress"`
	Kind            string     `json:"kind"`
	LoginAt         time.Time  `json:"loginAt"`
	LogoutAt        *time.Time `json:"logoutAt,omitempty"`
	LogoutOrigin    string     `json:"logoutOrigin,omitempty"`
	DurationSeconds *int64     `json:"durationSeconds,omitempty"`
	LastSeenAt      *time.Time `json:"lastSeenAt,omitempty"`
}

// Attempt 是 API 返回的一条失败登录尝试。
type Attempt struct {
	ID            string    `json:"id"`
	User          string    `json:"user"`
	SourceHost    string    `json:"sourceHost"`
	SourceAddress string    `json:"sourceAddress"`
	PID           int       `json:"pid"`
	AttemptAt     time.Time `json:"attemptAt"`
}

// SessionFilter 是会话列表的筛选与分页条件。
type SessionFilter struct {
	State    string // open | closed | all
	From     time.Time
	To       time.Time
	User     string
	Source   string
	Query    string
	Page     int
	PageSize int
}

// AttemptFilter 是失败尝试列表的筛选与分页条件。
type AttemptFilter struct {
	From     time.Time
	To       time.Time
	User     string
	Source   string
	Query    string
	Page     int
	PageSize int
}

// SessionPage 是会话列表的分页结果。
type SessionPage struct {
	Items []Session
	Total int
}

// AttemptPage 是失败尝试列表的分页结果。
type AttemptPage struct {
	Items []Attempt
	Total int
}

// storedSession 是仓储读取的数据库行。
type storedSession struct {
	id              string
	serverID        string
	userName        string
	tty             string
	pid             int
	sourceHost      string
	sourceAddress   string
	kind            string
	loginAt         time.Time
	logoutAt        *time.Time
	logoutOrigin    string
	durationSeconds *int64
	lastSeenAt      *time.Time
	origin          string
}
