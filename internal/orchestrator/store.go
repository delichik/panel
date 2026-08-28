package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	id "panel/internal/platform/identity"

	"go.uber.org/zap"
)

var ErrOwnershipLost = errors.New("orchestrator job ownership lost")

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) withNow(now func() time.Time) *Store {
	if now != nil {
		s.now = now
	}
	return s
}

type jobRow struct {
	ID, ApplicationID, ServerID, InstanceID, Action                                       string
	DesiredGeneration                                                                     int
	DesiredSpecHash, DesiredRevisionID                                                    string
	DesiredSpecJSON                                                                       string
	RemoveData                                                                            bool
	ForceNonce                                                                            int64
	State                                                                                 string
	Priority, Attempts                                                                    int
	NextRunAt, LeaseExpiresAt                                                             sql.NullString
	LeaseOwner, LeaseToken, ExecutionID                                                   string
	IntentID, TriggerType, TriggerResourceType, TriggerResourceID, Reason, IdempotencyKey string
	LastStage, LastStepsJSON, ErrorCode, ErrorClass, ErrorMessage, ErrorDetail            string
	CreatedAt, UpdatedAt                                                                  string
	StartedAt, FinishedAt                                                                 sql.NullString
}

const jobColumns = `id,application_id,server_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,remove_data,force_nonce,state,priority,attempts,next_run_at,lease_owner,lease_token,lease_expires_at,execution_id,intent_id,trigger_type,trigger_resource_type,trigger_resource_id,reason,idempotency_key,last_stage,last_steps_json,error_code,error_class,error_message,error_detail,created_at,started_at,finished_at,updated_at`

func scanJob(scanner interface{ Scan(...any) error }) (Job, error) {
	var r jobRow
	if err := scanner.Scan(
		&r.ID, &r.ApplicationID, &r.ServerID, &r.InstanceID, &r.Action,
		&r.DesiredGeneration, &r.DesiredSpecHash, &r.DesiredRevisionID, &r.DesiredSpecJSON,
		&r.RemoveData, &r.ForceNonce, &r.State, &r.Priority, &r.Attempts,
		&r.NextRunAt, &r.LeaseOwner, &r.LeaseToken, &r.LeaseExpiresAt,
		&r.ExecutionID, &r.IntentID, &r.TriggerType, &r.TriggerResourceType,
		&r.TriggerResourceID, &r.Reason, &r.IdempotencyKey, &r.LastStage,
		&r.LastStepsJSON, &r.ErrorCode, &r.ErrorClass, &r.ErrorMessage,
		&r.ErrorDetail, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.UpdatedAt,
	); err != nil {
		return Job{}, err
	}
	return jobFromRow(r), nil
}

func jobFromRow(r jobRow) Job {
	var steps []Step
	_ = json.Unmarshal([]byte(firstJSON(r.LastStepsJSON, "[]")), &steps)
	return Job{
		ID: r.ID, ApplicationID: r.ApplicationID, ServerID: r.ServerID,
		InstanceID: r.InstanceID, Action: r.Action, DesiredGeneration: r.DesiredGeneration,
		DesiredSpecHash: r.DesiredSpecHash, DesiredRevisionID: r.DesiredRevisionID,
		DesiredSpecJSON: json.RawMessage(firstJSON(r.DesiredSpecJSON, "{}")),
		RemoveData:      r.RemoveData, ForceNonce: r.ForceNonce, State: r.State,
		Priority: r.Priority, Attempts: r.Attempts, NextRunAt: parseNullableTime(r.NextRunAt),
		LeaseOwner: r.LeaseOwner, LeaseToken: r.LeaseToken, LeaseExpiresAt: parseNullableTime(r.LeaseExpiresAt),
		ExecutionID: r.ExecutionID, IntentID: r.IntentID, TriggerType: r.TriggerType,
		TriggerResourceType: r.TriggerResourceType, TriggerResourceID: r.TriggerResourceID,
		Reason: r.Reason, IdempotencyKey: r.IdempotencyKey, LastStage: r.LastStage,
		LastSteps: steps, ErrorCode: r.ErrorCode, ErrorClass: r.ErrorClass,
		ErrorMessage: r.ErrorMessage, ErrorDetail: r.ErrorDetail, CreatedAt: parseTimeValue(r.CreatedAt),
		StartedAt: parseNullableTime(r.StartedAt), FinishedAt: parseNullableTime(r.FinishedAt), UpdatedAt: parseTimeValue(r.UpdatedAt),
	}
}

func (s *Store) GetJob(ctx context.Context, jobID string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=?`, strings.TrimSpace(jobID))
	return scanJob(row)
}

func (s *Store) InstanceContainerName(ctx context.Context, instanceID string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT container_name FROM application_instances WHERE id=?`, strings.TrimSpace(instanceID)).Scan(&name)
	return name, err
}

