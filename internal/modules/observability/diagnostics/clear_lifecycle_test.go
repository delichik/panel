package diagnostics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func awaitClearFinished(t *testing.T, s *Service) ClearRuntimeDataResult {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if status := s.ClearRuntimeDataStatus(); !status.Running {
			return status
		}
		select {
		case <-deadline:
			t.Fatal("clear did not finish")
		case <-tick.C:
		}
	}
}

// DIAG-CLR-001: retries while running share the same durable-in-process run
// identity and status; a page reload can read progress without creating work.
func TestClearConcurrentRequestSharesRunAndProgress(t *testing.T) {
	s := NewService()
	started, release := make(chan struct{}), make(chan struct{})
	s.SetClearRuntimeDataHook(func(ctx context.Context) (ClearRuntimeDataResult, error) {
		ReportClearRuntimeDataProgress(ctx, "clearing_logs", false)
		close(started)
		<-release
		return ClearRuntimeDataResult{Cleared: true}, nil
	})
	first, err := s.StartClearRuntimeData()
	if err != nil {
		t.Fatal(err)
	}
	<-started
	second, err := s.StartClearRuntimeData()
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == "" || first.RunID != second.RunID || second.Stage != "clearing_logs" || second.StartedAt == nil {
		t.Fatalf("run identity/progress lost: first=%+v second=%+v", first, second)
	}
	close(release)
	finished := awaitClearFinished(t, s)
	if finished.RunID != first.RunID || finished.Status != "succeeded" || finished.Stage != "completed" || finished.FinishedAt == nil || !finished.Cleared {
		t.Fatalf("invalid final status: %+v", finished)
	}
}

func TestClearFailureRetainsLogicalOutcomeAndFailedStage(t *testing.T) {
	for _, test := range []struct {
		name        string
		cleared     bool
		stage, code string
	}{
		{"partial deletion", false, "clearing_logs", "clear_runtime_data_failed"},
		{"worker restart", true, "resuming_workers", "clear_runtime_data_resume_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := NewService()
			s.SetClearRuntimeDataHook(func(ctx context.Context) (ClearRuntimeDataResult, error) {
				ReportClearRuntimeDataProgress(ctx, "resuming_workers", test.cleared)
				return ClearRuntimeDataResult{Cleared: test.cleared, FailedStage: test.stage, Error: test.code}, errors.New("database /private/path password=secret")
			})
			if _, err := s.StartClearRuntimeData(); err != nil {
				t.Fatal(err)
			}
			got := awaitClearFinished(t, s)
			if got.Status != "failed" || got.Cleared != test.cleared || got.FailedStage != test.stage || got.Error != test.code || strings.Contains(got.Error, "secret") {
				t.Fatalf("unsafe or incorrect status: %+v", got)
			}
		})
	}
}

func TestClearPanicTerminatesRunAndAllowsRetry(t *testing.T) {
	s := NewService()
	s.SetClearRuntimeDataHook(func(ctx context.Context) (ClearRuntimeDataResult, error) {
		ReportClearRuntimeDataProgress(ctx, "clearing_coordination", false)
		panic("private failure")
	})
	first, err := s.StartClearRuntimeData()
	if err != nil {
		t.Fatal(err)
	}
	got := awaitClearFinished(t, s)
	if got.Status != "failed" || got.FailedStage != "clearing_coordination" {
		t.Fatalf("panic left run active: %+v", got)
	}
	s.SetClearRuntimeDataHook(func(context.Context) (ClearRuntimeDataResult, error) {
		return ClearRuntimeDataResult{Cleared: true}, nil
	})
	second, err := s.StartClearRuntimeData()
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == second.RunID {
		t.Fatal("retry reused finished run identity")
	}
	if got := awaitClearFinished(t, s); got.Status != "succeeded" {
		t.Fatalf("retry failed: %+v", got)
	}
}
