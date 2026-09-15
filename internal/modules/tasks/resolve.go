package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"panel/internal/platform/activitylog"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

// ResolveUncertain records an authenticated human's verification. It does not
// claim that Panel independently observed the remote result.
func (s *Service) ResolveUncertain(ctx context.Context, taskID, outcome, reason string) (Task, error) {
	actor := activitylog.ActorFromContext(ctx)
	if actor.Kind != "user" || actor.ID == "" {
		return Task{}, panelerr.Forbidden("execution_verification_requires_user", "An authenticated user must verify this execution")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || (outcome != "succeeded" && outcome != "failed") {
		return Task{}, panelerr.Validation("execution_verification_invalid", "A verification reason and a succeeded or failed outcome are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	var row taskRow
	if err := orm.New(tx).From("tasks").Where("id = ?", taskID).First(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, panelerr.NotFound("execution")
		}
		return Task{}, err
	}
	if row.Status != StatusRunning || row.Stage != "uncertain" {
		return Task{}, panelerr.Conflict("execution_not_uncertain", "Only an execution awaiting verification can be resolved")
	}
	status := StatusFailed
	result := "failed"
	if outcome == "succeeded" {
		status = StatusCompleted
		result = "succeeded"
	}
	reason = activitylog.Redact(reason)
	_, err = activitylog.AppendTx(ctx, tx, []activitylog.EventInput{{EventType: "execution.verified", Kind: "observation", Domain: firstNonEmpty(row.ResourceType, "system"), Action: row.Type, OperationID: row.OperationID, RunID: row.ID, Actor: actor, Initiator: actor, Text: reason, Data: map[string]any{"taskId": row.ID, "verificationSource": "manual", "outcome": result, "reason": reason}}})
	if err != nil {
		return Task{}, err
	}
	resultSQL, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?,stage='verified',error=CASE WHEN ?='completed' THEN '' ELSE ? END,finished_at=?,next_run_at=NULL WHERE id=? AND status='running' AND stage='uncertain'`, status, status, reason, time.Now().UTC().Format(time.RFC3339Nano), taskID)
	if err != nil {
		return Task{}, err
	}
	n, err := resultSQL.RowsAffected()
	if err != nil {
		return Task{}, err
	}
	if n != 1 {
		return Task{}, panelerr.Conflict("execution_not_uncertain", "Only an execution awaiting verification can be resolved")
	}
	if err := tx.Commit(); err != nil {
		return Task{}, err
	}
	s.invalidateFirstActiveByKey(row.ConcurrencyKey)
	return s.Get(ctx, taskID)
}

func (h *Handler) ResolveUncertain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		httpx.Error(w, panelerr.Validation("execution_verification_invalid", "A verification reason and a succeeded or failed outcome are required"))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		httpx.Error(w, panelerr.Validation("execution_verification_invalid", "A verification reason and a succeeded or failed outcome are required"))
		return
	}
	executionID := taskIDFromRequest(r)
	if _, err := h.service.Get(r.Context(), executionID); err != nil {
		var pe *panelerr.Error
		if errors.As(err, &pe) && pe.HTTPStatus == http.StatusNotFound && h.externalResolver != nil {
			value, resolveErr := h.externalResolver(r.Context(), executionID, input.Outcome, input.Reason)
			if resolveErr != nil {
				httpx.Error(w, resolveErr)
				return
			}
			httpx.JSON(w, http.StatusOK, value)
			return
		}
		httpx.Error(w, err)
		return
	}
	task, err := h.service.ResolveUncertain(r.Context(), taskIDFromRequest(r), input.Outcome, input.Reason)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	h.decorateTask(&task)
	httpx.JSON(w, http.StatusOK, task)
}
