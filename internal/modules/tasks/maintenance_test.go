package tasks

import (
	"context"
	"testing"
)

func TestRuntimeMaintenanceRestoresOriginalWorkerState(t *testing.T) {
	for _, started := range []bool{false, true} {
		s := newTestService(t)
		w := NewWorker(s)
		if started {
			w.Start(context.Background())
		}
		if err := w.PauseRuntimeWriters(context.Background()); err != nil {
			t.Fatal(err)
		}
		if w.TaskRuntime().WorkerRunning {
			t.Fatal("worker not paused")
		}
		if _, ok := s.maintenance.Enter(); ok {
			t.Fatal("task writes not paused")
		}
		w.ResumeRuntimeWriters(context.Background())
		if w.TaskRuntime().WorkerRunning != started {
			t.Fatal("maintenance changed original worker state")
		}
		w.Stop()
	}
}
