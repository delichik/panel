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
