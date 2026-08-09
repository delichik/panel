package applications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

func TestHandlerListApplications(t *testing.T) {
	fake := &fakeApplicationService{apps: []Application{{ID: "app-1", Name: "web", SpecYAML: "name: web\n", DeploymentServers: []string{"srv-1"}, ReverseProxy: []ReverseProxyRule{{Domain: "example.test"}}, JobID: "panel-web", Namespace: "default"}}}
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
	var apps []ApplicationSummary
	if err := json.Unmarshal(raw, &apps); err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].ID != "app-1" || apps[0].JobID != "panel-web" {
		t.Fatalf("apps = %#v", apps)
	}
	if bytes.Contains(raw, []byte("specYaml")) || bytes.Contains(raw, []byte("deploymentServers")) || bytes.Contains(raw, []byte("reverseProxy")) {
		t.Fatalf("summary leaked detail fields: %s", raw)
	}
}

func serveTestRoute(handler *Handler, method, target string, body *bytes.Buffer) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	rec := httptest.NewRecorder()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body.Bytes())
	}
	req := httptest.NewRequest(method, target, reader)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerApplicationFiles(t *testing.T) {
	fake := &fakeApplicationService{files: []ApplicationFile{{ID: "file-1", ApplicationID: "app-1", Name: "config/app.conf", Kind: "template"}}}
	handler := NewHandler(fake)

	rec := serveTestRoute(handler, http.MethodGet, "/api/v1/applications/app-1/files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUploadsAndDownloadsEditSessionBinary(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	session, err := svc.BeginEditSession(context.Background(), applicationEditSessionOwner, BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	file, err := writer.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("binary-content"))
	_ = writer.WriteField("revision", "1")
	_ = writer.WriteField("clientOperationId", "upload-binary")
	_ = writer.WriteField("name", "logo")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	req := httptest.NewRequest(http.MethodPut, "/api/v1/application-edit-sessions/"+session.ID+"/uploads/logo", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Idempotency-Key", "upload-binary")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}

	download := httptest.NewRecorder()
	mux.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/api/v1/application-edit-sessions/"+session.ID+"/files/logo/content", nil))
	if download.Code != http.StatusOK || download.Body.String() != "binary-content" {
		t.Fatalf("download status=%d body=%q", download.Code, download.Body.String())
	}
	if download.Header().Get("Content-Disposition") == "" {
		t.Fatal("download filename header is missing")
	}
}

func TestApplicationDownloadDispositionSanitizesFilename(t *testing.T) {
	for _, name := range []string{"../bad\r\nX-Evil: yes\".txt", "报告.txt"} {
		header := applicationContentDisposition(name)
		mediaType, params, err := mime.ParseMediaType(header)
		if err != nil || mediaType != "attachment" {
			t.Fatalf("header %q: %v", header, err)
		}
		if strings.ContainsAny(params["filename"], "\r\n\x00/\\\"") {
			t.Fatalf("unsafe filename %q", params["filename"])
		}
	}
}

func TestReadApplicationUploadBoundary(t *testing.T) {
	content, err := readApplicationUpload(strings.NewReader("1234"), 4)
	if err != nil || string(content) != "1234" {
		t.Fatalf("boundary content=%q err=%v", content, err)
	}
	if _, err := readApplicationUpload(strings.NewReader("12345"), 4); !errors.Is(err, errApplicationUploadTooLarge) {
		t.Fatalf("over limit err=%v", err)
	}
}

func TestHandlerPersistentData(t *testing.T) {
	fake := &fakeApplicationService{persistentData: PackageResult{Filename: "web-persistent.zip", Content: []byte("data")}}
	handler := NewHandler(fake)

	rec := serveTestRoute(handler, http.MethodGet, "/api/v1/applications/app-1/persistent-data", nil)

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

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/app-1/persistent-data", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || fake.restoredPersistentID != "app-1" || string(fake.restoredPersistentContent) != "zip" {
		t.Fatalf("restore status=%d id=%q content=%q body=%s", rec.Code, fake.restoredPersistentID, fake.restoredPersistentContent, rec.Body.String())
	}
}

func TestHandlerDeployAndStopApplication(t *testing.T) {
	fake := &fakeApplicationService{op: OperationResult{TaskID: "task-1", EvalID: "eval-1", Application: Application{ID: "app-1"}}}
	handler := NewHandler(fake)

	rec := serveTestRoute(handler, http.MethodPost, "/api/v1/applications/app-1/deploy", nil)
	if rec.Code != http.StatusOK || fake.deployedID != "app-1" {
		t.Fatalf("deploy status=%d id=%q body=%s", rec.Code, fake.deployedID, rec.Body.String())
	}

	rec = serveTestRoute(handler, http.MethodPost, "/api/v1/applications/app-1/stop", nil)
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

	rec := serveTestRoute(handler, http.MethodGet, "/api/v1/applications/app-1/runtime", nil)
	if rec.Code != http.StatusOK || fake.runtimeID != "app-1" {
		t.Fatalf("runtime status=%d id=%q body=%s", rec.Code, fake.runtimeID, rec.Body.String())
	}

	rec = serveTestRoute(handler, http.MethodGet, "/api/v1/applications/app-1/logs?instanceId=inst-1&containerName=web&tail=20", nil)
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
	fileArchiveInput          FileArchiveInput
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
	migratedID                string
	migrateInput              MigrationInput
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
	records                   OperationRecordListResult
	recordDetail              OperationRecordDetail
}

func (f *fakeApplicationService) ListApplicationOperationRecords(_ context.Context, _ OperationRecordFilter) (OperationRecordListResult, error) {
	return f.records, nil
}

func (f *fakeApplicationService) GetApplicationOperationRecord(_ context.Context, _ string) (OperationRecordDetail, error) {
	return f.recordDetail, nil
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

func (f *fakeApplicationService) GetFile(ctx context.Context, id, fileID string) (ApplicationFile, error) {
	for _, file := range f.files {
		if file.ID == fileID {
			file.ContentBase64 = "aGVsbG8="
			return file, nil
		}
	}
	return ApplicationFile{}, panelerr.NotFound("application_file")
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

func (f *fakeApplicationService) UploadSaveSessionArchive(ctx context.Context, sessionID string, in FileArchiveInput) ([]ApplicationFile, error) {
	f.sessionID = sessionID
	f.fileArchiveInput = in
	return f.files, nil
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

func (f *fakeApplicationService) Migrate(ctx context.Context, id string, in MigrationInput) (OperationResult, error) {
	f.migratedID = id
	f.migrateInput = in
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
