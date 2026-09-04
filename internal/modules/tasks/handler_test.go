package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpx "panel/internal/platform/http"
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

func waitForRunnerTask(t *testing.T, runner *recordingRunner, wantID string) Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runner.task.ID != "" && (wantID == "" || runner.task.ID == wantID) {
			return runner.task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("runner did not receive task (want id %q)", wantID)
	return Task{}
}

func serveTaskRoute(handler *Handler, method, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRunNowDispatchesQueuedTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "server_connectivity_test", ServerID: "srv_1"})
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	handler := NewHandler(svc, runner)

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/run-now")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected %d, got %d", http.StatusAccepted, rec.Code)
	}
	waitForRunnerTask(t, runner, task.ID)
	if runner.task.Status != StatusQueued {
		t.Fatalf("expected queued task, got %#v", runner.task)
	}
}

func TestHandlerRunNowRejectsUnsupportedTaskType(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	runner := &capabilityRunner{}
	handler := NewHandler(svc, runner)

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/run-now")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected unsupported task to be rejected, got %d", rec.Code)
	}
	if runner.task.ID != "" {
		t.Fatalf("runner should not receive unsupported task, got %#v", runner.task)
	}
}

func TestHandlerRunNowRejectsDefinitionWithoutCapability(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	runner := &capabilityRunner{}
	handler := NewHandler(svc, runner)

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/run-now")

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

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected retry to be accepted, got %d", rec.Code)
	}
	retried := waitForRunnerTask(t, &runner.recordingRunner, "")
	if retried.ID == "" || retried.ID == task.ID || retried.TriggerTaskID != task.ID {
		t.Fatalf("expected runner to receive retry task, got %#v", retried)
	}
}

func TestHandlerDecoratesAllowCancelFromDefinition(t *testing.T) {
	svc := newTestService(t)
	blocked, err := svc.Create(context.Background(), CreateInput{Type: "package_upgrade_selected", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := svc.Create(context.Background(), CreateInput{Type: "package_refresh", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc)

	rec := serveTaskRoute(handler, http.MethodGet, "/api/v1/tasks/"+blocked.ID)
	var response struct {
		Data Task `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	got := response.Data
	if got.AllowCancel {
		t.Fatalf("expected non-cancellable task to expose allowCancel=false, got %#v", got)
	}

	rec = serveTaskRoute(handler, http.MethodGet, "/api/v1/tasks/"+allowed.ID)
	response = struct {
		Data Task `json:"data"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	got = response.Data
	if !got.AllowCancel {
		t.Fatalf("expected cancellable task to expose allowCancel=true, got %#v", got)
	}
}

func TestHandlerRetryRejectsNonFailedTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "package_refresh", Status: StatusQueued})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc, &capabilityRunner{})

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected queued retry to be rejected, got %d", rec.Code)
	}
}

func TestHandlerListParsesMultiValueFilters(t *testing.T) {
	svc := newTestService(t)
	svc.Registry().Replace(Definition{
		Type:              "package_refresh",
		AllowRunNow:       true,
		AllowRetry:        true,
		Execute:           func(TaskContext) error { return nil },
		ConcurrencyPolicy: ConcurrencyResourceExclusive,
	})
	ctx := context.Background()
	running, err := svc.Create(ctx, CreateInput{Type: "sample_task", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := svc.Create(ctx, CreateInput{Type: "package_refresh", Status: StatusFailed})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CreateInput{Type: "sample_restart", Status: StatusCompleted}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?status=running&status=failed&type=sample_task&type=package_refresh&includeInternal=true", nil)
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
	for _, task := range result.Items {
		if task.Type == "package_refresh" && (!task.AllowRunNow || !task.AllowRetry) {
			t.Fatalf("expected registered capabilities in task response: %#v", task)
		}
		if task.Type == "sample_task" && (task.AllowRunNow || task.AllowRetry) {
			t.Fatalf("unexpected capabilities for unsupported test definition: %#v", task)
		}
	}
}
