package tasks

import "time"

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

type Task struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	ServerID   string     `json:"serverId"`
	Status     string     `json:"status"`
	Stage      string     `json:"stage"`
	Percentage *float64   `json:"percentage"`
	Summary    string     `json:"summary"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

type Log struct {
	Cursor int64     `json:"cursor"`
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"`
	Line   string    `json:"line"`
}

type CreateInput struct {
	Type     string
	ServerID string
	Summary  string
}
