package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRestoreMaintenanceAPIsRequireRestoreAuthentication(t *testing.T) {
	auth := testMaintenanceAuth(t, maintenanceAuthRestore)
	app := &RestoreApp{
		cfg:        maintenanceTestConfig(t),
		mux:        http.NewServeMux(),
		auth:       auth,
		operations: make(map[string]maintenanceOperation),
		status: Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 1, Mode: ModeRestoreRunning,
			Phase: PhasePassword},
	}
	app.routes()

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/restore/status", ""},
		{http.MethodPost, "/api/v1/restore/password", `{"password":"secret"}`},
		{http.MethodPost, "/api/v1/restore/retry", ""},
		{http.MethodPost, "/api/v1/restore/clear-pending", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without auth status = %d", tc.method, tc.path, rec.Code)
		}
	}

	exportAuth := testMaintenanceAuth(t, maintenanceAuthExport)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/restore/status", nil)
	req.Header.Set("Authorization", "Bearer "+testMaintenanceToken(exportAuth))
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("export token used for restore status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/restore/status", nil)
	req.Header.Set("Authorization", "Bearer normal-runtime-token")
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("normal token used for restore status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/restore/status", nil)
	req.Header.Set("Authorization", "Bearer "+testMaintenanceToken(auth))
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore token status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestMaintenanceAuthenticationExpiresAndLogoutRevokes(t *testing.T) {
	auth := testMaintenanceAuth(t, maintenanceAuthExport)
	now := time.Now().UTC()
	auth.now = func() time.Time { return now }
	token := maintenanceTokenPrefix(maintenanceAuthExport) + "expiring"
	auth.sessions[token] = maintenanceSession{username: "admin", expiresAt: now.Add(time.Minute)}

	protected := auth.require(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid token status = %d", rec.Code)
	}

	now = now.Add(time.Minute)
	rec = httptest.NewRecorder()
	protected(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d", rec.Code)
	}

	token = testMaintenanceToken(auth)
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.Header.Set("Authorization", "Bearer "+token)
	auth.logoutAPI(httptest.NewRecorder(), logout)
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	protected(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("logged out token status = %d", rec.Code)
	}
}

func TestExportStartIsAtomicAndIdempotent(t *testing.T) {
	var runs atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	app := &ExportApp{
		status:     Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 7, Mode: ModeBackupExporting, Phase: PhaseReady},
		operations: make(map[string]maintenanceOperation),
		runFn: func(context.Context, string) {
			runs.Add(1)
			started <- struct{}{}
			<-release
		},
	}

	const count = 16
	var wg sync.WaitGroup
	statuses := make(chan int, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/start", bytes.NewBufferString(`{"expectedRevision":7,"clientOperationId":"same-operation"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.startAPI(rec, req)
			statuses <- rec.Code
		}()
	}
	wg.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("export runner did not start")
	}
	close(release)
	close(statuses)
	for status := range statuses {
		if status != http.StatusAccepted {
			t.Fatalf("idempotent concurrent start status = %d", status)
		}
	}
	if runs.Load() != 1 {
		t.Fatalf("export runner count = %d, want 1", runs.Load())
	}
	status := app.currentStatus()
	if status.Revision != 8 || status.Phase != PhaseCheckpointing || status.Capabilities.CanStart {
		t.Fatalf("unexpected accepted status: %+v", status)
	}
}

func TestExportStartRejectsStaleRevisionAndCompetingOperation(t *testing.T) {
	var runs atomic.Int32
	app := &ExportApp{
		status:     Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 4, Mode: ModeBackupExporting, Phase: PhaseReady},
		operations: make(map[string]maintenanceOperation),
		runFn:      func(context.Context, string) { runs.Add(1) },
	}
	stale := httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/start", bytes.NewBufferString(`{"expectedRevision":3,"clientOperationId":"stale"}`))
	stale.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.startAPI(rec, stale)
	if rec.Code != http.StatusConflict || runs.Load() != 0 {
		t.Fatalf("stale command status=%d runs=%d", rec.Code, runs.Load())
	}

	first := httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/start", bytes.NewBufferString(`{"expectedRevision":4,"clientOperationId":"first"}`))
	first.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	app.startAPI(rec, first)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first command status = %d", rec.Code)
	}
	competing := httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/start", bytes.NewBufferString(`{"expectedRevision":4,"clientOperationId":"second"}`))
	competing.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	app.startAPI(rec, competing)
	if rec.Code != http.StatusConflict {
		t.Fatalf("competing command status = %d", rec.Code)
	}
}

