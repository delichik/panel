package applications

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
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

func TestHandlerApplicationFiles(t *testing.T) {
	fake := &fakeApplicationService{files: []ApplicationFile{{ID: "file-1", ApplicationID: "app-1", Path: "config/app.conf", Kind: "template"}}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/files", nil)
	handler.ListFiles(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/applications/app-1/files", bytes.NewBufferString(`{"path":"config/app.conf","kind":"template","contentBase64":"aGVsbG8="}`))
	handler.SaveFile(rec, req)
	if rec.Code != http.StatusOK || fake.fileInput.Path != "config/app.conf" || fake.fileInput.Kind != "template" {
		t.Fatalf("save file status=%d input=%#v body=%s", rec.Code, fake.fileInput, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/applications/app-1/files/file-1", nil)
	handler.DeleteFile(rec, req)
	if rec.Code != http.StatusNoContent || fake.deletedFileID != "file-1" {
		t.Fatalf("delete file status=%d id=%q body=%s", rec.Code, fake.deletedFileID, rec.Body.String())
	}
}

func TestHandlerPackageApplication(t *testing.T) {
	fake := &fakeApplicationService{pkg: PackageResult{Filename: "web-package.zip", Content: []byte("zip")}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/package", nil)
	handler.Package(rec, req)

	if rec.Code != http.StatusOK || fake.packagedID != "app-1" || rec.Body.String() != "zip" {
		t.Fatalf("package status=%d id=%q body=%q", rec.Code, fake.packagedID, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content type = %q", got)
	}
}

func TestHandlerPersistentData(t *testing.T) {
	fake := &fakeApplicationService{persistentData: PackageResult{Filename: "web-persistent.zip", Content: []byte("data")}}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/persistent-data", nil)
	handler.PersistentData(rec, req)

	if rec.Code != http.StatusOK || fake.persistentDataID != "app-1" || rec.Body.String() != "data" {
		t.Fatalf("persistent data status=%d id=%q body=%q", rec.Code, fake.persistentDataID, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content type = %q", got)
	}
}

func TestHandlerRestorePersistentData(t *testing.T) {
	fake := &fakeApplicationService{op: OperationResult{TaskID: "task-1", Application: Application{ID: "app-1"}}}
	handler := NewHandler(fake)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "data.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("zip"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/app-1/persistent-data", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	handler.RestorePersistentData(rec, req)

	if rec.Code != http.StatusOK || fake.restoredPersistentID != "app-1" || string(fake.restoredPersistentContent) != "zip" {
		t.Fatalf("restore status=%d id=%q content=%q body=%s", rec.Code, fake.restoredPersistentID, fake.restoredPersistentContent, rec.Body.String())
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
		runtime: ApplicationRuntime{ApplicationID: "app-1", RuntimeID: "panel-web", Status: "running", ObservedAt: time.Now().UTC()},
		logs:    LogResult{InstanceID: "inst-1", ContainerName: "web", Type: "combined", Logs: "hello"},
	}
	handler := NewHandler(fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/runtime", nil)
	handler.Runtime(rec, req)
	if rec.Code != http.StatusOK || fake.runtimeID != "app-1" {
		t.Fatalf("runtime status=%d id=%q body=%s", rec.Code, fake.runtimeID, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/applications/app-1/logs?instanceId=inst-1&containerName=web&tail=20", nil)
	handler.Logs(rec, req)
	if rec.Code != http.StatusOK || fake.logID != "app-1" || fake.logInput.InstanceID != "inst-1" || fake.logInput.ContainerName != "web" || fake.logInput.Tail != 20 {
		t.Fatalf("logs status=%d id=%q input=%#v body=%s", rec.Code, fake.logID, fake.logInput, rec.Body.String())
	}
}

type fakeApplicationService struct {
	apps                      []Application
	app                       Application
	files                     []ApplicationFile
	saved                     SaveInput
	fileInput                 FileSaveInput
	op                        OperationResult
	runtime                   ApplicationRuntime
	logs                      LogResult
	deployedID                string
	stoppedID                 string
	stopPurge                 bool
	runtimeID                 string
	logID                     string
	logInput                  LogInput
	deletedFileID             string
	pkg                       PackageResult
	packagedID                string
	persistentData            PackageResult
	persistentDataID          string
	restoredPersistentID      string
	restoredPersistentContent []byte
	session                   SaveSessionResult
	sessionID                 string
	checkedID                 string
	updatedImageID            string
}

func (f *fakeApplicationService) List(ctx context.Context) ([]Application, error) {
	return f.apps, nil
}

func (f *fakeApplicationService) TemplateCatalog(context.Context) (TemplateCatalog, error) {
	return TemplateCatalog{}, nil
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

func (f *fakeApplicationService) ListFiles(ctx context.Context, id string) ([]ApplicationFile, error) {
	return f.files, nil
}

func (f *fakeApplicationService) SaveFile(ctx context.Context, id string, in FileSaveInput) (ApplicationFile, error) {
	f.fileInput = in
	if len(f.files) > 0 {
		return f.files[0], nil
	}
	return ApplicationFile{ID: "file-1", ApplicationID: id, Path: in.Path, Kind: in.Kind}, nil
}

func (f *fakeApplicationService) DeleteFile(ctx context.Context, id, fileID string) error {
	f.deletedFileID = fileID
	return nil
}

func (f *fakeApplicationService) BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error) {
	f.saved = in.Save
	if f.session.ID == "" {
		f.session.ID = "asave_1"
	}
	return f.session, nil
}

func (f *fakeApplicationService) UploadSaveSessionFile(ctx context.Context, sessionID string, in FileSaveInput) (ApplicationFile, error) {
	f.sessionID = sessionID
	f.fileInput = in
	return ApplicationFile{ID: "file-1", Path: in.Path, Kind: in.Kind}, nil
}

func (f *fakeApplicationService) DeleteSaveSessionFile(ctx context.Context, sessionID string, in FileDeleteInput) error {
	f.sessionID = sessionID
	f.fileInput.Path = in.Path
	return nil
}

func (f *fakeApplicationService) CommitSaveSession(ctx context.Context, sessionID string) (Application, error) {
	f.sessionID = sessionID
	return f.app, nil
}

func (f *fakeApplicationService) Package(ctx context.Context, id string) (PackageResult, error) {
	f.packagedID = id
	return f.pkg, nil
}

func (f *fakeApplicationService) PersistentData(ctx context.Context, id string) (PackageResult, error) {
	f.persistentDataID = id
	return f.persistentData, nil
}

func (f *fakeApplicationService) RestorePersistentData(ctx context.Context, id string, content []byte) (OperationResult, error) {
	f.restoredPersistentID = id
	f.restoredPersistentContent = content
	return f.op, nil
}

func (f *fakeApplicationService) Validate(ctx context.Context, id string) (ValidationResult, error) {
	return ValidationResult{Valid: true}, nil
}

func (f *fakeApplicationService) Plan(ctx context.Context, id string) (PlanResult, error) {
	return PlanResult{}, nil
}

func (f *fakeApplicationService) CheckImageUpdate(ctx context.Context, id string) (Application, error) {
	f.checkedID = id
	return f.app, nil
}

func (f *fakeApplicationService) UpdateImage(ctx context.Context, id string) (OperationResult, error) {
	f.updatedImageID = id
	return f.op, nil
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
