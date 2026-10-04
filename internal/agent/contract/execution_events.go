package contract

import (
	"context"
	"time"
)

// ExecutionEvent is a durable source fact, not a snapshot of mutable Job state.
// Its identity and original timestamp remain unchanged across retransmission.
type ExecutionEvent struct {
	EventID     string    `json:"eventId"`
	SourceSeq   int64     `json:"sourceSeq"`
	OccurredAt  time.Time `json:"occurredAt"`
	EventType   string    `json:"eventType"`
	OperationID string    `json:"operationId"`
	RunID       string    `json:"runId"`
	ExecutionID string    `json:"executionId"`
	StepID      string    `json:"stepId,omitempty"`
	StepName    string    `json:"stepName,omitempty"`
	Status      string    `json:"status,omitempty"`
	Stream      string    `json:"stream,omitempty"`
	Text        string    `json:"text,omitempty"`
	DataJSON    string    `json:"dataJson"`
}

type ExecutionEventsRequest struct {
	ExecutionID    string
	SourceEpoch    string
	SourceStreamID string
	AfterSourceSeq int64
	Limit          int
}

type ExecutionEventsResponse struct {
	SourceID        string
	SourceEpoch     string
	SourceStreamID  string
	Events          []ExecutionEvent
	HeadSeq         int64
	EndSeq          int64
	HasMore         bool
	AckedThroughSeq int64
}

type ExecutionEventsAck struct {
	ExecutionID      string
	SourceID         string
	SourceEpoch      string
	SourceStreamID   string
	ThroughSourceSeq int64
}

// Unknown means a durable intent survived process loss without a durable result.
// It never authorizes repeating the remote side effect.
type ExecutionResult struct {
	State  string
	Result *RuntimeReconcileResponse
	Error  string
	EndSeq int64
}

type ExecutionEventsClient interface {
	ReadExecutionEvents(context.Context, string, ExecutionEventsRequest) (ExecutionEventsResponse, error)
	AckExecutionEvents(context.Context, string, ExecutionEventsAck) error
	GetExecutionResult(context.Context, string, string) (ExecutionResult, error)
}

// The sink returns only after fsync/commit. A failure must stop the executor
// before the next side effect. The observer is local, never serialized in RPC.
type ExecutionEventSink interface {
	Append(context.Context, ExecutionEvent) error
}

type executionEventContextKey struct{}

func WithExecutionEventSink(ctx context.Context, sink ExecutionEventSink) context.Context {
	return context.WithValue(ctx, executionEventContextKey{}, sink)
}
func ExecutionEventSinkFromContext(ctx context.Context) ExecutionEventSink {
	sink, _ := ctx.Value(executionEventContextKey{}).(ExecutionEventSink)
	return sink
}

// EventPersistenceError distinguishes transport-evidence failures from a
// confirmed rejection of the remote action. A step-result write can fail after
// the action has taken effect, so this never authorizes blind retry.
type EventPersistenceError struct {
	Stage string
	Err   error
}

func (e *EventPersistenceError) Error() string { return "persist " + e.Stage + ": " + e.Err.Error() }
func (e *EventPersistenceError) Unwrap() error { return e.Err }

// ExecutionResolution is a human declaration, never an automatic observation.
// Panel binds the actor fields to its authenticated session before forwarding
// over the existing mutually authenticated Agent connection.
type ExecutionResolution struct {
	ExecutionID string `json:"executionId"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason"`
	ActorID     string `json:"actorId"`
	ActorName   string `json:"actorName"`
}
type ExecutionResolutionClient interface {
	ResolveExecution(context.Context, string, ExecutionResolution) (ExecutionResult, error)
}