func TestRestorePasswordIsAtomicAndIllegalPhaseConflicts(t *testing.T) {
	var runs atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	app := &RestoreApp{
		status:     Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 2, Mode: ModeRestoreRunning, Phase: PhasePassword},
		operations: make(map[string]maintenanceOperation),
		applyFn: func(context.Context, string) {
			runs.Add(1)
			started <- struct{}{}
			<-release
		},
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/password", bytes.NewBufferString(`{"password":"secret","expectedRevision":2,"clientOperationId":"password-op"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.passwordAPI(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Errorf("password command status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("restore runner did not start")
	}
	close(release)
	if runs.Load() != 1 {
		t.Fatalf("restore runner count = %d, want 1", runs.Load())
	}

	illegal := httptest.NewRequest(http.MethodPost, "/api/v1/restore/password", bytes.NewBufferString(`{"password":"secret","clientOperationId":"other"}`))
	illegal.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.passwordAPI(rec, illegal)
	if rec.Code != http.StatusConflict {
		t.Fatalf("illegal phase password status = %d", rec.Code)
	}
}

func TestRestoreClearPendingIdempotencyDoesNotRepeatLifecycleAction(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writePendingRestoreMarker(t, cfg.DataRoot, Manifest{FormatVersion: 1, CreatedAt: time.Now().UTC(), Encrypted: true})
	restarter := &fakeRestarter{supported: true}
	app := &RestoreApp{
		cfg:        cfg,
		restarter:  restarter,
		operations: make(map[string]maintenanceOperation),
		status: Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 5, Mode: ModeRestoreRunning,
			Phase: PhasePassword, RestartSupported: true},
	}
	command := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/clear-pending", bytes.NewBufferString(`{"expectedRevision":5}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "clear-once")
		rec := httptest.NewRecorder()
		app.clearPendingAPI(rec, req)
		return rec
	}
	first := command()
	second := command()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("clear statuses = %d, %d", first.Code, second.Code)
	}
	if restarter.calls != 1 {
		t.Fatalf("clear-pending lifecycle action count = %d, want 1", restarter.calls)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(cfg.DataRoot), "pending.json")); !os.IsNotExist(err) {
		t.Fatalf("pending marker remains after clear: %v", err)
	}
}

func TestRestoreRetryUsesPasswordGateForEncryptedArchive(t *testing.T) {
	var runs atomic.Int32
	app := &RestoreApp{
		operations: make(map[string]maintenanceOperation),
		status: Status{SchemaVersion: MaintenanceStatusSchemaVersion, Revision: 3, Mode: ModeRestoreRunning,
			Phase: PhaseFailed, Manifest: &Manifest{Encrypted: true},
			Error: "Unable to apply restored data", ErrorDetail: &MaintenanceError{Code: "restore_apply_failed", Message: "Unable to apply restored data", Retryable: true}},
		applyFn: func(context.Context, string) { runs.Add(1) },
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/retry", bytes.NewBufferString(`{"expectedRevision":3,"clientOperationId":"retry-encrypted"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.retryAPI(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("encrypted retry status = %d", rec.Code)
	}
	status := app.currentStatus()
	if status.Phase != PhasePassword || !status.Capabilities.CanSubmitPassword || runs.Load() != 0 {
		t.Fatalf("encrypted retry should return to password gate without running: %+v runs=%d", status, runs.Load())
	}
}

func TestRestorePendingCredentialWorksWithoutAppDatabase(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	manifest := writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	marker, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	marker.Manifest = manifest
	marker.MaintenanceAuth = &maintenanceCredential{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir(cfg.DataRoot), "pending.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.AppDatabase = filepath.Join(t.TempDir(), "missing", "app.db")
	cfg.AdminUsername = ""
	cfg.AdminPasswordHash = ""

	app, err := NewRestoreApp(cfg)
	if err != nil {
		t.Fatalf("restore app should use pending credential without app.db: %v", err)
	}
	token := loginMaintenance(t, app.Handler(), "admin", "password")
	if token == "" || token[:3] != maintenanceTokenPrefix(maintenanceAuthRestore) {
		t.Fatalf("unexpected restore token %q", token)
	}
}

func TestStatusContractIncludesVersionCapabilitiesAndStructuredError(t *testing.T) {
	status := prepareStatus(Status{
		Mode:        ModeRestoreRunning,
		Phase:       PhaseFailed,
		Revision:    9,
		Error:       "Unable to apply restored data",
		ErrorDetail: &MaintenanceError{Code: "restore_apply_failed", Message: "Unable to apply restored data", Retryable: true},
	})
	if status.SchemaVersion != MaintenanceStatusSchemaVersion || status.Revision != 9 {
		t.Fatalf("status version contract missing: %+v", status)
	}
	if !status.Retryable || !status.Capabilities.CanRetry || !status.Capabilities.CanClearPending {
		t.Fatalf("status capabilities incorrect: %+v", status)
	}
	if status.Error == "" || status.ErrorDetail == nil || status.ErrorDetail.Code == "" {
		t.Fatalf("legacy and structured errors must coexist: %+v", status)
	}
}
