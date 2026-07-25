package runtimeevents

import "time"

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

	EventApplicationOperationCreated         = "application.operation.created"
	EventApplicationOperationTargetQueued    = "application.operation.target.queued"
	EventApplicationOperationTargetStarted   = "application.operation.target.started"
	EventApplicationOperationTargetSucceeded = "application.operation.target.succeeded"
	EventApplicationOperationTargetFailed    = "application.operation.target.failed"
	EventApplicationOperationCompleted       = "application.operation.completed"
	EventApplicationOperationFailed          = "application.operation.failed"
	EventTaskCreated                         = "task.created"
	EventTaskStarted                         = "task.started"
	EventTaskCompleted                       = "task.completed"
	EventTaskFailed                          = "task.failed"
	EventTaskRetried                         = "task.retried"
	EventTaskCancelled                       = "task.cancelled"
	EventLogAttached                         = "log.attached"
	EventDetailPruned                        = "event.detail.pruned"
)

type WriteEventInput struct {
	ID           string
	EventType    string
	Category     string
	SubjectType  string
	SubjectID    string
	OperationID  string
	Severity     string
	Source       string
	SourceModule string
	SourceType   string
	SourceID     string
	DedupeKey    string
	Summary      string
	OccurredAt   time.Time
	Detail       *EventDetailInput
	Application  *ApplicationOperationInput
}

type EventDetailInput struct {
	PayloadJSON    string
	Error          string
	LogRefsJSON    string
	TaskRefsJSON   string
	TargetRefsJSON string
}

type ApplicationOperationInput struct {
	ApplicationID           string
	ApplicationNameSnapshot string
	Action                  string
	Source                  string
	TriggeredBy             string
	TriggerReason           string
	Status                  string
	StartedAt               *time.Time
	FinishedAt              *time.Time
	TargetTotal             int
	TargetSucceeded         int
	TargetFailed            int
	FailureSummary          string
}

type Event struct {
	ID              string     `json:"id"`
	EventType       string     `json:"eventType"`
	Category        string     `json:"category"`
	SubjectType     string     `json:"subjectType,omitempty"`
	SubjectID       string     `json:"subjectId,omitempty"`
	OperationID     string     `json:"operationId,omitempty"`
	Severity        string     `json:"severity"`
	Source          string     `json:"source,omitempty"`
	SourceModule    string     `json:"sourceModule,omitempty"`
	SourceType      string     `json:"sourceType,omitempty"`
	SourceID        string     `json:"sourceId,omitempty"`
	Summary         string     `json:"summary"`
	OccurredAt      time.Time  `json:"occurredAt"`
	DetailAvailable bool       `json:"detailAvailable"`
	DetailPrunedAt  *time.Time `json:"detailPrunedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type EventDetail struct {
	Event
	PayloadJSON    string `json:"payloadJson"`
	Error          string `json:"error,omitempty"`
	LogRefsJSON    string `json:"logRefsJson"`
	TaskRefsJSON   string `json:"taskRefsJson"`
	TargetRefsJSON string `json:"targetRefsJson"`
}

type ApplicationOperationDetail struct {
	Operation OperationRecord `json:"operation"`
	Events    []Event         `json:"events"`
	Targets   []any           `json:"targets"`
}

type SystemEventDetail struct {
	Event      Event `json:"event"`
	Payload    any   `json:"payload"`
	LogRefs    []any `json:"logRefs"`
	TaskRefs   []any `json:"taskRefs"`
	TargetRefs []any `json:"targetRefs"`
}

type OperationRecord struct {
	OperationID             string     `json:"operationId"`
	ApplicationID           string     `json:"applicationId"`
	ApplicationNameSnapshot string     `json:"applicationNameSnapshot"`
	Action                  string     `json:"action"`
	Source                  string     `json:"source"`
	TriggeredBy             string     `json:"triggeredBy,omitempty"`
	TriggerReason           string     `json:"triggerReason,omitempty"`
	Status                  string     `json:"status"`
	StartedAt               *time.Time `json:"startedAt,omitempty"`
	FinishedAt              *time.Time `json:"finishedAt,omitempty"`
	TargetTotal             int        `json:"targetTotal"`
	TargetSucceeded         int        `json:"targetSucceeded"`
	TargetFailed            int        `json:"targetFailed"`
	LatestEventAt           time.Time  `json:"latestEventAt"`
	DetailAvailable         bool       `json:"detailAvailable"`
	FailureSummary          string     `json:"failureSummary,omitempty"`
	CreatedAt               time.Time  `json:"createdAt"`
	UpdatedAt               time.Time  `json:"updatedAt"`
}

type ListFilter struct {
	ApplicationID string
	Action        string
	Category      string
	SubjectType   string
	SubjectID     string
	Source        string
	Status        string
	Severity      string
	EventType     string
	From          *time.Time
	To            *time.Time
	Limit         int
	Offset        int
}

type ListResult[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type CleanupResult struct {
	DetailsPruned     int
	EventsDeleted     int
	OperationsDeleted int
}
