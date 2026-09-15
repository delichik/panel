package executionevents

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	contract "panel/internal/agent/contract"
)

func request(id string) contract.RuntimeReconcileRequest {
	return contract.RuntimeReconcileRequest{ExecutionID: id, OperationID: "operation", RunID: "run", JobID: "job", InstanceID: "instance", ApplicationID: "app", ServerID: "server", Action: "apply", DesiredGeneration: 4, DesiredSpecHash: "hash"}
}
func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDurableReplayAndAckPreserveResultIdentity(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "spool")
	s := openTest(t, dir)
	req := request("execution")
	run, old, err := s.Begin(ctx, req)
	if err != nil || old != nil {
		t.Fatalf("begin: %v %v", old, err)
	}
	for i := 0; i < 230; i++ {
		if err = run.Append(ctx, contract.ExecutionEvent{EventType: "output.chunk", Stream: "stdout", Text: "a line"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = run.Finish(ctx, contract.RuntimeReconcileResponse{ObservedState: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	first, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 200 || !first.HasMore || first.EndSeq != 233 {
		t.Fatalf("page: %+v", first)
	}
	replay, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Events[7] != first.Events[7] {
		t.Fatal("retransmission changed original event")
	}
	badAck := contract.ExecutionEventsAck{ExecutionID: req.ExecutionID, SourceID: first.SourceID, SourceEpoch: "other", SourceStreamID: first.SourceStreamID, ThroughSourceSeq: 200}
	if err = s.Ack(ctx, badAck); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong epoch ACK: %v", err)
	}
	badAck.SourceEpoch = first.SourceEpoch
	if err = s.Ack(ctx, badAck); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, dir)
	tail, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID, SourceEpoch: first.SourceEpoch, SourceStreamID: first.SourceStreamID, AfterSourceSeq: 200, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Events) != 33 || tail.HasMore || tail.AckedThroughSeq != 200 || tail.Events[0].SourceSeq != 201 {
		t.Fatalf("tail: %+v", tail)
	}
	if tail.SourceID != first.SourceID {
		t.Fatal("source identity changed after restart")
	}
	run, old, err = s.Begin(ctx, req)
	if err != nil || run != nil || old.State != "finished" || old.Result.ObservedState != "running" {
		t.Fatalf("replay result: %+v %v", old, err)
	}
	req.DesiredGeneration++
	if _, _, err = s.Begin(ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed request: %v", err)
	}
}

func TestRestartDoesNotRepeatUnfinishedSideEffect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := openTest(t, dir)
	req := request("execution")
	run, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.Append(ctx, contract.ExecutionEvent{EventType: "step.started", StepID: "step", StepName: "create_container"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, dir)
	result, err := s.Result(ctx, req.ExecutionID)
	if err != nil || result.State != "unknown" {
		t.Fatalf("recovered result: %+v %v", result, err)
	}
	if run, old, err := s.Begin(ctx, req); err != nil || run != nil || old.State != "unknown" {
		t.Fatalf("duplicate: %+v %v", old, err)
	}
	if _, _, err := s.Begin(ctx, request("different")); !errors.Is(err, ErrInFlight) {
		t.Fatalf("conflicting side effect admitted: %v", err)
	}
}

func TestOutputChunksRetainUnicodeAndRedactBeforeSplit(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	req := request("execution")
	req.Spec.Env = map[string]string{"TOKEN": "a-secret-across-a-chunk-boundary"}
	run, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("界", 4090) + req.Spec.Env["TOKEN"] + strings.Repeat("界", 9000)
	if err = run.Append(ctx, contract.ExecutionEvent{EventType: "output.chunk", Text: text, Stream: "stderr"}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	var reconstructed strings.Builder
	for _, event := range page.Events {
		if event.EventType == "output.chunk" {
			reconstructed.WriteString(event.Text)
			if len([]byte(event.Text)) > 16384 {
				t.Fatal("oversized text chunk")
			}
		}
	}
	expected := strings.ReplaceAll(text, req.Spec.Env["TOKEN"], "[REDACTED]")
	if reconstructed.String() != expected {
		t.Fatal("output lost characters or leaked the secret")
	}
}

func TestCancelledExecutionRemainsFencedAfterDurableResult(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	req := request("execution")
	run, _, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.Finish(ctx, contract.RuntimeReconcileResponse{ErrorCode: "start_failed"}, context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	result, err := s.Result(ctx, req.ExecutionID)
	if err != nil || result.State != "unknown" || result.EndSeq != 0 {
		t.Fatalf("result: %+v %v", result, err)
	}
	if _, _, err = s.Begin(ctx, request("new")); err == nil {
		t.Fatal("unknown effect lost its conflict fence")
	}
}
