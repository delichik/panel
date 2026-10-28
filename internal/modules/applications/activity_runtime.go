package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	agentcontract "panel/internal/agent/contract"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/activitylog"
	"panel/internal/platform/events"
	"panel/internal/platform/logging"
)

type executionProgress struct {
	EndSeq int64
	Steps  []controlplane.Step
	Stage  string
}

// collectExecutionEvents returns a nonzero final source sequence only after the
// complete closed stream has committed to the JSONL file + events index.
// Source identity is bound to the trusted server/execution.
func (r *serviceRuntimeReconciler) collectExecutionEvents(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC) (int64, error) {
	progress, err := r.collectExecutionProgress(ctx, endpoint, req)
	return progress.EndSeq, err
}

// collectExecutionProgress pulls durable Agent step events, projects them onto
// the running Job (last_stage / last_steps_json), and returns stream watermarks.
func (r *serviceRuntimeReconciler) collectExecutionProgress(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC) (executionProgress, error) {
	client, ok := r.service.runtimeClient.(agentcontract.ExecutionEventsClient)
	if !ok {
		return executionProgress{}, errors.New("agent does not support durable execution events")
	}
	ctxStore := events.NewStore(r.service.db)
	var after int64
	var epoch, stream string
	if rec, err := ctxStore.Get(ctx, req.ExecutionID); err == nil {
		after = rec.LastSourceSeq
		if rec.FinishedAt != "" && after > 0 {
			return executionProgress{EndSeq: after}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return executionProgress{}, err
	}
	resources := []activitylog.Resource{{Type: "application", ID: req.ApplicationID, Role: "subject", RevisionID: req.DesiredRevisionID, Generation: req.DesiredGeneration}, {Type: "server", ID: req.ServerID, Role: "target"}}
	initiator := activitylog.Actor{}
	action := req.Action
	trigger := ""
	operationID, runID := req.OperationID, req.RunID
	if existing, err := ctxStore.Get(ctx, req.ExecutionID); err == nil {
		if existing.OperationID != "" {
			operationID = existing.OperationID
		}
		if existing.RequestID != "" {
			runID = existing.RequestID
		}
		if existing.ActorJSON != "" {
			_ = json.Unmarshal([]byte(existing.ActorJSON), &initiator)
		}
		if existing.ResourcesJSON != "" {
			_ = json.Unmarshal([]byte(existing.ResourcesJSON), &resources)
		}
		if existing.Title != "" {
			action = firstNonEmpty(action, existing.Title)
		}
	}
	logCtx := logging.Ensure(ctx, logging.Context{
		StreamID:    req.ExecutionID,
		Module:      "agent-evidence",
		OperationID: operationID,
		ExecutionID: req.ExecutionID,
		RequestID:   runID,
	})
	_ = logging.RecordContext(logCtx, logging.ContextMeta{
		Origin:        events.OriginProcess,
		Title:         action,
		Summary:       "execution evidence",
		ActorJSON:     logging.MarshalActorJSON(map[string]string{"kind": initiator.Kind, "id": initiator.ID, "name": initiator.Name}),
		ResourcesJSON: logging.MarshalResourcesJSON(resources),
	})
	stepsByID := map[string]controlplane.Step{}
	stepOrder := make([]string, 0, 8)
	sawStep := false
	for {
		page, err := client.ReadExecutionEvents(ctx, endpoint, agentcontract.ExecutionEventsRequest{ExecutionID: req.ExecutionID, SourceEpoch: epoch, SourceStreamID: stream, AfterSourceSeq: after, Limit: 100})
		if err != nil {
			return executionProgress{}, err
		}
		if page.SourceID == "" || page.SourceEpoch == "" || page.SourceStreamID != req.ExecutionID || (epoch != "" && epoch != page.SourceEpoch) || (stream != "" && stream != page.SourceStreamID) {
			return executionProgress{}, errors.New("agent returned a different execution source")
		}
		epoch, stream = page.SourceEpoch, page.SourceStreamID
		if page.HeadSeq < after || page.EndSeq < 0 || page.EndSeq > page.HeadSeq || (page.EndSeq > 0 && page.EndSeq != page.HeadSeq) {
			return executionProgress{}, errors.New("invalid execution stream watermark")
		}
		if page.AckedThroughSeq > after {
			if err := r.recordExecutionGap(logCtx, req, operationID, resources, page.SourceEpoch, after+1, page.AckedThroughSeq); err != nil {
				return executionProgress{}, err
			}
			return executionProgress{}, fmt.Errorf("execution evidence gap: source acknowledged through %d, panel has %d", page.AckedThroughSeq, after)
		}
		if len(page.Events) > 100 {
			return executionProgress{}, errors.New("agent event batch exceeds requested limit")
		}
		closed := false
		for _, remote := range page.Events {
			wantSeq := after + 1
			switch {
			case remote.EventID == "":
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: empty event id (wantSeq=%d)", wantSeq)
			case remote.ExecutionID != req.ExecutionID:
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: executionId got %q want %q", remote.ExecutionID, req.ExecutionID)
			case remote.OperationID != operationID:
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: operationId got %q want %q", remote.OperationID, operationID)
			case remote.RunID != runID:
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: runId got %q want %q", remote.RunID, runID)
			case remote.SourceSeq != wantSeq:
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: sourceSeq got %d want %d", remote.SourceSeq, wantSeq)
			case remote.SourceSeq > page.HeadSeq:
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: sourceSeq %d > head %d", remote.SourceSeq, page.HeadSeq)
			case remote.OccurredAt.IsZero():
				return executionProgress{}, fmt.Errorf("execution event identity or sequence mismatch: zero occurredAt at sourceSeq=%d", remote.SourceSeq)
			}
			switch remote.EventType {
			case "execution.started", "execution.finished", "step.started", "step.finished", "stream.closed", "output.chunk", "uncertainty.detected", "verification.finished", "execution.manually_verified":
			default:
				return executionProgress{}, fmt.Errorf("unsupported execution event type %q", remote.EventType)
			}
			if strings.HasPrefix(remote.EventType, "step.") && remote.StepID == "" {
				return executionProgress{}, errors.New("execution step has no identity")
			}
			data := map[string]any{}
			if remote.DataJSON != "" {
				if err = json.Unmarshal([]byte(remote.DataJSON), &data); err != nil {
					return executionProgress{}, err
				}
				if data == nil {
					return executionProgress{}, errors.New("execution event data must be an object")
				}
			}
			if remote.EventType == "stream.closed" {
				end, valid := data["endSeq"].(float64)
				if !valid || end != float64(remote.SourceSeq) || page.EndSeq != remote.SourceSeq {
					return executionProgress{}, errors.New("stream closure does not match final source sequence")
				}
				closed = true
			}
			if remote.EventType == "step.started" || remote.EventType == "step.finished" {
				sawStep = true
				name := strings.TrimSpace(remote.StepName)
				if name == "" {
					name = remote.StepID
				}
				status := strings.TrimSpace(remote.Status)
				if status == "" && remote.EventType == "step.started" {
					status = "running"
				}
				step := controlplane.Step{Name: name, Status: status, Detail: remote.Text}
				if existing, ok := stepsByID[remote.StepID]; ok {
					if step.Name == "" {
						step.Name = existing.Name
					}
					if step.Detail == "" {
						step.Detail = existing.Detail
					}
					if remote.EventType == "step.started" && existing.Status != "" && existing.Status != "running" {
						step.Status = existing.Status
					}
				} else {
					stepOrder = append(stepOrder, remote.StepID)
				}
				stepsByID[remote.StepID] = step
			}
			fields := []zap.Field{
				zap.String("eventType", remote.EventType),
				zap.String("eventId", remote.EventID),
				zap.Int64("sourceSeq", remote.SourceSeq),
				zap.String("sourceEpoch", epoch),
				zap.String("stream", remote.Stream),
				zap.String("stepId", remote.StepID),
				zap.String("stepName", remote.StepName),
				zap.String("status", remote.Status),
				zap.String("jobId", req.JobID),
				zap.String("agentSourceId", page.SourceID),
				zap.String("action", action),
				zap.String("trigger", trigger),
				zap.Time("occurredAt", remote.OccurredAt),
			}
			msg := remote.Text
			if msg == "" {
				msg = remote.EventType
			}
			logger := logging.From(logCtx)
			switch {
			case remote.Status == "failed" || remote.Stream == "stderr":
				logger.Error(msg, fields...)
			case remote.Status == "unknown" || remote.EventType == "uncertainty.detected":
				logger.Warn(msg, fields...)
			default:
				logger.Info(msg, fields...)
			}
			if err = ctxStore.SetLastSourceSeq(logCtx, req.ExecutionID, remote.SourceSeq); err != nil {
				return executionProgress{}, err
			}
			after = remote.SourceSeq
		}
		if after > 0 {
			if err = client.AckExecutionEvents(ctx, endpoint, agentcontract.ExecutionEventsAck{ExecutionID: req.ExecutionID, SourceID: page.SourceID, SourceEpoch: epoch, SourceStreamID: stream, ThroughSourceSeq: after}); err != nil {
				return executionProgress{}, err
			}
		}
		if after < page.HeadSeq {
			if len(page.Events) == 0 || !page.HasMore {
				if err := r.recordExecutionGap(logCtx, req, operationID, resources, epoch, after+1, page.HeadSeq); err != nil {
					return executionProgress{}, err
				}
				return executionProgress{}, errors.New("agent omitted execution events before its head sequence")
			}
			continue
		}
		if page.HasMore {
			return executionProgress{}, errors.New("agent requested another page beyond its head sequence")
		}
		progress := executionProgress{Steps: stepsFromOrder(stepOrder, stepsByID), Stage: ""}
		if len(progress.Steps) > 0 {
			progress.Stage = progress.Steps[len(progress.Steps)-1].Name
		}
		if sawStep {
			r.persistJobProgress(ctx, req, progress)
		}
		if page.EndSeq > 0 {
			if !closed {
				rec, getErr := ctxStore.Get(logCtx, req.ExecutionID)
				if getErr != nil || rec.FinishedAt == "" || rec.LastSourceSeq != page.EndSeq {
					return executionProgress{}, errors.New("execution stream has no durable closing event")
				}
			} else if err = ctxStore.UpdateFinished(logCtx, req.ExecutionID, "closed", false); err != nil {
				return executionProgress{}, err
			} else if err = ctxStore.SetLastSourceSeq(logCtx, req.ExecutionID, page.EndSeq); err != nil {
				return executionProgress{}, err
			}
			progress.EndSeq = page.EndSeq
			return progress, nil
		}
		return progress, nil
	}
}

func stepsFromOrder(order []string, byID map[string]controlplane.Step) []controlplane.Step {
	if len(order) == 0 {
		return nil
	}
	out := make([]controlplane.Step, 0, len(order))
	for _, id := range order {
		if step, ok := byID[id]; ok {
			out = append(out, step)
		}
	}
	return out
}

func (r *serviceRuntimeReconciler) persistJobProgress(ctx context.Context, req controlplane.ReconcileRequestRPC, progress executionProgress) {
	if r == nil || r.service == nil || r.service.db == nil || strings.TrimSpace(req.JobID) == "" {
		return
	}
	if len(progress.Steps) == 0 && strings.TrimSpace(progress.Stage) == "" {
		return
	}
	store := controlplane.NewStore(r.service.db)
	job, err := store.GetJob(ctx, req.JobID)
	if err != nil || job.State != controlplane.JobRunning || job.LeaseToken == "" {
		return
	}
	if job.ExecutionID != "" && req.ExecutionID != "" && job.ExecutionID != req.ExecutionID {
		return
	}
	merged := mergeProjectedSteps(job.LastSteps, progress.Steps)
	stage := progress.Stage
	if stage == "" && len(merged) > 0 {
		stage = merged[len(merged)-1].Name
	}
	_, _ = store.UpdateProgress(ctx, job, stage, merged)
}

// mergeProjectedSteps appends or updates steps from a new event page onto the
// Job's already-projected trail (collect starts after the last acked seq).
func mergeProjectedSteps(existing, incoming []controlplane.Step) []controlplane.Step {
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return incoming
	}
	out := append([]controlplane.Step(nil), existing...)
	indexByName := map[string]int{}
	for i, step := range out {
		indexByName[step.Name] = i
	}
	for _, step := range incoming {
		if i, ok := indexByName[step.Name]; ok {
			out[i] = step
			continue
		}
		indexByName[step.Name] = len(out)
		out = append(out, step)
	}
	return out
}

