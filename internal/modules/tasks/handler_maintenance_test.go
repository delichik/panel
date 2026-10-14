package tasks

import (
	"context"
	"errors"
	"testing"
)

type maintenanceDispatchRunner func(context.Context, Task) error

func (run maintenanceDispatchRunner) RunNow(ctx context.Context, task Task) error {
	return run(ctx, task)
}

func TestDispatchRunNowSkipsExecutionAndFailureDuringMaintenance(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := NewHandler(svc, maintenanceDispatchRunner(func(context.Context, Task) error {
		called = true
		return errors.New("dispatch failed")
	}))
	svc.maintenance.Pause()
	defer svc.maintenance.Resume()
	handler.dispatchRunNow(task)
	if called {
		t.Fatal("paused dispatch invoked the runner")
	}
	latest, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != StatusQueued {
		t.Fatalf("paused dispatch changed task status: %s", latest.Status)
	}
}

func TestDispatchRunNowDrainsFailureBookkeepingBeforeMaintenance(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.maintenance.Resume()
	handler := NewHandler(svc, maintenanceDispatchRunner(func(context.Context, Task) error {
		// Start maintenance while dispatch is in flight. A cancelled deadline
		// must report an undrained writer, rather than allow deletion to begin.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := svc.maintenance.PauseContext(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("maintenance did not wait for dispatch: %v", err)
		}
		return errors.New("dispatch failed")
	}))
	handler.dispatchRunNow(task)
	if err := svc.maintenance.PauseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	latest, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != StatusFailed {
		t.Fatalf("dispatch failure was not persisted before drain: %s", latest.Status)
	}
}
