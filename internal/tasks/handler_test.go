package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/httpx"
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
	runner := &capabilityRunner{}
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

func TestHandlerRunNowRejectsDefinitionWithoutCapability(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "application_deploy", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	runner := &capabilityRunner{}
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
	runner := &capabilityRunner{}
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
	handler := NewHandler(svc, &capabilityRunner{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry", nil)
	rec := httptest.NewRecorder()

	handler.Retry(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected queued retry to be rejected, got %d", rec.Code)
	}
}

func TestHandlerListParsesMultiValueFilters(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	running, err := svc.Create(ctx, CreateInput{Type: "application_deploy", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := svc.Create(ctx, CreateInput{Type: "package_refresh", Status: StatusFailed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateInput{Type: "application_restart", Status: StatusCompleted}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=running&status=failed&type=application_deploy&type=package_refresh&includeInternal=true", nil)
	rec := httptest.NewRecorder()

	handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list to succeed, got %d", rec.Code)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result ListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Items) != 2 {
		t.Fatalf("expected two filtered tasks, got %#v", result)
	}
	ids := map[string]bool{result.Items[0].ID: true, result.Items[1].ID: true}
	if !ids[running.ID] || !ids[failed.ID] {
		t.Fatalf("unexpected filtered task ids: %#v", result.Items)
	}
}
