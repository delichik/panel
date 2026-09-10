package activity

import (
	"panel/internal/platform/activitylog"
	"time"
)

type EventInput = activitylog.EventInput
type Event = activitylog.Event
type Actor = activitylog.Actor
type Resource = activitylog.Resource
type Receipt = activitylog.Receipt

type Filter struct {
	Q, Domain, Action, Kind, Level, Trigger, ActorID           string
	ResourceType, ResourceID, OperationID, ExecutionID, StepID string
	Phase, Result, Attention, HadError                         string
	From, To                                                   *time.Time
	Cursor                                                     string
	Limit                                                      int
	SnapshotSeq                                                int64
	AfterSeq                                                   int64
}
type Page[T any] struct {
	Items               []T    `json:"items"`
	NextCursor          string `json:"nextCursor,omitempty"`
	HasMore             bool   `json:"hasMore"`
	SnapshotSeq         int64  `json:"snapshotSeq"`
	HeadSeq             int64  `json:"headSeq"`
	ProjectedThroughSeq int64  `json:"projectedThroughSeq"`
	IndexState          string `json:"indexState"`
	Total               int    `json:"total"`
}
type Operation struct {
	OperationID      string     `json:"operationId"`
	Title            string     `json:"title"`
	Domain           string     `json:"domain"`
	Action           string     `json:"action"`
	Trigger          string     `json:"trigger"`
	Actor            Actor      `json:"actor"`
	Resources        []Resource `json:"resources"`
	Phase            string     `json:"phase"`
	Result           string     `json:"result,omitempty"`
	Attention        bool       `json:"attention"`
	Uncertainty      bool       `json:"uncertainty"`
	FailureSummary   string     `json:"failureSummary,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	FirstSeq         int64      `json:"firstSeq"`
	LastSeq          int64      `json:"lastSeq"`
	EventCount       int        `json:"eventCount"`
	AttemptCount     int        `json:"attemptCount"`
	EvidenceComplete bool       `json:"evidenceComplete"`
	HadError         bool       `json:"hadError"`
}
type Execution struct {
	LogicalExecutionID string     `json:"logicalExecutionId"`
	LastSeq            int64      `json:"lastSeq"`
	IsAggregate        bool       `json:"isAggregate"`
	ExecutionID        string     `json:"executionId"`
	RunID              string     `json:"runId,omitempty"`
	Phase              string     `json:"phase"`
	Result             string     `json:"result,omitempty"`
	Resources          []Resource `json:"resources"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	FinishedAt         *time.Time `json:"finishedAt,omitempty"`
}
type Step struct {
	StepID       string     `json:"stepId"`
	ParentStepID string     `json:"parentStepId,omitempty"`
	ExecutionID  string     `json:"executionId"`
	Name         string     `json:"name"`
	Phase        string     `json:"phase"`
	Result       string     `json:"result,omitempty"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}
type Command struct {
	Kind        string `json:"kind"`
	ExecutionID string `json:"executionId"`
	Label       string `json:"label"`
}
type Detail struct {
	Operation         Operation   `json:"operation"`
	Events            []Event     `json:"events"`
	Executions        []Execution `json:"executions"`
	Steps             []Step      `json:"steps"`
	RelatedOperations []string    `json:"relatedOperations"`
	AvailableCommands []Command   `json:"availableCommands"`
	SnapshotSeq       int64       `json:"snapshotSeq"`
	HeadSeq           int64       `json:"headSeq"`
	HasMore           bool        `json:"hasMore"`
	NextCursor        string      `json:"nextCursor,omitempty"`
}
