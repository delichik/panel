package backups

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/platform/config"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestRestoreMaintenanceEncryptedPendingRequiresPasswordAndKeepsWrongPasswordError(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	manifest := writeEncryptedPendingRestore(t, cfg.DataRoot, "correct-password")

	app, err := NewRestoreApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initial := app.currentStatus()
	if initial.Mode != ModeRestoreRunning || initial.Phase != PhasePassword {
		t.Fatalf("expected encrypted pending restore to wait for password, got %+v", initial)
	}
	if initial.Manifest == nil || initial.Manifest.CreatedAt.IsZero() || !initial.Manifest.Encrypted {
		t.Fatalf("expected safe encrypted manifest in restore status, got %+v", initial.Manifest)
	}
	if !initial.Manifest.CreatedAt.Equal(manifest.CreatedAt) {
		t.Fatalf("manifest createdAt = %s, want %s", initial.Manifest.CreatedAt, manifest.CreatedAt)
	}
	token := loginMaintenance(t, app.Handler(), "admin", "password")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/password", bytes.NewBufferString(`{"password":"wrong-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected password submission to be accepted for retryable restore, got %d", rec.Code)
	}

	status := waitForStatus(t, app.currentStatus, func(status Status) bool {
		return status.Phase == PhasePassword && status.Error != ""
	})
	if status.Error != "Backup password is invalid" {
		t.Fatalf("expected wrong password error to be retained, got %+v", status)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(cfg.DataRoot), "pending.json")); err != nil {
		t.Fatalf("pending restore marker should remain after wrong password: %v", err)
	}
}

func TestRestoreClearPendingDeletesMarkerAndRequestsNormalRestart(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writePendingRestoreMarker(t, cfg.DataRoot, Manifest{FormatVersion: 1, CreatedAt: time.Now().UTC()})
	restarter := &fakeRestarter{supported: true}
	app := &RestoreApp{
		cfg:       cfg,
		mux:       http.NewServeMux(),
		restarter: restarter,
		status: Status{
			Mode:             ModeRestoreRunning,
			Phase:            PhasePassword,
			RestartSupported: true,
		},
		auth: testMaintenanceAuth(t, maintenanceAuthRestore),
	}
	app.routes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/clear-pending", nil)
	req.Header.Set("Authorization", "Bearer "+testMaintenanceToken(app.auth))
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected clear-pending success, got %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(cfg.DataRoot), "pending.json")); !os.IsNotExist(err) {
		t.Fatalf("expected pending restore marker to be removed, got %v", err)
	}
	status := decodeStatusEnvelope(t, rec.Body.Bytes())
	if status.Phase != PhaseCompleted {
		t.Fatalf("expected clear-pending to complete maintenance lifecycle, got %+v", status)
	}
	if restarter.calls != 1 || len(restarter.modes) != 1 || restarter.modes[0] != MaintenanceModeNormal {
		t.Fatalf("expected normal restart request after clearing pending restore, calls=%d modes=%v", restarter.calls, restarter.modes)
	}
}

func TestRestoreClearPendingFallsBackWhenRestartUnsupported(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writePendingRestoreMarker(t, cfg.DataRoot, Manifest{FormatVersion: 1, CreatedAt: time.Now().UTC()})
	restarter := &fakeRestarter{supported: false}
	app := &RestoreApp{
		cfg:       cfg,
		mux:       http.NewServeMux(),
		restarter: restarter,
		status: Status{
			Mode:             ModeRestoreRunning,
			Phase:            PhasePassword,
			RestartSupported: false,
		},
		auth: testMaintenanceAuth(t, maintenanceAuthRestore),
	}
	app.routes()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/clear-pending", nil)
	req.Header.Set("Authorization", "Bearer "+testMaintenanceToken(app.auth))
	app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected clear-pending success without restart support, got %d", rec.Code)
	}
	if restarter.calls != 0 {
		t.Fatalf("unsupported restart must use maintenance shutdown fallback, got restart calls=%d", restarter.calls)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(cfg.DataRoot), "pending.json")); !os.IsNotExist(err) {
		t.Fatalf("expected pending restore marker to be removed before fallback shutdown, got %v", err)
	}
	status := decodeStatusEnvelope(t, rec.Body.Bytes())
	if status.Phase != PhaseCompleted || status.RestartSupported {
		t.Fatalf("expected completed status without restart support, got %+v", status)
	}
}

func TestExportMaintenanceRequiresPendingMarkerAndUsesPasswordGate(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeMaintenanceAdmin(t, cfg.AppDatabase)

	missingPendingApp, err := NewExportApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if status := missingPendingApp.currentStatus(); status.Phase != PhaseFailed || status.Error == "" {
		t.Fatalf("expected export maintenance without pending marker to fail safely, got %+v", status)
	}

	if err := writePendingExport(cfg.DataRoot, pendingExport{
		ExportID:  "export-123",
		CreatedAt: time.Now().UTC(),
		Encrypt:   true,
	}); err != nil {
		t.Fatal(err)
	}
	app, err := NewExportApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := app.currentStatus()
	if status.Mode != ModeBackupExporting || status.Phase != PhasePassword || status.ExportID != "export-123" {
		t.Fatalf("expected encrypted pending export to wait for password, got %+v", status)
	}
}

