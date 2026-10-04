package activitylog

import "time"

type Actor struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}
type Resource struct {
	Type       string         `json:"resourceType"`
	ID         string         `json:"resourceId"`
	Role       string         `json:"role,omitempty"`
	Name       string         `json:"nameSnapshot,omitempty"`
	RevisionID string         `json:"revisionId,omitempty"`
	Generation int            `json:"generation,omitempty"`
	Snapshot   map[string]any `json:"snapshot,omitempty"`
}
type EventInput struct {
	EventID          string         `json:"eventId"`
	EventVersion     int            `json:"eventVersion"`
	EventType        string         `json:"eventType"`
	Kind             string         `json:"kind"`
	Level            string         `json:"level"`
	Domain           string         `json:"domain"`
	Action           string         `json:"action,omitempty"`
	OperationID      string         `json:"operationId,omitempty"`
	RunID            string         `json:"runId,omitempty"`
	ExecutionID      string         `json:"executionId,omitempty"`
	StepID           string         `json:"stepId,omitempty"`
	ParentStepID     string         `json:"parentStepId,omitempty"`
	CausationEventID string         `json:"causationEventId,omitempty"`
	SourceID         string         `json:"sourceId"`
	SourceEpoch      string         `json:"sourceEpoch"`
	SourceStreamID   string         `json:"sourceStreamId"`
	SourceSeq        int64          `json:"sourceSeq"`
	OccurredAt       time.Time      `json:"occurredAt"`
	Actor            Actor          `json:"actor"`
	Initiator        Actor          `json:"initiator"`
	Resources        []Resource     `json:"resources"`
	Trigger          string         `json:"trigger,omitempty"`
	RequestID        string         `json:"requestId,omitempty"`
	Stream           string         `json:"stream,omitempty"`
	MessageCode      string         `json:"messageCode,omitempty"`
	MessageArgs      map[string]any `json:"messageArgs,omitempty"`
	Text             string         `json:"text,omitempty"`
	Data             map[string]any `json:"data"`
}
type Event struct {
	EventInput
	Seq         int64     `json:"seq"`
	RecordedAt  time.Time `json:"recordedAt"`
	ContentHash string    `json:"contentHash"`
}
type Receipt struct {
	Events  []EventReceipt `json:"events"`
	HeadSeq int64          `json:"headSeq"`
}
type EventReceipt struct {
	EventID string `json:"eventId"`
	Seq     int64  `json:"seq"`
}
