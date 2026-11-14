package orchestrator

import (
	"encoding/json"
	"strings"
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
	Automatic           bool
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

// Job is the deploy Run domain type. Rows are stored in event_runs with
// type=application_deploy. State keeps pending/succeeded; the table uses
// queued/completed. attempts is the deploy claim count and is not retry_count.
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

// Deploy progress stages projected from Agent execution step events. When a
// running+uncertainty Job has one of these as LastStage, UI treats it as normal
// converging progress rather than result_unknown attention.
var deployProgressStages = map[string]struct{}{
	"write_files":               {},
	"ensure_image":              {},
	"inspect_container":         {},
	"create_container":          {},
	"start_container":           {},
	"verify_running":            {},
	"replace_stop":              {},
	"replace_remove":            {},
	"cleanup_legacy_workspace":  {},
	"stop":                      {},
	"purge":                     {},
	"apply":                     {},
}

// IsDeployProgressStage reports whether stage is an in-flight Agent ensure step.
func IsDeployProgressStage(stage string) bool {
	_, ok := deployProgressStages[strings.TrimSpace(stage)]
	return ok
}

// UncertaintyNeedsAttention is true when monitor-mode fencing has no projected
// progress yet (or evidence is exhausted). Fresh step stages mean normal deploy.
func UncertaintyNeedsAttention(errorClass, lastStage string) bool {
	if strings.TrimSpace(errorClass) != "uncertainty" {
		return false
	}
	return !IsDeployProgressStage(lastStage)
}

// ApplyReadyToSucceed enforces ORCH-CTRL-007 when a verify_running step is in
// the trail: apply must not succeed until that step succeeded. Empty trails or
// stubs without verify_running (scripted agents / observation-only) stay allowed.
func ApplyReadyToSucceed(action string, steps []Step) bool {
	if action != ActionApply || len(steps) == 0 {
		return true
	}
	sawVerify := false
	for _, step := range steps {
		if step.Name != "verify_running" {
			continue
		}
		sawVerify = true
		if step.Status == "succeeded" {
			return true
		}
	}
	return !sawVerify
}

type ReconcileRequestRPC struct {
	OperationID           string
	RunID                 string
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
