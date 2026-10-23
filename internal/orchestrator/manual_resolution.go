package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logcontexts"
	"panel/internal/platform/logging"
)

func (s *Store) GetJobByExecutionID(ctx context.Context, executionID string) (Job, error) {
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE execution_id=?`, executionID))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, panelerr.NotFound("execution")
	}
	return job, err
}

// ResolveUncertainManually records the human declaration without inventing a
// runtime observation. Its CAS protects the original execution/version and
// keeps all prior uncertainty and failed attempts intact.
func (s *Store) ResolveUncertainManually(ctx context.Context, job Job, outcome, reason string, actor activitylog.Actor) (bool, error) {
	reason = strings.TrimSpace(reason)
	if actor.Kind != "user" || actor.ID == "" {
		return false, panelerr.Forbidden("execution_verification_requires_user", "An authenticated user must verify this execution")
	}
	if reason == "" || (outcome != "succeeded" && outcome != "failed") {
		return false, panelerr.Validation("execution_verification_invalid", "A verification reason and a succeeded or failed outcome are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current, err := scanJobTx(ctx, tx, job.ID)
	if err != nil {
		return false, err
	}
	if current.State != JobRunning || current.ErrorClass != "uncertainty" || current.ExecutionID != job.ExecutionID || current.LeaseToken != job.LeaseToken || current.DesiredGeneration != job.DesiredGeneration || current.DesiredRevisionID != job.DesiredRevisionID || current.DesiredSpecHash != job.DesiredSpecHash {
		return false, panelerr.Conflict("execution_not_uncertain", "Only an execution awaiting verification can be resolved")
	}
	reason = activitylog.Redact(reason)
	resources := []activitylog.Resource{{Type: "application", ID: job.ApplicationID, Role: "subject", RevisionID: job.DesiredRevisionID, Generation: job.DesiredGeneration}, {Type: "server", ID: job.ServerID, Role: "target"}}
	logCtx := logging.Ensure(ctx, logging.Context{Module: "orchestrator", OperationID: job.IntentID, ExecutionID: job.ExecutionID})
	logging.From(logCtx).Info(reason,
		zap.String("eventType", "execution.verified"),
		zap.String("action", job.Action),
		zap.String("jobId", job.ID),
		zap.String("outcome", outcome),
		zap.String("verificationSource", "manual"),
		zap.String("actorId", actor.ID),
	)
	_ = logging.RecordContext(logCtx, logging.ContextMeta{
		Origin:        logcontexts.OriginUser,
		Title:         "execution.verified",
		Summary:       reason,
		Result:        outcome,
		HadError:      outcome == "failed",
		Finished:      true,
		ActorJSON:     logging.MarshalActorJSON(map[string]string{"kind": actor.Kind, "id": actor.ID, "name": actor.Name}),
		ResourcesJSON: logging.MarshalResourcesJSON(resources),
	})
	message := ""
	code := ""
	class := ""
	if outcome == "failed" {
		message = reason
		code = "manually_verified_failed"
		class = "manual_verification"
	}
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET state=?,last_stage='manually_verified',error_code=?,error_class=?,error_message=?,error_detail=?,lease_owner='',lease_token='',lease_expires_at=NULL,next_run_at=NULL,finished_at=?,updated_at=? WHERE id=?`, outcome, code, class, message, reason, s.now().UTC().Format(time.RFC3339Nano), s.now().UTC().Format(time.RFC3339Nano), job.ID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
