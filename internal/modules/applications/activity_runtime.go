package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/activitylog"
)

// collectExecutionEvents returns a nonzero final source sequence only after the
// complete closed stream has committed. Source identity is bound to the trusted
// server/execution, never accepted as an arbitrary producer from the Agent.
func (r *serviceRuntimeReconciler) collectExecutionEvents(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC) (int64, error) {
	client, ok := r.service.runtimeClient.(agentcontract.ExecutionEventsClient)
	if !ok {
		return 0, errors.New("agent does not support durable execution events")
	}
	sourceID := "agent:" + req.ServerID
	var after int64
	var epoch, stream string
	err := r.service.db.QueryRowContext(ctx, `SELECT source_seq,source_epoch,source_stream_id FROM activity_events WHERE source_id=? AND execution_id=? ORDER BY source_seq DESC LIMIT 1`, sourceID, req.ExecutionID).Scan(&after, &epoch, &stream)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	resources := []activitylog.Resource{{Type: "application", ID: req.ApplicationID, Role: "subject", RevisionID: req.DesiredRevisionID, Generation: req.DesiredGeneration}, {Type: "server", ID: req.ServerID, Role: "target"}}
	// Preserve the execution's original names/initiator. Do not look up current
	// resource records while ingesting late events after rename/deletion.
	initiator := activitylog.Actor{}
	action := req.Action
	trigger := ""
	operationID, runID := req.OperationID, req.RunID
	var resourceJSON, initiatorJSON string
	err = r.service.db.QueryRowContext(ctx, `SELECT resources_json,initiator_json,action,trigger,operation_id,run_id FROM activity_events WHERE execution_id=? ORDER BY seq LIMIT 1`, req.ExecutionID).Scan(&resourceJSON, &initiatorJSON, &action, &trigger, &operationID, &runID)
	if err == nil {
		if err = json.Unmarshal([]byte(resourceJSON), &resources); err != nil {
			return 0, err
		}
		if err = json.Unmarshal([]byte(initiatorJSON), &initiator); err != nil {
			return 0, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	for {
		page, err := client.ReadExecutionEvents(ctx, endpoint, agentcontract.ExecutionEventsRequest{ExecutionID: req.ExecutionID, SourceEpoch: epoch, SourceStreamID: stream, AfterSourceSeq: after, Limit: 100})
		if err != nil {
			return 0, err
		}
		if page.SourceID == "" || page.SourceEpoch == "" || page.SourceStreamID != req.ExecutionID || (epoch != "" && epoch != page.SourceEpoch) || (stream != "" && stream != page.SourceStreamID) {
			return 0, errors.New("agent returned a different execution source")
		}
		epoch, stream = page.SourceEpoch, page.SourceStreamID
		if page.HeadSeq < after || page.EndSeq < 0 || page.EndSeq > page.HeadSeq || (page.EndSeq > 0 && page.EndSeq != page.HeadSeq) {
			return 0, errors.New("invalid execution stream watermark")
		}
		if page.AckedThroughSeq > after {
			if err := r.recordExecutionGap(ctx, req, operationID, runID, resources, page.SourceEpoch, after+1, page.AckedThroughSeq); err != nil {
				return 0, err
			}
			return 0, fmt.Errorf("execution evidence gap: source acknowledged through %d, panel has %d", page.AckedThroughSeq, after)
		}
		if len(page.Events) > 100 {
			return 0, errors.New("agent event batch exceeds requested limit")
		}
		batch := make([]activitylog.EventInput, 0, len(page.Events))
		for _, remote := range page.Events {
			if remote.EventID == "" || remote.ExecutionID != req.ExecutionID || remote.OperationID != operationID || remote.RunID != runID || remote.SourceSeq != after+int64(len(batch))+1 || remote.SourceSeq > page.HeadSeq || remote.OccurredAt.IsZero() {
				return 0, errors.New("execution event identity or sequence mismatch")
			}
			kind := "lifecycle"
			switch remote.EventType {
			case "execution.started", "execution.finished", "step.started", "step.finished", "stream.closed":
			case "output.chunk":
				kind = "output"
			case "uncertainty.detected":
				kind = "integrity"
			case "verification.finished", "execution.manually_verified":
				kind = "observation"
			default:
				return 0, fmt.Errorf("unsupported execution event type %q", remote.EventType)
			}
			if strings.HasPrefix(remote.EventType, "step.") && remote.StepID == "" {
				return 0, errors.New("execution step has no identity")
			}
			data := map[string]any{}
			if remote.DataJSON != "" {
				if err = json.Unmarshal([]byte(remote.DataJSON), &data); err != nil {
					return 0, err
				}
				if data == nil {
					return 0, errors.New("execution event data must be an object")
				}
			}
			if remote.EventType == "stream.closed" {
				end, valid := data["endSeq"].(float64)
				if !valid || end != float64(remote.SourceSeq) || page.EndSeq != remote.SourceSeq {
					return 0, errors.New("stream closure does not match final source sequence")
				}
			}
			data["step"] = remote.StepName
			data["status"] = remote.Status
			data["jobId"] = req.JobID
			// Retain the source's own persistent identity as evidence; SourceID remains
			// the authenticated Panel server identity for authorization and indexing.
			data["agentSourceId"] = page.SourceID
			level := "info"
			if remote.Status == "failed" || remote.Stream == "stderr" {
				level = "error"
			} else if remote.Status == "unknown" {
				level = "warning"
			}
			batch = append(batch, activitylog.EventInput{EventID: sourceID + ":" + remote.EventID, EventType: remote.EventType, EventVersion: 1, Kind: kind, Level: level, Domain: "application", Action: action, OperationID: operationID, RunID: runID, ExecutionID: req.ExecutionID, StepID: remote.StepID, SourceID: sourceID, SourceEpoch: epoch, SourceStreamID: stream, SourceSeq: remote.SourceSeq, OccurredAt: remote.OccurredAt, Actor: activitylog.Actor{Kind: "agent", ID: req.ServerID}, Initiator: initiator, Trigger: trigger, Resources: resources, Stream: remote.Stream, Text: remote.Text, Data: data})
		}
		if len(batch) > 0 {
			if _, err = activitylog.Append(ctx, r.service.db, batch); err != nil {
				return 0, err
			}
			after = page.Events[len(page.Events)-1].SourceSeq
		}
		if after > 0 {
			if err = r.resolveExecutionGaps(ctx, req, operationID, runID, resources, epoch, after); err != nil {
				return 0, err
			}
			if err = client.AckExecutionEvents(ctx, endpoint, agentcontract.ExecutionEventsAck{ExecutionID: req.ExecutionID, SourceID: page.SourceID, SourceEpoch: epoch, SourceStreamID: stream, ThroughSourceSeq: after}); err != nil {
				return 0, err
			}
		}
		if after < page.HeadSeq {
			if len(batch) == 0 || !page.HasMore {
				if err := r.recordExecutionGap(ctx, req, operationID, runID, resources, epoch, after+1, page.HeadSeq); err != nil {
					return 0, err
				}
				return 0, errors.New("agent omitted execution events before its head sequence")
			}
			continue
		}
		if page.HasMore {
			return 0, errors.New("agent requested another page beyond its head sequence")
		}
		if page.EndSeq > 0 {
			// A later no-op poll may see only ACKed rows; verify the immutable closure
			// already exists rather than trusting a claimed endSeq from the transport.
			var closed int
			if err = r.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_events WHERE source_id=? AND source_epoch=? AND source_stream_id=? AND source_seq=? AND event_type='stream.closed'`, sourceID, epoch, stream, page.EndSeq).Scan(&closed); err != nil {
				return 0, err
			}
			if closed != 1 {
				return 0, errors.New("execution stream has no durable closing event")
			}
			return page.EndSeq, nil
		}
		return 0, nil
	}
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
	var endSeq int64
	if result.State != "missing" {
		if endSeq, err = r.collectExecutionEvents(ctx, endpoint, req); err != nil {
			return controlplane.ReconcileResponse{}, false, err
		}
	}
	if result.State != "finished" || result.Result == nil || endSeq == 0 || result.EndSeq != endSeq {
		return controlplane.ReconcileResponse{}, false, nil
	}
	if result.Result.VerificationSource == "manual" {
		store := controlplane.NewStore(r.service.db)
		job, err := store.GetJobByExecutionID(ctx, req.ExecutionID)
		if err != nil {
			return controlplane.ReconcileResponse{}, false, err
		}
		err = applyManualResult(ctx, store, job, *result.Result)
		// A human declaration deliberately does not flow through acceptResponse,
		// which would otherwise write fabricated automatic Instance observations.
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
				_, _ = r.collectExecutionEvents(runCtx, endpoint, req)
			}
		}
	}()
	response, callErr := client.RuntimeReconcile(ctx, endpoint, input)
	cancel()
	<-done
	collectCtx, collectCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer collectCancel()
	if endSeq, err := r.collectExecutionEvents(collectCtx, endpoint, req); err != nil {
		return reconcileResponseFromAgent(response), fmt.Errorf("execution evidence was not confirmed: %w", err)
	} else if endSeq == 0 && callErr == nil {
		return reconcileResponseFromAgent(response), errors.New("execution output stream is not closed")
	}
	return reconcileResponseFromAgent(response), callErr
}

// A gap is itself an immutable fact, not merely an error that disappears from
// the next controller poll. Repeated detection does not overwrite its time.
func (r *serviceRuntimeReconciler) recordExecutionGap(ctx context.Context, req controlplane.ReconcileRequestRPC, operationID, runID string, resources []activitylog.Resource, epoch string, from, to int64) error {
	eventID := fmt.Sprintf("agent-gap:%s:%s:%s:%d:%d", req.ServerID, req.ExecutionID, epoch, from, to)
	var present int
	if err := r.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_events WHERE event_id=?`, eventID).Scan(&present); err != nil {
		return err
	}
	if present > 0 {
		return nil
	}
	_, err := activitylog.Append(ctx, r.service.db, []activitylog.EventInput{{EventID: eventID, EventType: "evidence.gap_detected", Kind: "integrity", Level: "warning", Domain: "application", OperationID: operationID, RunID: runID, ExecutionID: req.ExecutionID, SourceID: "panel", SourceStreamID: "integrity:" + req.ExecutionID, Actor: activitylog.Actor{Kind: "controller", ID: "application"}, Resources: resources, Text: "Execution evidence has a source sequence gap and requires retransmission", Data: map[string]any{"sourceId": "agent:" + req.ServerID, "sourceEpoch": epoch, "sourceStreamId": req.ExecutionID, "fromSourceSeq": from, "toSourceSeq": to}}})
	return err
}

func (r *serviceRuntimeReconciler) resolveExecutionGaps(ctx context.Context, req controlplane.ReconcileRequestRPC, operationID, runID string, resources []activitylog.Resource, epoch string, after int64) error {
	rows, err := r.service.db.QueryContext(ctx, `SELECT g.event_id,g.data_json FROM activity_events g WHERE g.execution_id=? AND g.event_type='evidence.gap_detected' AND NOT EXISTS(SELECT 1 FROM activity_events resolved WHERE resolved.event_type='evidence.gap_resolved' AND resolved.causation_event_id=g.event_id)`, req.ExecutionID)
	if err != nil {
		return err
	}
	type gap struct {
		id       string
		from, to int64
	}
	var gaps []gap
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var data struct {
			Epoch string `json:"sourceEpoch"`
			From  int64  `json:"fromSourceSeq"`
			To    int64  `json:"toSourceSeq"`
		}
		if err = json.Unmarshal([]byte(raw), &data); err != nil {
			rows.Close()
			return err
		}
		if data.Epoch == epoch && data.From > 0 && data.To >= data.From && data.To <= after {
			gaps = append(gaps, gap{id, data.From, data.To})
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, g := range gaps {
		var count int64
		if err = r.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_events WHERE source_id=? AND source_epoch=? AND source_stream_id=? AND source_seq BETWEEN ? AND ?`, "agent:"+req.ServerID, epoch, req.ExecutionID, g.from, g.to).Scan(&count); err != nil {
			return err
		}
		if count != g.to-g.from+1 {
			continue
		}
		if _, err = activitylog.Append(ctx, r.service.db, []activitylog.EventInput{{EventID: "resolved:" + g.id, EventType: "evidence.gap_resolved", CausationEventID: g.id, Kind: "integrity", Level: "info", Domain: "application", OperationID: operationID, RunID: runID, ExecutionID: req.ExecutionID, SourceID: "panel", SourceStreamID: "integrity:" + req.ExecutionID, Actor: activitylog.Actor{Kind: "controller", ID: "application"}, Resources: resources, Text: "Missing execution evidence has been durably received", Data: map[string]any{"sourceEpoch": epoch, "fromSourceSeq": g.from, "toSourceSeq": g.to}}}); err != nil {
			return err
		}
	}
	return nil
}
