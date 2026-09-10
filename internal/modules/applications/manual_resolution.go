package applications

import (
	"context"
	"strings"

	agentcontract "panel/internal/agent/contract"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
)

// ResolveExecutionManually accepts an authenticated declaration only for the
// still-fenced unknown execution. The Agent durably releases its matching
// execution fence before Panel can release the corresponding Job conflict.
func (s *Service) ResolveExecutionManually(ctx context.Context, executionID, outcome, reason string) (any, error) {
	actor := activitylog.ActorFromContext(ctx)
	if actor.Kind != "user" || actor.ID == "" {
		return nil, panelerr.Forbidden("execution_verification_requires_user", "An authenticated user must verify this execution")
	}
	reason = activitylog.Redact(strings.TrimSpace(reason))
	if reason == "" || (outcome != "succeeded" && outcome != "failed") {
		return nil, panelerr.Validation("execution_verification_invalid", "A verification reason and a succeeded or failed outcome are required")
	}
	store := controlplane.NewStore(s.db)
	job, err := store.GetJobByExecutionID(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if job.State != controlplane.JobRunning || job.ErrorClass != "uncertainty" {
		return nil, panelerr.Conflict("execution_not_uncertain", "Only an execution awaiting verification can be resolved")
	}
	client, ok := s.runtimeClient.(agentcontract.ExecutionResolutionClient)
	if !ok {
		return nil, panelerr.Conflict("execution_resolution_unsupported", "The Agent must support manual execution verification")
	}
	if s.servers == nil {
		return nil, panelerr.Conflict("execution_resolution_unavailable", "The execution server is unavailable")
	}
	srv, err := s.servers.Get(ctx, job.ServerID)
	if err != nil {
		return nil, err
	}
	endpoint, ok := agentURLFromServer(srv)
	if !ok {
		return nil, panelerr.Conflict("execution_resolution_unavailable", "The execution server is unavailable")
	}
	// This is a request to verify, not an already verified outcome. If the RPC
	// or event transfer fails, the Job remains unknown and cannot be reissued.
	_, err = activitylog.Append(ctx, s.db, []activitylog.EventInput{{EventType: "verification.requested", Kind: "request", Domain: "application", Action: job.Action, OperationID: job.IntentID, RunID: job.IntentID, ExecutionID: job.ExecutionID, Actor: actor, Text: reason, Data: map[string]any{"jobId": job.ID, "verificationSource": "manual", "declaredOutcome": outcome, "phase": "waiting", "uncertainty": true}}})
	if err != nil {
		return nil, err
	}
	result, err := client.ResolveExecution(ctx, endpoint, agentcontract.ExecutionResolution{ExecutionID: executionID, Outcome: outcome, Reason: reason, ActorID: actor.ID, ActorName: actor.Name})
	if err != nil {
		return nil, err
	}
	if result.State != "finished" || result.Result == nil || result.Result.VerificationSource != "manual" || result.Result.VerifiedBy != actor.ID || result.Result.VerificationReason != reason {
		return nil, panelerr.Conflict("execution_resolution_unconfirmed", "The Agent did not confirm the same manual declaration")
	}
	remoteOutcome := manualOutcome(*result.Result)
	if remoteOutcome != outcome {
		return nil, panelerr.Conflict("execution_resolution_unconfirmed", "The Agent did not confirm the same manual declaration")
	}
	reconciler := &serviceRuntimeReconciler{service: s}
	endSeq, err := reconciler.collectExecutionEvents(ctx, endpoint, requestForJob(job))
	if err != nil {
		return nil, err
	}
	if endSeq == 0 || endSeq != result.EndSeq {
		return nil, panelerr.Conflict("execution_resolution_unconfirmed", "The Agent did not confirm the same manual declaration")
	}
	if err := applyManualResult(ctx, store, job, *result.Result); err != nil {
		return nil, err
	}
	return map[string]any{"operationId": job.IntentID, "executionId": job.ExecutionID, "result": outcome, "verificationSource": "manual"}, nil
}

func requestForJob(job controlplane.Job) controlplane.ReconcileRequestRPC {
	return controlplane.ReconcileRequestRPC{JobID: job.ID, ExecutionID: job.ExecutionID, OperationID: job.IntentID, RunID: job.IntentID, ApplicationID: job.ApplicationID, ServerID: job.ServerID, InstanceID: job.InstanceID, Action: job.Action, DesiredGeneration: job.DesiredGeneration, DesiredRevisionID: job.DesiredRevisionID, DesiredSpecHash: job.DesiredSpecHash}
}
func manualOutcome(result agentcontract.RuntimeReconcileResponse) string {
	if result.ErrorCode != "" || result.ErrorMessage != "" {
		return "failed"
	}
	return "succeeded"
}
func applyManualResult(ctx context.Context, store *controlplane.Store, job controlplane.Job, result agentcontract.RuntimeReconcileResponse) error {
	actor := activitylog.Actor{Kind: "user", ID: result.VerifiedBy, Name: result.VerifiedByName}
	outcome := manualOutcome(result)
	_, err := store.ResolveUncertainManually(ctx, job, outcome, result.VerificationReason, actor)
	if err == nil {
		return nil
	}
	// The recovery poll may have committed the identical declaration while the
	// HTTP caller was receiving its ACK. Only the matching durable declaration
	// makes this race idempotent; a changed result/version is never accepted.
	current, getErr := store.GetJob(ctx, job.ID)
	if getErr != nil || current.ExecutionID != job.ExecutionID || current.State != outcome {
		return err
	}
	var n int
	if queryErr := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM activity_events WHERE execution_id=? AND event_type='execution.verified' AND actor_id=? AND json_extract(data_json,'$.verificationSource')='manual' AND json_extract(data_json,'$.outcome')=? AND json_extract(data_json,'$.reason')=?`, job.ExecutionID, actor.ID, outcome, result.VerificationReason).Scan(&n); queryErr != nil {
		return queryErr
	}
	if n == 1 {
		return nil
	}
	return err
}
