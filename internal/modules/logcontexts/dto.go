package logcontexts

// ContextDTO is the product JSON shape for one log_contexts row.
type ContextDTO struct {
	StreamID    string `json:"streamId"`
	Module      string `json:"module"`
	Origin      string `json:"origin"`
	OperationID string `json:"operationId,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
	Title       string `json:"title,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Result      string `json:"result,omitempty"`
	HadError    bool   `json:"hadError"`
	StartedAt   string `json:"startedAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	FinishedAt  string `json:"finishedAt,omitempty"`
}

// ListPage is a cursor page of log contexts.
type ListPage struct {
	Items      []ContextDTO `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
	HasMore    bool         `json:"hasMore"`
}

// LinesPage is a cursor page of JSONL log lines for one stream.
type LinesPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}
