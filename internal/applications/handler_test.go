package applications

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"panel/internal/httpx"
)

func TestHandlerListApplications(t *testing.T) {
	fake := &fakeApplicationService{apps: []Application{{ID: "app-1", Name: "web"}}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	handler.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env httpx.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env.Data)
	var apps []Application
	if err := json.Unmarshal(raw, &apps); err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].ID != "app-1" {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestHandlerCreateApplication(t *testing.T) {
	fake := &fakeApplicationService{app: Application{ID: "app-1", Name: "web"}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications", bytes.NewBufferString(`{"name":"web","enabled":true,"specYaml":"name: web\nimage: nginx\n","variables":{"A":"1"}}`))
	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !fake.saved.Enabled || fake.saved.Name != "web" || fake.saved.Variables["A"] != "1" {
		t.Fatalf("saved = %#v", fake.saved)
	}
}

func TestHandlerDeployAndStopApplication(t *testing.T) {
	fake := &fakeApplicationService{op: OperationResult{TaskID: "task-1", EvalID: "eval-1", Application: Application{ID: "app-1"}}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/app-1/deploy", nil)
	handler.Deploy(rec, req)
	if rec.Code != http.StatusOK || fake.deployedID != "app-1" {
		t.Fatalf("deploy status=%d id=%q body=%s", rec.Code, fake.deployedID, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/applications/app-1/stop", nil)
	handler.Stop(rec, req)
	if rec.Code != http.StatusOK || fake.stoppedID != "app-1" || fake.stopPurge {
		t.Fatalf("stop status=%d id=%q purge=%v body=%s", rec.Code, fake.stoppedID, fake.stopPurge, rec.Body.String())
	}
}

func TestHandlerRuntimeAndLogs(t *testing.T) {
	fake := &fakeApplicationService{
		runtime: ApplicationRuntime{ApplicationID: "app-1", JobID: "panel-web", JobStatus: "running", ObservedAt: time.Now().UTC()},
		logs:    LogResult{AllocID: "alloc-1", Task: "web", Type: "stdout", Logs: "hello"},
	}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/runtime", nil)
	handler.Runtime(rec, req)
	if rec.Code != http.StatusOK || fake.runtimeID != "app-1" {
		t.Fatalf("runtime status=%d id=%q body=%s", rec.Code, fake.runtimeID, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/logs?allocId=alloc-1&task=web&tail=20", nil)
	handler.Logs(rec, req)
	if rec.Code != http.StatusOK || fake.logID != "app-1" || fake.logInput.AllocID != "alloc-1" || fake.logInput.Tail != 20 {
		t.Fatalf("logs status=%d id=%q input=%#v body=%s", rec.Code, fake.logID, fake.logInput, rec.Body.String())
	}
}

type fakeApplicationService struct {
	apps       []Application
	app        Application
	saved      SaveInput
	op         OperationResult
	runtime    ApplicationRuntime
	logs       LogResult
	deployedID string
	stoppedID  string
	stopPurge  bool
	runtimeID  string
	logID      string
	logInput   LogInput
}

func (f *fakeApplicationService) List(ctx context.Context) ([]Application, error) {
	return f.apps, nil
}

func (f *fakeApplicationService) Get(ctx context.Context, id string) (Application, error) {
	return f.app, nil
}

func (f *fakeApplicationService) Create(ctx context.Context, in SaveInput) (Application, error) {
	f.saved = in
	return f.app, nil
}

func (f *fakeApplicationService) Update(ctx context.Context, id string, in SaveInput) (Application, error) {
	f.saved = in
	return f.app, nil
}

func (f *fakeApplicationService) Delete(ctx context.Context, id string) error {
	return nil
}

func (f *fakeApplicationService) Validate(ctx context.Context, id string) (ValidationResult, error) {
	return ValidationResult{Valid: true}, nil
}

func (f *fakeApplicationService) Plan(ctx context.Context, id string) (PlanResult, error) {
	return PlanResult{}, nil
}

func (f *fakeApplicationService) Deploy(ctx context.Context, id string) (OperationResult, error) {
	f.deployedID = id
	return f.op, nil
}

func (f *fakeApplicationService) Stop(ctx context.Context, id string, purge bool) (OperationResult, error) {
	f.stoppedID = id
	f.stopPurge = purge
	return f.op, nil
}

func (f *fakeApplicationService) Restart(ctx context.Context, id string) (OperationResult, error) {
	return f.op, nil
}

func (f *fakeApplicationService) Runtime(ctx context.Context, id string) (ApplicationRuntime, error) {
	f.runtimeID = id
	return f.runtime, nil
}

func (f *fakeApplicationService) Logs(ctx context.Context, id string, in LogInput) (LogResult, error) {
	f.logID = id
	f.logInput = in
	return f.logs, nil
}
