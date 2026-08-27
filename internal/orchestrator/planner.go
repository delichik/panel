package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	id "panel/internal/platform/identity"

	"go.uber.org/zap"
)

type Planner struct {
	store *Store
}

func NewPlanner(store *Store) *Planner { return &Planner{store: store} }

func (p *Planner) EnsureRevision(ctx context.Context, in RevisionInput) (Revision, error) {
	if p == nil || p.store == nil {
		return Revision{}, ErrStoreUnavailable
	}
	if in.ApplicationID == "" || in.Generation <= 0 {
		return Revision{}, &ValidationError{Message: "application and generation are required"}
	}
	runtimeSpec := in.RenderedRuntimeSpec
	if len(runtimeSpec) == 0 {
		runtimeSpec = json.RawMessage(`{}`)
	}
	manifest, _ := json.Marshal(in.ManagedFileManifest)
	if len(manifest) == 0 {
		manifest = []byte(`[]`)
	}
	now := p.store.now().UTC().Format(time.RFC3339Nano)
	revisionID := id.New("arev")
	_, err := p.store.db.ExecContext(ctx, `INSERT INTO application_revisions(id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,job_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(application_id,generation) DO NOTHING`,
		revisionID, in.ApplicationID, in.Generation, in.SpecHash, string(runtimeSpec), string(manifest), in.ImageReference, in.ResolvedImageDigest, in.SpecYAML, string(runtimeSpec), now)
	if err != nil {
		return Revision{}, err
	}
	return p.store.GetRevision(ctx, in.ApplicationID, in.Generation)
}

