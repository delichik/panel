package tasks

import (
	"context"
	"time"
)

const (
	StatusQueued          = "queued"
	StatusScheduled       = "scheduled"
	StatusRunning         = "running"
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusFailedRetryable = "failed_retryable"
	StatusBlocked         = "blocked"
	StatusCancelled       = "cancelled"
)

type RunningExecution struct {
	TaskID  string
	Context context.Context
	Cancel  context.CancelFunc
}

type Task struct {
	ID                  string     `json:"id"`
	OperationID         string     `json:"operationId"`
	Type                string     `json:"type"`
	ParentTaskID        string     `json:"parentTaskId,omitempty"`
	ChildIndex          int        `json:"childIndex,omitempty"`
	ChildCount          int        `json:"childCount,omitempty"`
	ExecutionMode       string     `json:"executionMode,omitempty"`
	ConcurrencyKey      string     `json:"concurrencyKey,omitempty"`
	ScheduleKey         string     `json:"scheduleKey,omitempty"`
	ServerID            string     `json:"serverId"`
	NodeID              string     `json:"nodeId"`
	ResourceType        string     `json:"resourceType,omitempty"`
	ResourceID          string     `json:"resourceId,omitempty"`
	TriggerType         string     `json:"triggerType,omitempty"`
	TriggerResourceType string     `json:"triggerResourceType,omitempty"`
	TriggerResourceID   string     `json:"triggerResourceId,omitempty"`
	TriggerTaskID       string     `json:"triggerTaskId,omitempty"`
	TriggeredBy         string     `json:"triggeredBy,omitempty"`
	ParamsJSON          string     `json:"paramsJson,omitempty"`
	MetadataJSON        string     `json:"metadataJson,omitempty"`
	Status              string     `json:"status"`
	Stage               string     `json:"stage"`
	Percentage          *float64   `json:"percentage"`
	Summary             string     `json:"summary"`
	Error               string     `json:"error,omitempty"`
	RetryCount          int        `json:"retryCount"`
	MaxRetries          int        `json:"maxRetries"`
	NextRunAt           *time.Time `json:"nextRunAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	StartedAt           *time.Time `json:"startedAt"`
	FinishedAt          *time.Time `json:"finishedAt"`
	AllowRunNow         bool       `json:"allowRunNow"`
	AllowRetry          bool       `json:"allowRetry"`
	AllowCancel         bool       `json:"allowCancel"`
}

type Log struct {
	Cursor int64     `json:"cursor"`
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"`
	Line   string    `json:"line"`
}

type CreateInput struct {
	OperationID         string
	Type                string
	ParentTaskID        string
	ChildIndex          int
	ChildCount          int
	ExecutionMode       string
	ConcurrencyKey      string
	ScheduleKey         string
	ServerID            string
	NodeID              string
	ResourceType        string
	ResourceID          string
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	TriggerTaskID       string
	TriggeredBy         string
	ParamsJSON          string
	MetadataJSON        string
	Summary             string
	Status              string
	RetryCount          int
	MaxRetries          int
	NextRunAt           *time.Time
}

type CreateBatchInput struct {
	Type          string
	Summary       string
	OperationID   string
	TriggerType   string
	TriggeredBy   string
	ExecutionMode string
	ForceParent   bool
	Inputs        []CreateInput
}

type Step struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"taskId"`
	Step         string     `json:"step"`
	Status       string     `json:"status"`
	Percentage   float64    `json:"percentage"`
	MetadataJSON string     `json:"metadataJson"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	Error        string     `json:"error,omitempty"`
}

type StepInput struct {
	Step         string
	Status       string
	Percentage   float64
	MetadataJSON string
	Error        string
}
