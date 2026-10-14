package diagnostics

import (
	"context"
	"errors"
	"time"

	id "panel/internal/platform/identity"
)

// Cleared describes logical deletion only. It may be true on a failed run when
// deletion completed but workers could not be resumed. A failed run can also
// have committed earlier batches even when Cleared is false.
type ClearRuntimeDataResult struct {
	RunID       string     `json:"runId,omitempty"`
	Cleared     bool       `json:"cleared"`
	Running     bool       `json:"running"`
	Status      string     `json:"status"`
	Stage       string     `json:"stage,omitempty"`
	FailedStage string     `json:"failedStage,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Error       string     `json:"errorCode,omitempty"`
}

type clearProgressKey struct{}

// ReportClearRuntimeDataProgress accepts stable phase identifiers only. The
// callback belongs to one run, so late progress cannot alter a later request.
func ReportClearRuntimeDataProgress(ctx context.Context, stage string, cleared bool) {
	switch stage {
	case "stopping_workers", "clearing_coordination", "clearing_logs", "clearing_metrics", "compacting", "resuming_workers":
	default:
		return
	}
	if report, ok := ctx.Value(clearProgressKey{}).(func(string, bool)); ok {
		report(stage, cleared)
	}
}

func (s *Service) SetClearRuntimeDataHook(hook func(context.Context) (ClearRuntimeDataResult, error)) {
	s.clearMu.Lock()
	defer s.clearMu.Unlock()
	s.clearHook = hook
}

func (s *Service) StartClearRuntimeData() (ClearRuntimeDataResult, error) {
	s.clearMu.Lock()
	if s.clearHook == nil {
		s.clearMu.Unlock()
		return ClearRuntimeDataResult{}, errors.New("runtime data clearing is unavailable")
	}
	if s.clearStatus.Running {
		result := s.clearStatus
		s.clearMu.Unlock()
		return result, nil
	}
	now := time.Now().UTC()
	initial := ClearRuntimeDataResult{RunID: id.New("clear"), Running: true, Status: "running", Stage: "stopping_workers", StartedAt: &now}
	s.clearStatus = initial
	hook := s.clearHook
	s.clearMu.Unlock()
	ctx := context.WithValue(context.Background(), clearProgressKey{}, func(stage string, cleared bool) {
		s.clearMu.Lock()
		defer s.clearMu.Unlock()
		if s.clearStatus.RunID == initial.RunID && s.clearStatus.Running {
			s.clearStatus.Stage = stage
			s.clearStatus.Cleared = s.clearStatus.Cleared || cleared
		}
	})
	go s.runClearRuntimeData(ctx, initial.RunID, hook)
	return initial, nil
}

func (s *Service) runClearRuntimeData(ctx context.Context, runID string, hook func(context.Context) (ClearRuntimeDataResult, error)) {
	var result ClearRuntimeDataResult
	var err error
	defer func() {
		if recover() != nil {
			err = errors.New("runtime data clearing panicked")
		}
		s.clearMu.Lock()
		defer s.clearMu.Unlock()
		if s.clearStatus.RunID != runID {
			return
		}
		finished := time.Now().UTC()
		s.clearStatus.Running = false
		s.clearStatus.FinishedAt = &finished
		s.clearStatus.Cleared = s.clearStatus.Cleared || result.Cleared
		if err != nil || !s.clearStatus.Cleared {
			s.clearStatus.Status = "failed"
			s.clearStatus.Error = "clear_runtime_data_failed"
			if result.Error == "clear_runtime_data_resume_failed" {
				s.clearStatus.Error = result.Error
			}
			s.clearStatus.FailedStage = result.FailedStage
			if s.clearStatus.FailedStage == "" {
				s.clearStatus.FailedStage = s.clearStatus.Stage
			}
			return
		}
		s.clearStatus.Status = "succeeded"
		s.clearStatus.Stage = "completed"
	}()
	result, err = hook(ctx)
}

func (s *Service) ClearRuntimeDataStatus() ClearRuntimeDataResult {
	s.clearMu.Lock()
	defer s.clearMu.Unlock()
	if s.clearStatus.Status == "" {
		return ClearRuntimeDataResult{Status: "idle"}
	}
	return s.clearStatus
}
