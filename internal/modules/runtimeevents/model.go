package runtimeevents

import (
	"context"
	"time"
)

const (
	CategoryApplication = "application"
	CategoryTask        = "task"
	CategoryAlert       = "alert"
	CategoryLog         = "log"
	CategoryRuntime     = "runtime"
	CategorySystem      = "system"

	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"

	EventApplicationOperationCreated   = "application.operation.created"
	EventApplicationOperationCompleted = "application.operation.completed"
	EventApplicationOperationFailed    = "application.operation.failed"
	EventTaskCreated                   = "task.created"
	EventTaskStarted                   = "task.started"
	EventTaskCompleted                 = "task.completed"
	EventTaskFailed                    = "task.failed"
	EventTaskRetried                   = "task.retried"
	EventTaskCancelled                 = "task.cancelled"
	EventAgentConnected                = "agent.connected"
	EventAgentDisconnected             = "agent.disconnected"
)

// WriteEventInput 是系统日志的一条待写入记录。系统日志只保留简单可读字段，
// 不承载 payload、日志/任务/目标引用或关联对象。
type WriteEventInput struct {
	ID           string
	EventType    string
	Category     string
	Severity     string
	Source       string
	SourceModule string
	DedupeKey    string
	Summary      string
	OccurredAt   time.Time
}

// Event 是系统日志对外返回的一条记录。
type Event struct {
	ID           string    `json:"id"`
	EventType    string    `json:"eventType"`
	Category     string    `json:"category"`
	Severity     string    `json:"severity"`
	Source       string    `json:"source,omitempty"`
	SourceModule string    `json:"sourceModule,omitempty"`
	Summary      string    `json:"summary"`
	OccurredAt   time.Time `json:"occurredAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

// EventWriter 是系统日志的专用写入接口。实现必须尽量非阻塞、可丢弃，
// 由 BufferedWriter 提供后台批量落库；Service.Log 提供同步直写便于测试。
type EventWriter interface {
	Log(ctx context.Context, in WriteEventInput)
}

type ListFilter struct {
	Category  string
	Source    string
	Severity  string
	EventType string
	From      *time.Time
	To        *time.Time
	Limit     int
	Offset    int
}

type ListResult[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type CleanupResult struct {
	EventsDeleted int
}