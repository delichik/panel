package tasks

import (
	"context"
	"time"
)

// PauseRuntimeWriters drains manager work and collectors, including manually
// dispatched goroutines outside the worker loops. Legacy asynchronous executors
// retain their execution registration until completion; do not delete beneath
// them if they cannot finish within the maintenance caller's deadline.
func (w *Worker) PauseRuntimeWriters(ctx context.Context) error {
	if w == nil || w.service == nil {
		return nil
	}
	w.lifecycleMu.Lock()
	w.maintenanceWasRunning = w.running
	w.lifecycleMu.Unlock()
	if err := w.service.maintenance.PauseContext(ctx); err != nil {
		return err
	}
	w.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for w.service.RunningExecutionCount() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	return nil
}

func (w *Worker) ResumeRuntimeWriters(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	w.service.maintenance.Resume()
	w.lifecycleMu.Lock()
	wasRunning := w.maintenanceWasRunning
	w.maintenanceWasRunning = false
	w.lifecycleMu.Unlock()
	if wasRunning {
		w.Start(ctx)
	}
}