func TestExportStartAndPasswordConflictOutsideExpectedPhase(t *testing.T) {
	passwordApp := &ExportApp{status: Status{Mode: ModeBackupExporting, Phase: PhasePassword, ExportID: "encrypted-export"}}
	startRec := httptest.NewRecorder()
	passwordApp.startAPI(startRec, httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/start", nil))
	if startRec.Code != http.StatusConflict {
		t.Fatalf("expected start to conflict while password is required, got %d", startRec.Code)
	}
	if status := decodeStatusEnvelope(t, startRec.Body.Bytes()); status.Phase != PhasePassword {
		t.Fatalf("expected conflict response to keep password-required state, got %+v", status)
	}

	readyApp := &ExportApp{status: Status{Mode: ModeBackupExporting, Phase: PhaseReady, ExportID: "plain-export"}}
	passwordRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/password", bytes.NewBufferString(`{"password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	readyApp.passwordAPI(passwordRec, req)
	if passwordRec.Code != http.StatusConflict {
		t.Fatalf("expected password submission to conflict for non-encrypted ready export, got %d", passwordRec.Code)
	}
	if status := decodeStatusEnvelope(t, passwordRec.Body.Bytes()); status.Phase != PhaseReady {
		t.Fatalf("expected conflict response to keep ready state, got %+v", status)
	}
}

func TestExportDownloadOnlyAllowedForCompletedMatchingExport(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	exportID := "export-complete"
	backupDir := filepath.Join(cfg.DataRoot, "tmp", "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, exportID+".panel-backup"), []byte("backup bytes"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		status     Status
		requestID  string
		wantStatus int
	}{
		{
			name: "completed matching export",
			status: Status{
				Mode:              ModeBackupExporting,
				Phase:             PhaseCompleted,
				ExportID:          exportID,
				DownloadAvailable: true,
			},
			requestID:  exportID,
			wantStatus: http.StatusOK,
		},
		{
			name: "not completed",
			status: Status{
				Mode:              ModeBackupExporting,
				Phase:             PhaseArchiving,
				ExportID:          exportID,
				DownloadAvailable: true,
			},
			requestID:  exportID,
			wantStatus: http.StatusNotFound,
		},
		{
			name: "wrong export id",
			status: Status{
				Mode:              ModeBackupExporting,
				Phase:             PhaseCompleted,
				ExportID:          exportID,
				DownloadAvailable: true,
			},
			requestID:  "other-export",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "completed but unavailable",
			status: Status{
				Mode:              ModeBackupExporting,
				Phase:             PhaseCompleted,
				ExportID:          exportID,
				DownloadAvailable: false,
			},
			requestID:  exportID,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &ExportApp{cfg: cfg, status: tc.status}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/export/"+tc.requestID+"/download", nil)
			req.SetPathValue("id", tc.requestID)
			rec := httptest.NewRecorder()

			app.downloadAPI(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("download status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusOK && rec.Body.String() != "backup bytes" {
				t.Fatalf("unexpected download body %q", rec.Body.String())
			}
		})
	}
}

func maintenanceTestConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return config.Config{
		AdminUsername:     "admin",
		AdminPasswordHash: string(hash),
		DataRoot:          filepath.Join(root, "data"),
		AppDatabase:       filepath.Join(root, "db", "app.db"),
		LogDatabase:       filepath.Join(root, "db", "log.db"),
		MetricsDatabase:   filepath.Join(root, "db", "metrics.db"),
	}
}

func testMaintenanceAuth(t *testing.T, authContext maintenanceAuthContext) *maintenanceAuth {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	auth := newMaintenanceAuthWithCredential(authContext, maintenanceCredential{Username: "admin", PasswordHash: string(hash)})
	testMaintenanceToken(auth)
	return auth
}

func testMaintenanceToken(auth *maintenanceAuth) string {
	const tokenBody = "test-maintenance-token"
	token := maintenanceTokenPrefix(auth.context) + tokenBody
	auth.mu.Lock()
	auth.sessions[token] = maintenanceSession{username: auth.username, expiresAt: auth.now().Add(time.Hour)}
	auth.mu.Unlock()
	return token
}

func loginMaintenance(t *testing.T, handler http.Handler, username, password string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("maintenance login status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Token == "" {
		t.Fatal("maintenance login returned empty token")
	}
	return envelope.Data.Token
}

func writeEncryptedPendingRestore(t *testing.T, dataRoot, password string) Manifest {
	t.Helper()
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(sourceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "config.json"), []byte(`{"safe":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	plain, manifest, err := buildArchive(ArchiveConfig{
		DataRoot:     sourceRoot,
		PanelVersion: "test",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encryptBytes(plain, password)
	if err != nil {
		t.Fatal(err)
	}
	dir := pendingDir(dataRoot)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	const archiveName = "restore.panel-backup"
	if err := os.WriteFile(filepath.Join(dir, archiveName), raw, 0600); err != nil {
		t.Fatal(err)
	}
	writePendingRestoreMarker(t, dataRoot, manifest)
	return manifest
}

func writePendingRestoreMarker(t *testing.T, dataRoot string, manifest Manifest) {
	t.Helper()
	dir := pendingDir(dataRoot)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(pendingRestore{
		ArchiveFilename: "restore.panel-backup",
		CreatedAt:       time.Now().UTC(),
		Manifest:        manifest,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pending.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeMaintenanceAdmin(t *testing.T, appDatabase string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(appDatabase), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", sqliteFileDSN(appDatabase))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
		CREATE TABLE auth_accounts (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL
		)
	`); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO auth_accounts (id, username, password_hash)
		VALUES ('admin', 'admin', ?)
	`, string(hash)); err != nil {
		t.Fatal(err)
	}
}

func waitForStatus(t *testing.T, current func() Status, done func(Status) bool) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var status Status
	for time.Now().Before(deadline) {
		status = current()
		if done(status) {
			return status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status, last status=%+v", status)
	return Status{}
}

func decodeStatusEnvelope(t *testing.T, raw []byte) Status {
	t.Helper()
	var envelope struct {
		Data  Status          `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode status envelope: %v; body=%s", err, string(raw))
	}
	return envelope.Data
}
