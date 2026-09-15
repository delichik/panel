package executionevents

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	contract "panel/internal/agent/contract"
)

func manualDecision(id, outcome string) contract.ExecutionResolution {
	return contract.ExecutionResolution{ExecutionID: id, Outcome: outcome, Reason: "Verified the persistent data and container on the node", ActorID: "admin-1", ActorName: "Administrator"}
}

func TestManualResolutionPreservesFactsAndReleasesUnknownFence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := openTest(t, dir)
	req := request("purge-execution")
	req.Action = "purge"
	req.RemoveData = true
	session, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().Add(-time.Minute).UTC()
	previous := contract.RuntimeReconcileResponse{ObservedState: "missing", ObservedAt: observedAt, ErrorCode: "purge_timeout", ErrorMessage: "not confirmed", Retryable: true}
	if err = session.Finish(ctx, previous, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	before, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Begin(ctx, request("next-execution")); !errors.Is(err, ErrInFlight) {
		t.Fatalf("unknown execution lost its fence: %v", err)
	}
	decision := manualDecision(req.ExecutionID, "succeeded")
	resolved, err := s.Resolve(ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != "finished" || resolved.EndSeq != before.HeadSeq+3 || resolved.Result.VerificationSource != "manual" || resolved.Result.VerifiedBy != decision.ActorID || resolved.Result.VerifiedAt == nil || resolved.Result.ErrorCode != "" {
		t.Fatalf("manual result: %+v", resolved)
	}
	if resolved.Result.ObservedState != previous.ObservedState || !resolved.Result.ObservedAt.Equal(observedAt) {
		t.Fatal("manual declaration fabricated a new observation")
	}
	after, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Events, after.Events[:len(before.Events)]) {
		t.Fatal("manual resolution rewrote previous facts")
	}
	kinds := []string{"execution.manually_verified", "execution.finished", "stream.closed"}
	for i, kind := range kinds {
		if after.Events[len(before.Events)+i].EventType != kind {
			t.Fatalf("missing manual fact: %+v", after.Events)
		}
	}
	if err = s.Ack(ctx, contract.ExecutionEventsAck{ExecutionID: req.ExecutionID, SourceID: after.SourceID, SourceEpoch: after.SourceEpoch, SourceStreamID: after.SourceStreamID, ThroughSourceSeq: after.EndSeq}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, dir)
	repeated, err := s.Resolve(ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, repeated) {
		t.Fatalf("idempotent resolution changed: %+v vs %+v", resolved, repeated)
	}
	decision.Reason = "A different declaration"
	if _, err = s.Resolve(ctx, decision); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed declaration should conflict: %v", err)
	}
	if _, _, err = s.Begin(ctx, request("next-execution")); err != nil {
		t.Fatalf("verified execution did not release fence: %v", err)
	}
}

func TestManualResolutionRejectsRunningAndRequiresIdentityReason(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	req := request("execution")
	session, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, manualDecision(req.ExecutionID, "failed")); !errors.Is(err, ErrResolutionState) {
		t.Fatalf("resolved active action: %v", err)
	}
	session.Abandon()
	for _, change := range []func(*contract.ExecutionResolution){func(in *contract.ExecutionResolution) { in.ActorID = "" }, func(in *contract.ExecutionResolution) { in.ActorName = "" }, func(in *contract.ExecutionResolution) { in.Reason = " " }, func(in *contract.ExecutionResolution) { in.Outcome = "retry" }} {
		in := manualDecision(req.ExecutionID, "failed")
		change(&in)
		if _, err = s.Resolve(ctx, in); !errors.Is(err, ErrResolutionInvalid) {
			t.Fatalf("invalid declaration accepted: %v", err)
		}
	}
	result, err := s.Resolve(ctx, manualDecision(req.ExecutionID, "failed"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.ObservedState != "unknown" || !result.Result.ObservedAt.IsZero() || result.Result.ErrorCode != "execution_manually_failed" || result.Result.Retryable {
		t.Fatalf("manual failure fabricated observation or retry: %+v", result.Result)
	}
}

func TestManualResolutionRollsBackAllFactsWhenCompletionCannotPersist(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	req := request("execution")
	session, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	session.Abandon()
	if _, err = s.db.Exec(`CREATE TRIGGER fail_resolution BEFORE INSERT ON events WHEN json_extract(NEW.body,'$.eventType')='execution.finished' BEGIN SELECT RAISE(ABORT,'evidence storage failed');END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve(ctx, manualDecision(req.ExecutionID, "succeeded")); err == nil {
		t.Fatal("expected persistence failure")
	}
	result, err := s.Result(ctx, req.ExecutionID)
	if err != nil || result.State != "unknown" || result.EndSeq != 0 {
		t.Fatalf("partial result committed: %+v %v", result, err)
	}
	page, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID})
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("partial manual facts committed: %+v %v", page, err)
	}
	if _, _, err = s.Begin(ctx, request("next")); !errors.Is(err, ErrInFlight) {
		t.Fatalf("write failure released fence: %v", err)
	}
}
