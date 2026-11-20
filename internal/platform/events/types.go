package events

// Origin values for events.origin.
const (
	OriginUser    = "user"
	OriginSystem  = "system"
	OriginProcess = "process"
)

// Record is one events row.
type Record struct {
	ID            string
	Module        string
	Level         string
	Origin        string
	OperationID   string
	ExecutionID   string
	RequestID     string
	Title         string
	Summary       string
	State         string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	LogFile       string
	StartedAt     string
	UpdatedAt     string
	FinishedAt    string
}

// ListFilter selects events rows for listing.
type ListFilter struct {
	Origin      string
	Module      string
	State       string
	Level       string
	OperationID string
	ExecutionID string
	HadError    *bool
	From        string
	To          string
	Limit       int
	Offset      int
}

// LineFilter narrows JSONL line reads for an event.
type LineFilter struct {
	Level string
	Limit int
	After int
}
