package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWorkerRunNowUsesManagerLifecycle(t *testing.T) {
	svc := newTestService(t)
	failureHookCalled := false
	svc.MustRegister(Definition{
		Type:        "worker_failure",
		AllowRunNow: true,
		AllowRetry:  true,
		Execute: func(TaskContext) error {
			return errors.New("worker failed")
		},
		OnFailure: func(_ context.Context, _ Task, cause error) error {
			failureHookCalled = cause != nil
			return nil
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "worker_failure"})
	if err != nil {
		t.Fatal(err)
	}

	if err := NewWorker(svc).RunNow(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "worker failed") || !failureHookCalled {
		t.Fatalf("expected manager failure lifecycle, task=%#v hook=%v", got, failureHookCalled)
	}
	if svc.HasRunningExecution(task.ID) {
		t.Fatal("finished task should not retain a running execution")
	}
}

func TestWorkerRunsDueTaskWithoutCreatingInternalTaskRecord(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{
		Type: "due_task",
		Execute: func(tc TaskContext) error {
			return tc.Log("system", "ran due task")
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "due_task"})
	if err != nil {
		t.Fatal(err)
	}
	worker := NewWorker(svc)

	worker.runDueTasks(context.Background())

	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("expected due task to complete, got %#v", got)
	}
	result, err := svc.List(context.Background(), ListFilter{IncludeInternal: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != task.ID {
		t.Fatalf("expected no internal queue-drain task record, got %#v", result.Items)
	}
	if _, exists := svc.Registry().Definition("task_queue_drain"); exists {
		t.Fatal("task_queue_drain must not be registered")
	}
}

func TestWorkerLeavesFutureRetryTaskQueued(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{
		Type: "future_retry",
		Execute: func(TaskContext) error {
			return nil
		},
	})
	nextRun := time.Now().UTC().Add(time.Hour)
	task, err := svc.Create(context.Background(), CreateInput{
		Type:      "future_retry",
		Status:    StatusFailedRetryable,
		NextRunAt: &nextRun,
	})
	if err != nil {
		t.Fatal(err)
	}

	NewWorker(svc).runDueTasks(context.Background())

	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailedRetryable {
		t.Fatalf("future retry task should not run, got %#v", got)
	}
}

func TestWorkerRunNowDoesNotDuplicateActiveExecution(t *testing.T) {
	svc := newTestService(t)
	runs := 0
	svc.MustRegister(Definition{
		Type: "active_task",
		Execute: func(TaskContext) error {
			runs++
			return nil
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "active_task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}

	if err := NewWorker(svc).RunNow(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	if runs != 0 {
		t.Fatalf("active task executor ran %d times", runs)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning || !svc.HasRunningExecution(task.ID) {
		t.Fatalf("active execution should remain owned, got %#v", got)
	}
}

func TestWorkerExpiresRegisteredStaleQueuedTypes(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{Type: "stale_worker", StaleQueuedAfter: time.Minute})
	task, err := svc.Create(context.Background(), CreateInput{Type: "stale_worker"})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`UPDATE tasks SET created_at=? WHERE id=?`, old, task.ID); err != nil {
		t.Fatal(err)
	}

	NewWorker(svc).expireStaleQueued(context.Background())

	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "worker startup timeout") {
		t.Fatalf("expected stale queued task to fail, got %#v", got)
	}
}

func TestWorkerFailsOrphanedRunningTasks(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE tasks SET status=? WHERE id=?`, StatusRunning, task.ID); err != nil {
		t.Fatal(err)
	}

	NewWorker(svc).failOrphanedRunning(context.Background())

	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "no active execution") {
		t.Fatalf("expected orphaned task to fail, got %#v", got)
	}
}
