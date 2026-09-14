package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"panel/internal/platform/activitylog"
	"strings"
	"time"
)

type ObservationWriter struct {
	db *sql.DB
}

func NewObservationWriter(db *sql.DB) *ObservationWriter { return &ObservationWriter{db: db} }

func (w *ObservationWriter) Write(ctx context.Context, in Observation) (WriteResult, error) {
	if w == nil || w.db == nil {
		return WriteResult{}, ErrStoreUnavailable
	}
	if strings.TrimSpace(in.InstanceID) == "" {
		return WriteResult{}, &ValidationError{Message: "instance is required"}
	}
	in.LastErrorMessage = activitylog.Redact(in.LastErrorMessage)
	in.LastErrorDetail = activitylog.Redact(in.LastErrorDetail)
	observedAt := in.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	state := in.ObservedState
	if state == "" {
		state = ObservedUnknown
	}
	status := state
	if state == ObservedMissing {
		status = "missing"
	}
	if state == ObservedUnknown {
		status = "unknown"
	}
	desiredSpecJSON := strings.TrimSpace(string(in.DesiredSpecJSON))
	// The CAS is source-aware: sequenced reports must advance the sequence;
	// an unsequenced reconcile response may refresh an agent report, while an
	// unsequenced report cannot overwrite an already accepted reconcile result.
	query := `UPDATE application_instances SET observed_state=?,observed_container_name=?,observed_container_id=?,observed_generation=?,observed_spec_hash=?,observed_image_digest=?,observed_at=?,observed_sequence=?,observed_source=?,last_reconcile_job_id=?,last_error_code=?,last_error_class=?,last_error_message=?,last_error_detail=?,last_deployed_generation=CASE WHEN ? > 0 THEN ? ELSE last_deployed_generation END,status=?,runtime_spec_json=CASE WHEN ?='reconcile' AND trim(COALESCE(? ,'')) NOT IN ('','null','{}') THEN ? ELSE runtime_spec_json END,last_error=CASE WHEN ? IN ('failed','unknown') THEN COALESCE(NULLIF(?,''),last_error) ELSE '' END,updated_at=? WHERE id=? AND ((? > observed_sequence AND ? > 0) OR (?=0 AND observed_source<>'reconcile' AND (observed_at IS NULL OR observed_at<=?)) OR (?=0 AND ?='reconcile'))`
	queryArgs := []any{state, in.ContainerName, in.ContainerID, in.ObservedGeneration, in.ObservedSpecHash, in.ObservedImageDigest,
		observedAt.Format(time.RFC3339Nano), in.Sequence, in.Source, in.JobID, in.LastErrorCode, in.LastErrorClass, in.LastErrorMessage, in.LastErrorDetail,
		in.ObservedGeneration, in.ObservedGeneration, status, in.Source, desiredSpecJSON, desiredSpecJSON, state, in.LastErrorMessage, observedAt.Format(time.RFC3339Nano), in.InstanceID,
		in.Sequence, in.Sequence, in.Sequence, observedAt.Format(time.RFC3339Nano), in.Sequence, in.Source}
	if strings.TrimSpace(in.JobID) != "" {
		query += ` AND EXISTS (SELECT 1 FROM jobs WHERE jobs.id=? AND jobs.state='running' AND jobs.lease_token=?)`
		queryArgs = append(queryArgs, in.JobID, in.LeaseToken)
	}
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return WriteResult{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, queryArgs...)
	if err != nil {
		return WriteResult{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return WriteResult{}, err
	}
	if affected == 0 {
		// Cache/report observations are advisory and routinely lose this CAS to a
		// newer reconcile result. They have no durable Job/operation to which a
		// user-visible warning could be attached, so quietly discard them.
		if strings.TrimSpace(in.JobID) != "" {
			rejection, rejectErr := w.describeRejection(ctx, tx, in, state, observedAt)
			if rejectErr != nil {
				return WriteResult{}, rejectErr
			}
			_, err = activitylog.AppendTx(ctx, tx, []activitylog.EventInput{{
				EventType:   "observation.rejected",
				Kind:        "observation",
				Level:       "warning",
				Domain:      "application",
				OperationID: rejection.operationID,
				ExecutionID: rejection.executionID,
				Resources:   rejection.resources,
				Data:        rejection.data,
			}})
			if err != nil {
				return WriteResult{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Accepted: affected == 1}, nil
}

type observationRejection struct {
	operationID string
	executionID string
	resources   []activitylog.Resource
	data        map[string]any
}

func (w *ObservationWriter) describeRejection(ctx context.Context, tx *sql.Tx, in Observation, state string, observedAt time.Time) (observationRejection, error) {
	type currentInstance struct {
		applicationID, serverID, observedState, observedSpecHash, observedAt, observedSource, lastJobID string
		observedGeneration                                                                              int
		observedSequence                                                                                int64
	}
	type currentJob struct {
		applicationID, serverID, instanceID, state, leaseOwner, leaseToken, operationID, executionID, revisionID string
		desiredGeneration                                                                                        int
	}

	var instance currentInstance
	instanceErr := tx.QueryRowContext(ctx, `SELECT application_id,server_id,observed_state,observed_generation,observed_spec_hash,COALESCE(observed_at,''),observed_sequence,observed_source,last_reconcile_job_id FROM application_instances WHERE id=?`, in.InstanceID).
		Scan(&instance.applicationID, &instance.serverID, &instance.observedState, &instance.observedGeneration, &instance.observedSpecHash, &instance.observedAt, &instance.observedSequence, &instance.observedSource, &instance.lastJobID)
	if instanceErr != nil && !errors.Is(instanceErr, sql.ErrNoRows) {
		return observationRejection{}, instanceErr
	}

	var job currentJob
	jobErr := tx.QueryRowContext(ctx, `SELECT application_id,server_id,instance_id,state,lease_owner,lease_token,intent_id,execution_id,desired_revision_id,desired_generation FROM jobs WHERE id=?`, in.JobID).
		Scan(&job.applicationID, &job.serverID, &job.instanceID, &job.state, &job.leaseOwner, &job.leaseToken, &job.operationID, &job.executionID, &job.revisionID, &job.desiredGeneration)
	if jobErr != nil && !errors.Is(jobErr, sql.ErrNoRows) {
		return observationRejection{}, jobErr
	}

	reason := "stale_observation"
	switch {
	case errors.Is(instanceErr, sql.ErrNoRows):
		reason = "instance_missing"
	case errors.Is(jobErr, sql.ErrNoRows):
		reason = "job_missing"
	case job.instanceID != in.InstanceID:
		reason = "instance_fencing_mismatch"
	case job.state != JobRunning || strings.TrimSpace(in.LeaseToken) == "" || job.leaseToken != in.LeaseToken:
		reason = "lease_lost"
	}

	applicationID := job.applicationID
	serverID := job.serverID
	if applicationID == "" {
		applicationID = instance.applicationID
	}
	if serverID == "" {
		serverID = instance.serverID
	}
	resources := make([]activitylog.Resource, 0, 2)
	if applicationID != "" {
		var name string
		_ = tx.QueryRowContext(ctx, `SELECT name FROM applications WHERE id=?`, applicationID).Scan(&name)
		resources = append(resources, activitylog.Resource{Type: "application", ID: applicationID, Name: name, Role: "target", RevisionID: job.revisionID, Generation: job.desiredGeneration})
	}
	if serverID != "" {
		var name string
		_ = tx.QueryRowContext(ctx, `SELECT name FROM servers WHERE id=?`, serverID).Scan(&name)
		resources = append(resources, activitylog.Resource{Type: "server", ID: serverID, Name: name, Role: "executor"})
	}

	return observationRejection{
		operationID: job.operationID,
		executionID: job.executionID,
		resources:   resources,
		data: map[string]any{
			"instanceId": in.InstanceID,
			"jobId":      in.JobID,
			"reason":     reason,
			"incoming": map[string]any{
				"source":             in.Source,
				"sequence":           in.Sequence,
				"observedAt":         observedAt,
				"observedState":      state,
				"observedGeneration": in.ObservedGeneration,
				"observedSpecHash":   in.ObservedSpecHash,
				"leasePresent":       strings.TrimSpace(in.LeaseToken) != "",
			},
			"current": map[string]any{
				"instanceExists":     instanceErr == nil,
				"observedAt":         instance.observedAt,
				"observedState":      instance.observedState,
				"observedGeneration": instance.observedGeneration,
				"observedSpecHash":   instance.observedSpecHash,
				"observedSequence":   instance.observedSequence,
				"observedSource":     instance.observedSource,
				"lastJobId":          instance.lastJobID,
				"jobExists":          jobErr == nil,
				"jobState":           job.state,
				"jobInstanceId":      job.instanceID,
				"leaseOwner":         job.leaseOwner,
				"leaseMatches":       jobErr == nil && strings.TrimSpace(in.LeaseToken) != "" && job.leaseToken == in.LeaseToken,
			},
		},
	}, nil
}