func (s *Store) DesiredChanged(ctx context.Context, job Job) (bool, error) {
	var desiredState, specHash, revisionID, desiredSpecJSON string
	var generation int
	err := s.db.QueryRowContext(ctx, `SELECT desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json FROM application_instances WHERE id=?`, job.InstanceID).
		Scan(&desiredState, &generation, &specHash, &revisionID, &desiredSpecJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Purge is idempotent: the instance row may already have been
			// removed by the finalizer while the runtime was being reconciled.
			// Apply/stop still need the desired row as a fencing signal.
			return job.Action != ActionPurge, nil
		}
		return false, err
	}
	wantState := desiredForAction(job.Action)
	var currentForceNonce int64
	if err := s.db.QueryRowContext(ctx, `SELECT force_nonce FROM jobs WHERE id=?`, job.ID).Scan(&currentForceNonce); err != nil {
		return false, err
	}
	return desiredState != wantState || generation != job.DesiredGeneration || strings.TrimSpace(specHash) != strings.TrimSpace(job.DesiredSpecHash) || strings.TrimSpace(revisionID) != strings.TrimSpace(job.DesiredRevisionID) || !sameJSON(desiredSpecJSON, job.DesiredSpecJSON) || currentForceNonce != job.ForceNonce, nil
}

func (s *Store) Requeue(ctx context.Context, job Job, reason string) (bool, error) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	var desiredState, desiredSpecHash, desiredRevisionID, desiredSpecJSON string
	var desiredGeneration int
	var removeData bool
	var forceNonce int64
	err := s.db.QueryRowContext(ctx, `SELECT i.desired_state,i.desired_generation,i.desired_spec_hash,i.desired_revision_id,i.desired_spec_json,CASE WHEN i.desired_state='purged' AND a.deletion_requested=1 THEN 1 ELSE 0 END,j.force_nonce
		FROM application_instances i JOIN applications a ON a.id=i.application_id JOIN jobs j ON j.id=? WHERE i.id=?`, job.ID, job.InstanceID).
		Scan(&desiredState, &desiredGeneration, &desiredSpecHash, &desiredRevisionID, &desiredSpecJSON, &removeData, &forceNonce)
	if errors.Is(err, sql.ErrNoRows) && job.Action == ActionPurge {
		// The purge finalizer may have removed the desired row after the
		// remote call. There is no next runtime action to enqueue.
		result, updateErr := s.db.ExecContext(ctx, `UPDATE jobs SET state='succeeded',lease_owner='',lease_token='',lease_expires_at=NULL,last_stage='purged',error_code='',error_class='',error_message='',error_detail='',finished_at=?,updated_at=? WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`, now, now, job.ID, job.LeaseOwner, job.LeaseToken)
		if updateErr != nil {
			return false, updateErr
		}
		affected, updateErr := result.RowsAffected()
		if affected == 1 {
			traceJobEvent("job_superseded", job, zap.String("reason", "purge_finalizer_removed_instance"))
		}
		return affected == 1, updateErr
	}
	if err != nil {
		return false, err
	}
	action := actionForDesired(desiredState)
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state='pending',action=?,desired_generation=?,desired_spec_hash=?,desired_revision_id=?,desired_spec_json=?,remove_data=?,force_nonce=?,lease_owner='',lease_token='',lease_expires_at=NULL,next_run_at=NULL,last_stage='superseded',error_code='desired_changed',error_class='superseded',error_message=?,error_detail='desired state changed while runtime call was in flight',finished_at=NULL,updated_at=? WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`,
		action, desiredGeneration, desiredSpecHash, desiredRevisionID, desiredSpecJSON, boolInt(removeData), forceNonce, reason, now, job.ID, job.LeaseOwner, job.LeaseToken)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if affected == 1 {
		traceJobEvent("job_superseded", job, zap.String("reason", reason), zap.String("new_action", action))
	}
	return affected == 1, err
}

func sameJSON(left string, right []byte) bool {
	left = strings.TrimSpace(left)
	rightRaw := strings.TrimSpace(string(right))
	if left == "" {
		left = "{}"
	}
	if rightRaw == "" {
		rightRaw = "{}"
	}
	var leftValue, rightValue any
	if json.Unmarshal([]byte(left), &leftValue) != nil || json.Unmarshal([]byte(rightRaw), &rightValue) != nil {
		return left == rightRaw
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func (s *Store) ListDue(ctx context.Context, limit int) ([]Job, error) {
	if limit < 1 {
		limit = 128
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE state IN ('pending','failed_retryable') AND (next_run_at IS NULL OR next_run_at='' OR next_run_at<=?)
		ORDER BY priority DESC, created_at ASC, id ASC LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) Claim(ctx context.Context, jobID, owner string, leaseTTL time.Duration) (Job, bool, error) {
	now := s.now().UTC()
	if leaseTTL <= 0 {
		leaseTTL = 3 * time.Minute
	}
	token := id.New("lease")
	executionID := id.New("exec")
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state='running',lease_owner=?,lease_token=?,lease_expires_at=?,execution_id=?,attempts=attempts+1,started_at=COALESCE(started_at,?),updated_at=?
		WHERE id=? AND state IN ('pending','failed_retryable') AND (next_run_at IS NULL OR next_run_at='' OR next_run_at<=?)`,
		owner, token, now.Add(leaseTTL).Format(time.RFC3339Nano), executionID,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), jobID, now.Format(time.RFC3339Nano))
	if err != nil {
		return Job{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return Job{}, false, err
	}
	job, err := s.GetJob(ctx, jobID)
	return job, true, err
}

