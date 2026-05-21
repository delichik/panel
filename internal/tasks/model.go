package tasks

import "time"

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

type Task struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	ServerID     string     `json:"serverId"`
	ResourceType string     `json:"resourceType,omitempty"`
	ResourceID   string     `json:"resourceId,omitempty"`
	Status       string     `json:"status"`
	Stage        string     `json:"stage"`
	Percentage   *float64   `json:"percentage"`
	Summary      string     `json:"summary"`
	Error        string     `json:"error,omitempty"`
	RetryCount   int        `json:"retryCount"`
	MaxRetries   int        `json:"maxRetries"`
	NextRunAt    *time.Time `json:"nextRunAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
}

type Log struct {
	Cursor int64     `json:"cursor"`
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"`
	Line   string    `json:"line"`
}

type CreateInput struct {
	Type         string
	ServerID     string
	ResourceType string
	ResourceID   string
	Summary      string
	Status       string
	RetryCount   int
	MaxRetries   int
	NextRunAt    *time.Time
}
