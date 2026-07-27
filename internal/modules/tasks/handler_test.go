package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/platform/http"
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

type fakeDeploymentProvider struct{}

func (fakeDeploymentProvider) DecorateDeploymentTasks(ctx context.Context, items []Task) error {
	for idx := range items {
		items[idx].Deployment = &TaskDeploymentProjection{
			Operation: &TaskDeploymentOperationProjection{
				ID:              "life-op-1",
				ApplicationID:   "app-1",
				ApplicationName: "web",
				Type:            "deploy",
				Status:          "deploying",
				CreatedAt:       items[idx].CreatedAt,
				UpdatedAt:       items[idx].CreatedAt,
			},
			Target: &TaskDeploymentTargetProjection{
				ID:              "life-target-1",
				OperationID:     "life-op-1",
				ApplicationID:   "app-1",
				ApplicationName: "web",
				ServerID:        "srv-1",
				Status:          "failed",
				State:           "failed_retryable",
				CreatedAt:       items[idx].CreatedAt,
				UpdatedAt:       items[idx].CreatedAt,
			},
		}
	}
	return nil
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
	if runner.task.ID != task.ID {
		t.Fatalf("expected runner to receive task %s, got %#v", task.ID, runner.task)
	}
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

	rec := serveTaskRoute(handler, http.MethodPost, "/api/v1/tasks/"+task.ID+"/retry")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected queued retry to be rejected, got %d", rec.Code)
	}
}

func TestHandlerDecoratesDeploymentProjectionOnlyOnGet(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{Type: "application_target_apply", ConcurrencyPolicy: ConcurrencyParallelAllowed})
	task, err := svc.Create(context.Background(), CreateInput{Type: "application_target_apply", ServerID: "srv-1", ResourceType: "application", ResourceID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(svc)
	handler.SetDeploymentProjectionProvider(fakeDeploymentProvider{})

	getRec := serveTaskRoute(handler, http.MethodGet, "/api/v1/tasks/"+task.ID)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get to succeed, got %d", getRec.Code)
	}
	var getEnvelope httpx.Envelope
	if err := json.NewDecoder(getRec.Body).Decode(&getEnvelope); err != nil {
		t.Fatal(err)
	}
	rawGet, err := json.Marshal(getEnvelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var got Task
	if err := json.Unmarshal(rawGet, &got); err != nil {
		t.Fatal(err)
	}
	if got.Deployment == nil || got.Deployment.Target == nil || got.Deployment.Target.State != "failed_retryable" {
		t.Fatalf("expected deployment projection on get response, got %#v", got.Deployment)
	}

	listRec := serveTaskRoute(handler, http.MethodGet, "/api/v1/tasks?includeInternal=true")
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list to succeed, got %d", listRec.Code)
	}
	var listEnvelope httpx.Envelope
	if err := json.NewDecoder(listRec.Body).Decode(&listEnvelope); err != nil {
		t.Fatal(err)
	}
	rawList, err := json.Marshal(listEnvelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result ListResult
	if err := json.Unmarshal(rawList, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) == 0 || result.Items[0].Deployment != nil || result.Items[0].ParamsJSON != "" || result.Items[0].MetadataJSON != "" {
		t.Fatalf("expected lightweight list response, got %#v", result.Items)
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
