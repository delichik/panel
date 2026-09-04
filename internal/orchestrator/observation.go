package orchestrator

import (
	"context"
	"database/sql"
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
	// an unsequenced reconcile response may refresh an agent report but cannot
	// overwrite an already accepted reconcile response.
	query := `UPDATE application_instances SET observed_state=?,observed_container_name=?,observed_container_id=?,observed_generation=?,observed_spec_hash=?,observed_image_digest=?,observed_at=?,observed_sequence=?,observed_source=?,last_reconcile_job_id=?,last_error_code=?,last_error_class=?,last_error_message=?,last_error_detail=?,last_deployed_generation=CASE WHEN ? > 0 THEN ? ELSE last_deployed_generation END,status=?,runtime_spec_json=CASE WHEN ?='reconcile' AND trim(COALESCE(? ,'')) NOT IN ('','null','{}') THEN ? ELSE runtime_spec_json END,last_error=CASE WHEN ? IN ('failed','unknown') THEN COALESCE(NULLIF(?,''),last_error) ELSE '' END,updated_at=? WHERE id=? AND ((? > observed_sequence AND ? > 0) OR (?=0 AND observed_source<>'reconcile' AND (observed_at IS NULL OR observed_at<=?)) OR (?=0 AND ?='reconcile'))`
	queryArgs := []any{state, in.ContainerName, in.ContainerID, in.ObservedGeneration, in.ObservedSpecHash, in.ObservedImageDigest,
		observedAt.Format(time.RFC3339Nano), in.Sequence, in.Source, in.JobID, in.LastErrorCode, in.LastErrorClass, in.LastErrorMessage, in.LastErrorDetail,
		in.ObservedGeneration, in.ObservedGeneration, status, in.Source, desiredSpecJSON, desiredSpecJSON, state, in.LastErrorMessage, observedAt.Format(time.RFC3339Nano), in.InstanceID,
		in.Sequence, in.Sequence, in.Sequence, observedAt.Format(time.RFC3339Nano), in.Sequence, in.Source}
	if strings.TrimSpace(in.JobID) != "" && strings.TrimSpace(in.LeaseToken) != "" {
		query += ` AND EXISTS (SELECT 1 FROM jobs WHERE jobs.id=? AND jobs.state='running' AND jobs.lease_token=?)`
		queryArgs = append(queryArgs, in.JobID, in.LeaseToken)
	}
	result, err := w.db.ExecContext(ctx, query, queryArgs...)
	if err != nil {
		return WriteResult{}, err
	}
	affected, err := result.RowsAffected()
	return WriteResult{Accepted: affected == 1}, err
}
