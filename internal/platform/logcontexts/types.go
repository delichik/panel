package logcontexts

// Origin values for log_contexts.origin.
const (
	OriginUser    = "user"
	OriginSystem  = "system"
	OriginProcess = "process"
)

// Record is one log_contexts row.
type Record struct {
	StreamID      string
	Module        string
	Origin        string
	OperationID   string
	ExecutionID   string
	RequestID     string
	Title         string
	Summary       string
	Result        string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	LogFile       string
	StartedAt     string
	UpdatedAt     string
	FinishedAt    string
}

// ListFilter selects log_contexts rows for listing.
type ListFilter struct {
	Origin      string
	Module      string
	OperationID string
	ExecutionID string
	HadError    *bool
	// From/To bound updated_at (RFC3339 / RFC3339Nano strings; inclusive).
	From   string
	To     string
	Limit  int
	Offset int
}

// LineFilter narrows JSONL line reads for a stream.
type LineFilter struct {
	Level string
	Limit int
	// After is a non-negative line offset into the filtered result (0-based).
	After int
}