func (s *Store) Renew(ctx context.Context, job Job, leaseTTL time.Duration) (bool, error) {
	if leaseTTL <= 0 {
		leaseTTL = 3 * time.Minute
	}
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at=?,updated_at=? WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`,
		now.Add(leaseTTL).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), job.ID, job.LeaseOwner, job.LeaseToken)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *Store) Succeed(ctx context.Context, job Job, response ReconcileResponse) (bool, error) {
	now := s.now().UTC()
	steps, _ := json.Marshal(response.Steps)
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state='succeeded',lease_owner='',lease_token='',lease_expires_at=NULL,last_stage=?,last_steps_json=?,error_code='',error_class='',error_message='',error_detail='',finished_at=?,updated_at=? WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`,
		lastStep(response.Steps), string(steps), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), job.ID, job.LeaseOwner, job.LeaseToken)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *Store) Fail(ctx context.Context, job Job, response ReconcileResponse) (bool, error) {
	now := s.now().UTC()
	state := JobFailed
	var nextRun any
	if response.Retryable {
		state = JobFailedRetryable
		nextRun = now.Add(retryDelay(job.Attempts, response.RetryAfter)).Format(time.RFC3339Nano)
	}
	steps, _ := json.Marshal(response.Steps)
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=?,lease_owner='',lease_token='',lease_expires_at=NULL,last_stage=?,last_steps_json=?,error_code=?,error_class=?,error_message=?,error_detail=?,next_run_at=?,finished_at=CASE WHEN ?='failed' THEN ? ELSE NULL END,updated_at=? WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`,
		state, lastStep(response.Steps), string(steps), response.ErrorCode, response.ErrorClass, response.ErrorMessage, response.ErrorDetail,
		nextRun, state, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), job.ID, job.LeaseOwner, job.LeaseToken)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (s *Store) RecoverExpiredLeases(ctx context.Context) error {
	now := s.now().UTC()
	expired, err := s.listRunningJobsWithExpiredLeases(ctx, now)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=CASE WHEN last_stage='' THEN 'pending' ELSE 'failed_retryable' END,
		lease_owner='',lease_token='',lease_expires_at=NULL,error_code=CASE WHEN last_stage='' THEN error_code ELSE 'lease_lost' END,
		error_class=CASE WHEN last_stage='' THEN error_class ELSE 'ownership' END,
		error_message=CASE WHEN last_stage='' THEN error_message ELSE 'orchestrator lease expired' END,
		error_detail=CASE WHEN last_stage='' THEN error_detail ELSE 'running job recovered after lease expiry' END,
		next_run_at=CASE WHEN last_stage='' THEN NULL ELSE ? END,updated_at=?
		WHERE state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at<>'' AND lease_expires_at<=?`,
		now.Add(retryDelay(1, 0)).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	for _, job := range expired {
		state := "pending"
		if job.LastStage != "" {
			state = JobFailedRetryable
		}
		traceJobEvent("lease_lost", job,
			zap.String("reason", "lease_expired"),
			zap.String("recovered_state", state))
	}
	_ = affected
	return nil
}

func (s *Store) listRunningJobsWithExpiredLeases(ctx context.Context, now time.Time) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM jobs
		WHERE state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at<>'' AND lease_expires_at<=?`, now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) Owned(ctx context.Context, job Job) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE id=? AND state='running' AND lease_owner=? AND lease_token=?`, job.ID, job.LeaseOwner, job.LeaseToken).Scan(&n)
	return n == 1, err
}

func firstJSON(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	return parseTime(value.String)
}

func parseTimeValue(value string) time.Time {
	if parsed := parseTime(value); parsed != nil {
		return *parsed
	}
	return time.Time{}
}

func lastStep(steps []Step) string {
	if len(steps) == 0 {
		return ""
	}
	return steps[len(steps)-1].Name
}

func (s *Store) Validate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("orchestrator store database is not configured")
	}
	return nil
}
