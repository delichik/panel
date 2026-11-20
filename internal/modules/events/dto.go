package events

import "panel/internal/platform/notifications"

// EventDTO is the product JSON shape for one events row.
type EventDTO struct {
	ID          string `json:"id"`
	Module      string `json:"module"`
	Level       string `json:"level"`
	Origin      string `json:"origin"`
	OperationID string `json:"operationId,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	State       string `json:"state,omitempty"`
	HadError    bool   `json:"hadError"`
	StartedAt   string `json:"startedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	FinishedAt  string `json:"finishedAt,omitempty"`
}

// ListPage is a cursor page of events.
type ListPage struct {
	Items      []EventDTO `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
	HasMore    bool       `json:"hasMore"`
}

// LinesPage is a cursor page of JSONL log lines.
type LinesPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

// NotificationsPage is the long-poll response for GET /api/v1/notifications
// (ENG-EVT-011). Items is never null.
type NotificationsPage struct {
	Items  []notifications.Notification `json:"items"`
	Cursor uint64                       `json:"cursor"`
}
