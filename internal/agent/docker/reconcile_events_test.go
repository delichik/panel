package docker

import (
	"context"
	"errors"
	"testing"

	contract "panel/internal/agent/contract"
)

type collectingSink struct {
	events   []contract.ExecutionEvent
	failType string
}

func (s *collectingSink) Append(_ context.Context, e contract.ExecutionEvent) error {
	if e.EventType == s.failType {
		return errors.New("disk full")
	}
	s.events = append(s.events, e)
	return nil
}
func TestStepIntentFailurePreventsSideEffect(t *testing.T) {
	sink := &collectingSink{failType: "step.started"}
	r := &reconcileRecorder{ctx: contract.WithExecutionEventSink(context.Background(), sink), executionID: "exec"}
	called := false
	if err := r.do("create_container", func(context.Context) error { called = true; return nil }); err == nil {
		t.Fatal("expected persistence error")
	}
	if called {
		t.Fatal("side effect ran without a durable intent")
	}
}
func TestStepsCaptureRealOrderAndMeasuredTimes(t *testing.T) {
	sink := &collectingSink{}
	r := &reconcileRecorder{ctx: contract.WithExecutionEventSink(context.Background(), sink), executionID: "exec"}
	if err := r.do("create_container", func(context.Context) error {
		if len(sink.events) != 1 || sink.events[0].EventType != "step.started" {
			t.Fatal("intent was not recorded before mutation")
		}
		return errors.New("create failed")
	}); err == nil {
		t.Fatal("failure not returned")
	}
	if len(sink.events) != 2 || sink.events[1].EventType != "step.finished" || sink.events[1].Status != "failed" {
		t.Fatalf("events: %+v", sink.events)
	}
	step := r.steps[0]
	if step.StartedAt == nil || step.FinishedAt == nil || step.FinishedAt.Before(*step.StartedAt) || sink.events[0].StepID != sink.events[1].StepID {
		t.Fatalf("step: %+v", step)
	}
}