func (r *serviceRuntimeReconciler) ResolveExecution(ctx context.Context, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, bool, error) {
	client, ok := r.service.runtimeClient.(agentcontract.ExecutionEventsClient)
	if !ok {
		return controlplane.ReconcileResponse{}, false, nil
	}
	srv, err := r.service.servers.Get(ctx, req.ServerID)
	if err != nil {
		return controlplane.ReconcileResponse{}, false, err
	}
	endpoint, ok := agentURLFromServer(srv)
	if !ok {
		return controlplane.ReconcileResponse{}, false, errors.New("agent endpoint unavailable")
	}
	result, err := client.GetExecutionResult(ctx, endpoint, req.ExecutionID)
	if err != nil {
		return controlplane.ReconcileResponse{}, false, err
	}
	if result.State == "missing" {
		// An authenticated Agent has authoritatively confirmed that it has no
		// durable record for this execution. Keeping the Job uncertain forever
		// would permanently occupy the application/server conflict domain. Runtime
		// reconcile is identity-labelled and idempotent, so release it through the
		// bounded retry policy with a fresh execution identity.
		return controlplane.ReconcileResponse{
			ErrorCode:    "execution_evidence_missing",
			ErrorClass:   "evidence",
			ErrorMessage: "agent has no durable record for the uncertain execution",
			ErrorDetail:  "the execution will be retried with a new identity",
			Retryable:    true,
		}, true, nil
	}
	progress, err := r.collectExecutionProgress(ctx, endpoint, req)
	if err != nil {
		return controlplane.ReconcileResponse{}, false, err
	}
	if result.State != "finished" || result.Result == nil || progress.EndSeq == 0 || result.EndSeq != progress.EndSeq {
		return controlplane.ReconcileResponse{}, false, nil
	}
	if result.Result.VerificationSource == "manual" {
		store := controlplane.NewStore(r.service.db)
		job, err := store.GetJobByExecutionID(ctx, req.ExecutionID)
		if err != nil {
			return controlplane.ReconcileResponse{}, false, err
		}
		err = r.service.applyManualResult(ctx, store, job, *result.Result)
		// A human declaration deliberately does not flow through acceptResponse,
		// which would otherwise write fabricated automatic Instance observations.
		// applyManualResult still invokes OnSucceeded/OnFailed hooks so purge
		// finalizers and reconcile-failure accounting cannot be skipped.
		return controlplane.ReconcileResponse{}, false, err
	}
	return reconcileResponseFromAgent(*result.Result), true, nil
}
func (r *serviceRuntimeReconciler) callTracked(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC, client AgentRuntimeReconcileClient, input agentcontract.RuntimeReconcileRequest) (controlplane.ReconcileResponse, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	// Incremental evidence remains readable while the RPC is executing.
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				_, _ = r.collectExecutionProgress(runCtx, endpoint, req)
			}
		}
	}()
	response, callErr := client.RuntimeReconcile(ctx, endpoint, input)
	cancel()
	<-done
	collectCtx, collectCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer collectCancel()
	progress, err := r.collectExecutionProgress(collectCtx, endpoint, req)
	if err != nil {
		out := reconcileResponseFromAgent(response)
		if len(progress.Steps) > 0 {
			out.Steps = progress.Steps
		}
		return out, fmt.Errorf("execution evidence was not confirmed: %w", err)
	}
	if progress.EndSeq == 0 && callErr == nil {
		out := reconcileResponseFromAgent(response)
		if len(progress.Steps) > 0 {
			out.Steps = progress.Steps
		}
		return out, errors.New("execution output stream is not closed")
	}
	out := reconcileResponseFromAgent(response)
	if len(out.Steps) == 0 && len(progress.Steps) > 0 {
		out.Steps = progress.Steps
	}
	return out, callErr
}

func (r *serviceRuntimeReconciler) recordExecutionGap(ctx context.Context, req controlplane.ReconcileRequestRPC, operationID string, resources []activitylog.Resource, epoch string, from, to int64) error {
	logCtx := logging.Ensure(ctx, logging.Context{
		StreamID:    req.ExecutionID,
		Module:      "agent-evidence",
		OperationID: operationID,
		ExecutionID: req.ExecutionID,
	})
	logging.From(logCtx).Warn("Execution evidence has a source sequence gap and requires retransmission",
		zap.String("eventType", "evidence.gap_detected"),
		zap.String("sourceId", "agent:"+req.ServerID),
		zap.String("sourceEpoch", epoch),
		zap.String("sourceStreamId", req.ExecutionID),
		zap.Int64("fromSourceSeq", from),
		zap.Int64("toSourceSeq", to),
	)
	_ = logging.RecordContext(logCtx, logging.ContextMeta{
		Origin:        events.OriginProcess,
		Title:         "evidence.gap_detected",
		Summary:       "Execution evidence has a source sequence gap and requires retransmission",
		HadError:      true,
		ResourcesJSON: logging.MarshalResourcesJSON(resources),
	})
	return nil
}
