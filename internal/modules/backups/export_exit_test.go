package backups

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExportExitRefusesUntilArchiveDownloaded(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	backupDir := filepath.Join(cfg.DataRoot, "tmp", "backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	exportID := "export-exit"
	archivePath := filepath.Join(backupDir, exportID+".panel-backup")
	if err := os.WriteFile(archivePath, []byte("backup bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	restarter := &fakeRestarter{supported: true}
	app := &ExportApp{
		cfg:        cfg,
		restarter:  restarter,
		operations: make(map[string]maintenanceOperation),
		status: Status{
			Mode:              ModeBackupExporting,
			Phase:             PhaseCompleted,
			ExportID:          exportID,
			DownloadAvailable: true,
			RestartSupported:  true,
		},
	}

	// Exit without downloading must be refused and keep the archive.
	rec := httptest.NewRecorder()
	app.exitAPI(rec, httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/exit", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("exit without download: status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive must be kept when not downloaded: %v", err)
	}
	if restarter.calls != 0 {
		t.Fatalf("refused exit must not restart, calls=%d", restarter.calls)
	}

	// Download marks the archive as fetched.
	dlRec := httptest.NewRecorder()
	dlReq := httptest.NewRequest(http.MethodGet, "/api/v1/backups/export/"+exportID+"/download", nil)
	dlReq.SetPathValue("id", exportID)
	app.downloadAPI(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("download status = %d, want 200", dlRec.Code)
	}

	// Exit now succeeds and removes the archive.
	rec = httptest.NewRecorder()
	app.exitAPI(rec, httptest.NewRequest(http.MethodPost, "/api/v1/backups/export/exit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("exit after download: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive should be removed after downloaded exit, stat err=%v", err)
	}
	if restarter.calls != 1 {
		t.Fatalf("expected one restart after downloaded exit, calls=%d", restarter.calls)
	}
}
