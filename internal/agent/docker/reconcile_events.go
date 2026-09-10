package docker

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "panel/internal/agent/contract"
)

var errReconcileOwnership = errors.New("container name is owned by a different resource")

type reconcileRecorder struct {
	ctx         context.Context
	executionID string
	steps       []contract.RuntimeReconcileStep
}

type executionStepKey struct{}

// do persists the intent before entering fn and the actual result afterward.
// Local direct callers without a sink still get measured step timestamps.
func (r *reconcileRecorder) do(name string, fn func(context.Context) error) error {
	started := time.Now().UTC()
	step := contract.RuntimeReconcileStep{StepID: fmt.Sprintf("%s:step:%d", r.executionID, len(r.steps)+1), Name: name, Status: "running", StartedAt: &started}
	r.steps = append(r.steps, step)
	index := len(r.steps) - 1
	sink := contract.ExecutionEventSinkFromContext(r.ctx)
	if sink != nil {
		if err := sink.Append(r.ctx, contract.ExecutionEvent{EventType: "step.started", StepID: step.StepID, StepName: name, Status: "running", OccurredAt: started}); err != nil {
			return &contract.EventPersistenceError{Stage: "step intent", Err: err}
		}
	}
	ctx := context.WithValue(r.ctx, executionStepKey{}, step.StepID)
	runErr := fn(ctx)
	finished := time.Now().UTC()
	r.steps[index].FinishedAt = &finished
	r.steps[index].Status = "succeeded"
	if runErr != nil {
		r.steps[index].Status = "failed"
		r.steps[index].Detail = runErr.Error()
	}
	if sink != nil {
		// A cancelled RPC must not prevent recording an already-observed result.
		if err := sink.Append(context.WithoutCancel(r.ctx), contract.ExecutionEvent{EventType: "step.finished", StepID: step.StepID, StepName: name, Status: r.steps[index].Status, Text: r.steps[index].Detail, OccurredAt: finished}); err != nil {
			return &contract.EventPersistenceError{Stage: "step result", Err: err}
		}
	}
	return runErr
}

// VerifyExecution performs only local/Engine reads. Matching names alone are
// insufficient: apply also checks resource ownership, revision labels and the
// actual managed-file manifest. A mismatch leaves uncertainty intact.
func (r *LocalRuntime) VerifyExecution(ctx context.Context, req contract.RuntimeReconcileRequest) (contract.RuntimeReconcileResponse, bool, error) {
	if r == nil || r.client == nil {
		return contract.RuntimeReconcileResponse{}, false, fmt.Errorf("runtime is not configured")
	}
	name := firstNonEmpty(req.Spec.ContainerName, req.PreviousContainerName, containerNameForInstance(req.InstanceID))
	inspect, err := r.client.inspectContainer(ctx, name)
	result := contract.RuntimeReconcileResponse{ContainerName: name, ObservedAt: time.Now().UTC(), ObservedGeneration: req.DesiredGeneration, ObservedSpecHash: req.DesiredSpecHash}
	if isDockerNotFound(err) {
		result.ObservedState = "missing"
		// Container absence cannot prove persistent application data was removed.
		return result, (req.Action == "stop" || req.Action == "purge") && !req.RemoveData, nil
	}
	if err != nil {
		return result, false, err
	}
	if !managedContainerMatches(inspect, req.ApplicationID, req.InstanceID) {
		return result, false, nil
	}
	result.ContainerID = inspect.ID
	result.ObservedState = dockerStateToRuntime(inspect.State.Status, inspect.State.Running, inspect.State.ExitCode)
	result.ObservedImageDigest = imageDigestFromReference(inspect.Config.Image)
	if req.Action == "stop" {
		return result, !inspect.State.Running, nil
	}
	if req.Action != "apply" || !managedContainerMatchesDesiredRuntime(inspect, req.DesiredSpecHash, req.DesiredGeneration) {
		return result, false, nil
	}
	manifestHash, drift, err := r.managedFilesDrift(req.ApplicationID, req.InstanceID)
	if err != nil {
		return result, false, err
	}
	return result, inspect.State.Running && !drift && manifestHash == inspect.Config.Labels[labelManagedFilesHash], nil
}
