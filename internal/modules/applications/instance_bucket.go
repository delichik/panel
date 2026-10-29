package applications

import (
	"strings"

	appruntime "panel/internal/modules/applications/runtime"
	controlplane "panel/internal/orchestrator"
)

const (
	InstanceBucketDeploying = "deploying"
	InstanceBucketAbnormal  = "abnormal"
	InstanceBucketHealthy   = "healthy"
	InstanceBucketOmitted   = "omitted"
)

// InstanceBucketCounts holds app-level node bucket tallies. The three displayed
// buckets may sum to less than InstanceCount when stop/purge targets have
// already converged (omitted).
type InstanceBucketCounts struct {
	Healthy   int `json:"healthyInstanceCount"`
	Abnormal  int `json:"abnormalInstanceCount"`
	Deploying int `json:"deployingInstanceCount"`
}

type instanceClassifyInput struct {
	DesiredState       string
	ObservedStatus     string
	DesiredGeneration  int
	ObservedGeneration int
	DesiredSpecHash    string
	ObservedSpecHash   string
	HasActiveJob       bool
}

func classifyInstanceBucket(in instanceClassifyInput) string {
	if in.HasActiveJob {
		return InstanceBucketDeploying
	}
	desired := strings.TrimSpace(in.DesiredState)
	if desired == "" {
		desired = appruntime.DesiredRunning
	}
	observed := strings.TrimSpace(in.ObservedStatus)
	if observed == "" {
		observed = appruntime.StatusUnknown
	}

	switch desired {
	case appruntime.DesiredRunning:
		if observed == appruntime.StatusRunning && generationAndHashMatch(in) {
			return InstanceBucketHealthy
		}
		return InstanceBucketAbnormal
	case appruntime.DesiredStopped:
		if observed == appruntime.StatusStopped || observed == appruntime.StatusMissing {
			return InstanceBucketOmitted
		}
		return InstanceBucketAbnormal
	case controlplane.DesiredPurged:
		// A remaining instance row after purge means the target is not gone yet.
		return InstanceBucketAbnormal
	default:
		return InstanceBucketAbnormal
	}
}

func generationAndHashMatch(in instanceClassifyInput) bool {
	if in.DesiredGeneration > 0 && in.ObservedGeneration != in.DesiredGeneration {
		return false
	}
	desiredHash := strings.TrimSpace(in.DesiredSpecHash)
	if desiredHash == "" {
		return true
	}
	return strings.TrimSpace(in.ObservedSpecHash) == desiredHash
}

func accumulateBucket(counts *InstanceBucketCounts, bucket string) {
	switch bucket {
	case InstanceBucketDeploying:
		counts.Deploying++
	case InstanceBucketAbnormal:
		counts.Abnormal++
	case InstanceBucketHealthy:
		counts.Healthy++
	}
}

func isActiveDeployJobStatus(status string) bool {
	switch deployStatusToJobState(status) {
	case controlplane.JobPending, controlplane.JobRunning, controlplane.JobFailedRetryable:
		return true
	default:
		return false
	}
}
