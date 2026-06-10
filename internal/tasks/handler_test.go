package tasks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordingRunner struct {
	task Task
}

func (r *recordingRunner) RunNow(ctx context.Context, task Task) error {
	r.task = task
	return nil
}

type capabilityRunner struct {
	recordingRunner
	allow bool
}

func (r *capabilityRunner) CanRun(task Task) bool {
	return r.allow
}

func TestHandlerRunNowDispatchesQueuedTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "server_connectivity_test", ServerID: "srv_1"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	handler := NewHandler(svc, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/run-now", nil)
	rec := httptest.NewRecorder()

	handler.RunNow(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
	}
	if runner.task.ID != task.ID {
		t.Fatalf("expected runner to receive task %s, got %#v", task.ID, runner.task)
	}
	if runner.task.Status != StatusQueued {
		t.Fatalf("expected queued task, got %#v", runner.task)
	}
}

func TestHandlerRunNowRejectsUnsupportedTaskType(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "application_deploy", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	runner := &capabilityRunner{allow: false}
	handler := NewHandler(svc, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/run-now", nil)
	rec := httptest.NewRecorder()

	handler.RunNow(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected unsupported task to be rejected, got %d", rec.Code)
	}
	if runner.task.ID != "" {
		t.Fatalf("runner should not receive unsupported task, got %#v", runner.task)
	}
}

func TestHandlerRetryDispatchesRunnableFailedTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "package_refresh", ServerID: "srv_1", ResourceType: "server", ResourceID: "srv_1", Status: StatusFailed})
	if err != nil {
		t.Fatal(err)
	}
	runner := &capabilityRunner{allow: true}
	handler := NewHandler(svc, runner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry", nil)
	rec := httptest.NewRecorder()

	handler.Retry(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected retry to be accepted, got %d", rec.Code)
	}
	if runner.task.ID == "" || runner.task.ID == task.ID || runner.task.TriggerTaskID != task.ID {
		t.Fatalf("expected runner to receive retry task, got %#v", runner.task)
	}
}

func TestHandlerRetryRejectsNonFailedTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "package_refresh", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc, &capabilityRunner{allow: true})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry", nil)
	rec := httptest.NewRecorder()

	handler.Retry(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected queued retry to be rejected, got %d", rec.Code)
	}
}
