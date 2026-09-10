package executionevents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	contract "panel/internal/agent/contract"
)

var ErrResolutionInvalid = errors.New("execution resolution requires succeeded or failed, a reason, and authenticated actor identity")
var ErrResolutionState = errors.New("only an unknown execution can be manually resolved")

// Resolve records a human decision atomically with stream closure and the
// result used by future retries. It never fabricates Docker observations and
// never clears an instance fence unless every new fact has committed.
func (s *Store) Resolve(ctx context.Context, in contract.ExecutionResolution) (contract.ExecutionResult, error) {
	in.ExecutionID = strings.TrimSpace(in.ExecutionID)
	in.Outcome = strings.TrimSpace(in.Outcome)
	in.Reason = strings.TrimSpace(in.Reason)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.ActorName = strings.TrimSpace(in.ActorName)
	if in.ExecutionID == "" || (in.Outcome != "succeeded" && in.Outcome != "failed") || in.Reason == "" || in.ActorID == "" || in.ActorName == "" || len(in.Reason) > 16384 || len(in.ActorID) > 256 || len(in.ActorName) > 1024 || !utf8.ValidString(in.Reason) || !utf8.ValidString(in.ActorID) || !utf8.ValidString(in.ActorName) {
		return contract.ExecutionResult{}, ErrResolutionInvalid
	}
	canonical, err := json.Marshal(in)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	sum := sha256.Sum256(canonical)
	requestHash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE identity SET id=id WHERE id=1`); err != nil {
		return contract.ExecutionResult{}, err
	}
	var oldHash string
	err = tx.QueryRowContext(ctx, `SELECT request_hash FROM manual_resolutions WHERE execution_id=?`, in.ExecutionID).Scan(&oldHash)
	if err == nil {
		if oldHash != requestHash {
			return contract.ExecutionResult{}, ErrConflict
		}
		return s.resultTx(ctx, tx, in.ExecutionID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return contract.ExecutionResult{}, err
	}
	previous, err := s.resultTx(ctx, tx, in.ExecutionID)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	if previous.State == "missing" {
		return contract.ExecutionResult{}, ErrNotFound
	}
	if previous.State != "unknown" || s.active[in.ExecutionID] {
		return contract.ExecutionResult{}, ErrResolutionState
	}
	var safeRequest string
	if err = tx.QueryRowContext(ctx, `SELECT request_json FROM executions WHERE id=?`, in.ExecutionID).Scan(&safeRequest); err != nil {
		return contract.ExecutionResult{}, err
	}
	var request contract.RuntimeReconcileRequest
	if err = json.Unmarshal([]byte(safeRequest), &request); err != nil {
		return contract.ExecutionResult{}, err
	}
	session := &Session{store: s, request: request}
	now := time.Now().UTC()
	reason := session.Redact(in.Reason)
	result := contract.RuntimeReconcileResponse{ObservedState: "unknown"}
	if previous.Result != nil {
		result = *previous.Result
	}
	// ObservedAt/generation/container state remain exactly as last observed. The
	// declaration timestamp and author have their own explicit fields.
	if result.ObservedState == "" {
		result.ObservedState = "unknown"
	}
	result.VerificationSource = "manual"
	result.VerificationReason = reason
	result.VerifiedBy = in.ActorID
	result.VerifiedByName = in.ActorName
	result.VerifiedAt = &now
	result.Retryable = false
	result.RetryAfter = 0
	result.ErrorCode = ""
	result.ErrorClass = ""
	result.ErrorMessage = ""
	result.ErrorDetail = ""
	if in.Outcome == "failed" {
		result.ErrorCode = "execution_manually_failed"
		result.ErrorClass = "manual_verification"
		result.ErrorMessage = reason
	}
	declaration := map[string]any{"verificationSource": "manual", "outcome": in.Outcome, "reason": reason, "actorId": in.ActorID, "actorName": in.ActorName, "verifiedAt": now, "previousResult": previous.Result}
	declarationJSON, err := json.Marshal(declaration)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	if err = session.appendTx(ctx, tx, contract.ExecutionEvent{EventType: "execution.manually_verified", Status: in.Outcome, OccurredAt: now, Text: reason, DataJSON: string(declarationJSON)}); err != nil {
		return contract.ExecutionResult{}, err
	}
	data, err := json.Marshal(map[string]any{"result": result, "verificationSource": "manual", "outcome": in.Outcome})
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	if err = session.appendTx(ctx, tx, contract.ExecutionEvent{EventType: "execution.finished", Status: in.Outcome, OccurredAt: now, DataJSON: string(data)}); err != nil {
		return contract.ExecutionResult{}, err
	}
	var head int64
	if err = tx.QueryRowContext(ctx, `SELECT head_seq FROM executions WHERE id=?`, in.ExecutionID).Scan(&head); err != nil {
		return contract.ExecutionResult{}, err
	}
	closed, _ := json.Marshal(map[string]any{"endSeq": head + 1, "verificationSource": "manual"})
	if err = session.appendTx(ctx, tx, contract.ExecutionEvent{EventType: "stream.closed", OccurredAt: now, DataJSON: string(closed)}); err != nil {
		return contract.ExecutionResult{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO manual_resolutions(execution_id,request_hash,declaration_json,occurred_at) VALUES(?,?,?,?)`, in.ExecutionID, requestHash, string(declarationJSON), now.Format(time.RFC3339Nano)); err != nil {
		return contract.ExecutionResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE executions SET state='finished',end_seq=head_seq,result_json=?,error=? WHERE id=?`, string(raw), result.ErrorMessage, in.ExecutionID); err != nil {
		return contract.ExecutionResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return contract.ExecutionResult{}, err
	}
	return contract.ExecutionResult{State: "finished", Result: &result, Error: result.ErrorMessage, EndSeq: head + 1}, nil
}
