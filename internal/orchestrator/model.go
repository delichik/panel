package orchestrator

import (
	"encoding/json"
	"time"
)

const (
	ActionApply = "apply"
	ActionStop  = "stop"
	ActionPurge = "purge"

	DesiredRunning = "running"
	DesiredStopped = "stopped"
	DesiredPurged  = "purged"

	JobPending         = "pending"
	JobRunning         = "running"
	JobSucceeded       = "succeeded"
	JobFailedRetryable = "failed_retryable"
	JobFailed          = "failed"
	JobCancelled       = "cancelled"

	ObservedRunning = "running"
	ObservedStopped = "stopped"
	ObservedMissing = "missing"
	ObservedFailed  = "failed"
	ObservedUnknown = "unknown"
)

// ReconcileRequest is the single input shape for all intent and repair
// triggers. The planner persists it into a Job; callers never execute runtime
// mutations directly.
type ReconcileRequest struct {
	ApplicationID       string
	ServerIDs           []string
	ActionOverride      string
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	Reason              string
	Force               bool
	RemoveData          bool
	IdempotencyKey      string
}

// RevisionInput is an immutable, worker-safe runtime snapshot.
type RevisionInput struct {
	ApplicationID       string
	Generation          int
	SpecHash            string
	RenderedRuntimeSpec json.RawMessage
	ManagedFileManifest []map[string]any
	ImageReference      string
	ResolvedImageDigest string
	SpecYAML            string
}

type Revision struct {
	ID                  string
	ApplicationID       string
	Generation          int
	SpecHash            string
	RenderedRuntimeSpec json.RawMessage
	ManagedFileManifest []map[string]any
	ImageReference      string
	ResolvedImageDigest string
	SpecYAML            string
	CreatedAt           time.Time
}

type PlanInput struct {
	ApplicationID       string
	ServerID            string
	InstanceID          string
	Action              string
	DesiredState        string
	DesiredGeneration   int
	DesiredSpecHash     string
	DesiredRevisionID   string
	DesiredSpecJSON     json.RawMessage
	ContainerName       string
	RemoveData          bool
	ForceNonce          int64
	Priority            int
	IntentID            string
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	Reason              string
	IdempotencyKey      string
}

type Job struct {
	ID                  string
	ApplicationID       string
	ServerID            string
	InstanceID          string
	Action              string
	DesiredGeneration   int
	DesiredSpecHash     string
	DesiredRevisionID   string
	DesiredSpecJSON     json.RawMessage
	RemoveData          bool
	ForceNonce          int64
	State               string
	Priority            int
	Attempts            int
	NextRunAt           *time.Time
	LeaseOwner          string
	LeaseToken          string
	LeaseExpiresAt      *time.Time
	ExecutionID         string
	IntentID            string
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	Reason              string
	IdempotencyKey      string
	LastStage           string
	LastSteps           []Step
	ErrorCode           string
	ErrorClass          string
	ErrorMessage        string
	ErrorDetail         string
	CreatedAt           time.Time
	StartedAt           *time.Time
	FinishedAt          *time.Time
	UpdatedAt           time.Time
}

type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ReconcileRequestRPC struct {
	JobID                 string
	ExecutionID           string
	ApplicationID         string
	InstanceID            string
	ServerID              string
	Action                string
	DesiredGeneration     int
	DesiredSpecHash       string
	DesiredRevisionID     string
	DesiredSpecJSON       json.RawMessage
	RenderedRuntimeSpec   json.RawMessage
	RemoveData            bool
	PreviousContainerName string
}

type ReconcileResponse struct {
	ObservedState       string
	ContainerName       string
	ContainerID         string
	ObservedGeneration  int
	ObservedSpecHash    string
	ObservedImageDigest string
	ObservedAt          time.Time
	Steps               []Step
	ErrorCode           string
	ErrorClass          string
	ErrorMessage        string
	ErrorDetail         string
	Retryable           bool
	RetryAfter          time.Duration
}

type Observation struct {
	InstanceID          string
	Source              string
	Sequence            int64
	ObservedAt          time.Time
	ObservedState       string
	ContainerName       string
	ContainerID         string
	ObservedGeneration  int
	ObservedSpecHash    string
	ObservedImageDigest string
	LastErrorCode       string
	LastErrorClass      string
	LastErrorMessage    string
	LastErrorDetail     string
	JobID               string
	LeaseToken          string
	DesiredSpecJSON     json.RawMessage
}

type PlanResult struct {
	Job     Job
	Created bool
	Merged  bool
}

type WriteResult struct {
	Accepted bool
}