// EnsureRevisionAndPlanBatch commits the immutable revision, all desired
// instance updates and all Job merges in one AppDB transaction. It is the
// application-save path; callers should use it whenever a new generation is
// being planned.
func (p *Planner) EnsureRevisionAndPlanBatch(ctx context.Context, revisionInput RevisionInput, inputs []PlanInput) (Revision, []PlanResult, error) {
	if p == nil || p.store == nil || p.store.db == nil {
		return Revision{}, nil, ErrStoreUnavailable
	}
	if revisionInput.ApplicationID == "" || revisionInput.Generation <= 0 {
		return Revision{}, nil, &ValidationError{Message: "application and generation are required"}
	}
	for _, in := range inputs {
		if err := validatePlanInput(in); err != nil {
			return Revision{}, nil, err
		}
	}
	tx, err := p.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Revision{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertRevisionTx(ctx, tx, p.store.now, revisionInput); err != nil {
		return Revision{}, nil, err
	}
	revision, err := getRevisionTx(ctx, tx, revisionInput.ApplicationID, revisionInput.Generation)
	if err != nil {
		return Revision{}, nil, err
	}
	results := make([]PlanResult, 0, len(inputs))
	for _, in := range inputs {
		if in.DesiredRevisionID == "" {
			in.DesiredRevisionID = revision.ID
		}
		result, err := p.planTx(ctx, tx, in)
		if err != nil {
			return Revision{}, nil, err
		}
		results = append(results, result)
	}
	if err := tx.Commit(); err != nil {
		return Revision{}, nil, err
	}
	return revision, results, nil
}

func insertRevisionTx(ctx context.Context, tx *sql.Tx, nowFunc func() time.Time, in RevisionInput) error {
	runtimeSpec := in.RenderedRuntimeSpec
	if len(runtimeSpec) == 0 {
		runtimeSpec = json.RawMessage(`{}`)
	}
	manifest, _ := json.Marshal(in.ManagedFileManifest)
	if len(manifest) == 0 {
		manifest = []byte(`[]`)
	}
	now := time.Now().UTC()
	if nowFunc != nil {
		now = nowFunc().UTC()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO application_revisions(id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,job_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(application_id,generation) DO NOTHING`,
		id.New("arev"), in.ApplicationID, in.Generation, in.SpecHash, string(runtimeSpec), string(manifest), in.ImageReference, in.ResolvedImageDigest, in.SpecYAML, string(runtimeSpec), now.Format(time.RFC3339Nano))
	return err
}

func (p *Planner) Plan(ctx context.Context, in PlanInput) (PlanResult, error) {
	results, err := p.PlanBatch(ctx, []PlanInput{in})
	if err != nil {
		return PlanResult{}, err
	}
	return results[0], nil
}

// PlanBatch applies a set of desired instance updates and Job merges in one
// short AppDB transaction. The caller may send a wake signal only after this
// method returns successfully; a partial target set is never visible to the
// controller.
func (p *Planner) PlanBatch(ctx context.Context, inputs []PlanInput) ([]PlanResult, error) {
	if p == nil || p.store == nil || p.store.db == nil {
		return nil, ErrStoreUnavailable
	}
	if len(inputs) == 0 {
		return []PlanResult{}, nil
	}
	for _, in := range inputs {
		if err := validatePlanInput(in); err != nil {
			return nil, err
		}
	}
	tx, err := p.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	results := make([]PlanResult, 0, len(inputs))
	for _, in := range inputs {
		result, err := p.planTx(ctx, tx, in)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return results, nil
}

func (p *Planner) planTx(ctx context.Context, tx *sql.Tx, in PlanInput) (PlanResult, error) {
	if in.IdempotencyKey != "" {
		if existing, found, err := scanExistingJob(ctx, tx, in.ApplicationID, in.IdempotencyKey); err != nil {
			return PlanResult{}, err
		} else if found {
			traceJobEvent("job_merged", existing, zap.String("reason", "idempotency_key_replay"))
			return PlanResult{Job: existing, Merged: true}, nil
		}
	}
	if in.IntentID == "" {
		in.IntentID = id.New("intent")
	}
	if in.InstanceID == "" {
		in.InstanceID = in.ApplicationID + "-" + in.ServerID
	}
	if in.Action == "" {
		in.Action = actionForDesired(in.DesiredState)
	}
	if in.DesiredState == "" {
		in.DesiredState = desiredForAction(in.Action)
	}
	if in.DesiredSpecJSON == nil {
		in.DesiredSpecJSON = json.RawMessage(`{}`)
	}
	now := p.store.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?, 'pending','{}',0,'',?,?)
		ON CONFLICT(application_id,server_id) DO UPDATE SET
			id=excluded.id,container_name=CASE WHEN excluded.container_name<>'' THEN excluded.container_name ELSE application_instances.container_name END,
			desired_state=excluded.desired_state,desired_generation=excluded.desired_generation,desired_spec_hash=excluded.desired_spec_hash,
			desired_revision_id=excluded.desired_revision_id,desired_spec_json=excluded.desired_spec_json,updated_at=excluded.updated_at`,
		in.InstanceID, in.ApplicationID, in.ServerID, in.ContainerName, "", in.DesiredState, in.DesiredGeneration, in.DesiredSpecHash,
		in.DesiredRevisionID, string(in.DesiredSpecJSON), now, now); err != nil {
		return PlanResult{}, err
	}
	active, found, err := scanActiveJob(ctx, tx, in.ApplicationID, in.ServerID)
	if err != nil {
		return PlanResult{}, err
	}
	if found {
		if active.State == JobPending || active.State == JobFailedRetryable {
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET instance_id=?,action=?,desired_generation=?,desired_spec_hash=?,desired_revision_id=?,desired_spec_json=?,remove_data=?,force_nonce=?,priority=?,next_run_at=NULL,intent_id=?,trigger_type=?,trigger_resource_type=?,trigger_resource_id=?,reason=?,error_code='',error_class='',error_message='',error_detail='',finished_at=NULL,updated_at=? WHERE id=? AND state IN ('pending','failed_retryable')`,
				in.InstanceID, in.Action, in.DesiredGeneration, in.DesiredSpecHash, in.DesiredRevisionID, string(in.DesiredSpecJSON), boolInt(in.RemoveData), in.ForceNonce,
				in.Priority, in.IntentID, in.TriggerType, in.TriggerResourceType, in.TriggerResourceID, in.Reason, now, active.ID); err != nil {
				return PlanResult{}, err
			}
			active, err = scanJobTx(ctx, tx, active.ID)
			if err != nil {
				return PlanResult{}, err
			}
			traceJobEvent("job_merged", active, zap.String("reason", "active_pending_or_retryable_reused"))
		} else if active.State == JobRunning && in.ForceNonce > active.ForceNonce {
			// Keep the in-flight execution fenced by its original snapshot, but
			// leave the newer force intent on the same conflict-domain row. The
			// worker will observe the nonce mismatch after its RPC and requeue
			// the current desired state instead of losing a restart request.
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET force_nonce=?,updated_at=? WHERE id=? AND state='running'`, in.ForceNonce, now, active.ID); err != nil {
				return PlanResult{}, err
			}
			active.ForceNonce = in.ForceNonce
			traceJobEvent("job_merged", active, zap.String("reason", "running_force_nonce_bumped"))
		}
		return PlanResult{Job: active, Merged: true}, nil
	}
	job := Job{ID: id.New("job"), ApplicationID: in.ApplicationID, ServerID: in.ServerID, InstanceID: in.InstanceID,
		Action: in.Action, DesiredGeneration: in.DesiredGeneration, DesiredSpecHash: in.DesiredSpecHash, DesiredRevisionID: in.DesiredRevisionID,
		DesiredSpecJSON: in.DesiredSpecJSON, RemoveData: in.RemoveData, ForceNonce: in.ForceNonce, State: JobPending, Priority: in.Priority,
		IntentID: in.IntentID, TriggerType: in.TriggerType, TriggerResourceType: in.TriggerResourceType, TriggerResourceID: in.TriggerResourceID,
		Reason: in.Reason, IdempotencyKey: in.IdempotencyKey, CreatedAt: p.store.now().UTC(), UpdatedAt: p.store.now().UTC()}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs(id,application_id,server_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,remove_data,force_nonce,state,priority,attempts,next_run_at,lease_owner,lease_token,lease_expires_at,execution_id,intent_id,trigger_type,trigger_resource_type,trigger_resource_id,reason,idempotency_key,last_stage,last_steps_json,error_code,error_class,error_message,error_detail,created_at,started_at,finished_at,updated_at)
		VALUES(
			?,?,?,?,?,?,?,?,?, ?,
			?,?,?,?,?,?,?,?,?, ?,
			?,?,?,?,?,?,?,?,?, ?,
			?,?,?,?,?
		)`,
		job.ID, job.ApplicationID, job.ServerID, job.InstanceID, job.Action, job.DesiredGeneration, job.DesiredSpecHash, job.DesiredRevisionID,
		string(job.DesiredSpecJSON), boolInt(job.RemoveData), job.ForceNonce, job.State, job.Priority, 0, nil, "", "", nil, "", job.IntentID,
		job.TriggerType, job.TriggerResourceType, job.TriggerResourceID, job.Reason, job.IdempotencyKey, "", "[]", "", "", "", "", now, nil, nil, now); err != nil {
		return PlanResult{}, err
	}
	traceJobEvent("job_created", job, zap.String("reason", "no_active_job"))
	return PlanResult{Job: job, Created: true}, nil
}

func validatePlanInput(in PlanInput) error {
	if strings.TrimSpace(in.ApplicationID) == "" || strings.TrimSpace(in.ServerID) == "" {
		return &ValidationError{Message: "application and server are required"}
	}
	if in.Action != "" && in.Action != ActionApply && in.Action != ActionStop && in.Action != ActionPurge {
		return &ValidationError{Message: "unsupported reconcile action"}
	}
	return nil
}

func actionForDesired(desired string) string {
	if desired == DesiredPurged {
		return ActionPurge
	}
	if desired == DesiredStopped {
		return ActionStop
	}
	return ActionApply
}

func desiredForAction(action string) string {
	if action == ActionPurge {
		return DesiredPurged
	}
	if action == ActionStop {
		return DesiredStopped
	}
	return DesiredRunning
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func scanActiveJob(ctx context.Context, tx *sql.Tx, appID, serverID string) (Job, bool, error) {
	return scanJobPredicate(ctx, tx, `application_id=? AND server_id=? AND state IN ('pending','running','failed_retryable')`, appID, serverID)
}

func scanExistingJob(ctx context.Context, tx *sql.Tx, appID, key string) (Job, bool, error) {
	return scanJobPredicate(ctx, tx, `application_id=? AND idempotency_key<>'' AND idempotency_key=?`, appID, key)
}

func scanJobPredicate(ctx context.Context, tx *sql.Tx, predicate string, args ...any) (Job, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE `+predicate+` ORDER BY created_at DESC,id DESC LIMIT 1`, args...)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	return job, err == nil, err
}

func scanJobTx(ctx context.Context, tx *sql.Tx, jobID string) (Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id=?`, jobID))
}

var ErrStoreUnavailable = errors.New("orchestrator store unavailable")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
