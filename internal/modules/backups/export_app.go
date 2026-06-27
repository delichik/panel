package backups

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/buildinfo"
	"panel/internal/platform/config"
	httpx "panel/internal/platform/http"

	_ "modernc.org/sqlite"
)

type ExportApp struct {
	cfg    config.Config
	mux    *http.ServeMux
	mu     sync.RWMutex
	status Status
}

func PendingExportExists(dataRoot string) bool {
	_, err := os.Stat(filepath.Join(exportPendingDir(dataRoot), "pending.json"))
	return err == nil
}

func NewExportApp(cfg config.Config) (*ExportApp, error) {
	app := &ExportApp{
		cfg: cfg,
		mux: http.NewServeMux(),
		status: Status{
			Mode:             ModeBackupExporting,
			Phase:            PhaseFreezing,
			Progress:         5,
			StartedAt:        time.Now().UTC(),
			RestartSupported: false,
		},
	}
	app.routes()
	marker, err := readPendingExport(cfg.DataRoot)
	if err != nil {
		app.fail("Unable to read pending backup export marker")
		return app, nil
	}
	app.setExportID(marker.ExportID)
	if marker.Encrypt {
		app.set(PhasePassword, 10, "")
	} else {
		go app.run(context.Background(), "")
	}
	return app, nil
}

func (a *ExportApp) Handler() http.Handler { return a.mux }
func (a *ExportApp) Close() error          { return nil }

func (a *ExportApp) routes() {
	a.mux.HandleFunc("GET /api/v1/backups/export/current", a.statusAPI)
	a.mux.HandleFunc("POST /api/v1/backups/export/password", a.passwordAPI)
	a.mux.HandleFunc("GET /api/v1/backups/export/{id}/download", a.downloadAPI)
	a.mux.HandleFunc("POST /api/v1/backups/export/exit", a.exitAPI)
	a.mux.HandleFunc("/", a.static)
}

func (a *ExportApp) passwordAPI(w http.ResponseWriter, r *http.Request) {
	var req RestorePasswordRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	go a.run(context.Background(), req.Password)
	httpx.JSON(w, http.StatusAccepted, a.currentStatus())
}

func (a *ExportApp) statusAPI(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, a.currentStatus())
}

func (a *ExportApp) downloadAPI(w http.ResponseWriter, r *http.Request) {
	status := a.currentStatus()
	if status.Phase != PhaseCompleted || status.ExportID != r.PathValue("id") || !status.DownloadAvailable {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.cfg.DataRoot, "tmp", "backups", status.ExportID+".panel-backup")
	w.Header().Set("Content-Disposition", `attachment; filename="panel-`+filepath.Base(path)+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

func (a *ExportApp) exitAPI(w http.ResponseWriter, r *http.Request) {
	status := a.currentStatus()
	if status.Phase != PhaseCompleted && status.Phase != PhaseFailed {
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	_ = os.RemoveAll(exportPendingDir(a.cfg.DataRoot))
	httpx.JSON(w, http.StatusOK, status)
}

func (a *ExportApp) run(ctx context.Context, password string) {
	marker, err := readPendingExport(a.cfg.DataRoot)
	if err != nil {
		a.fail("Unable to read pending backup export marker")
		return
	}
	a.setExportID(marker.ExportID)
	a.set(PhaseCheckpointing, 20, "")
	if err := checkpointSQLiteFiles(ctx, a.cfg.AppDatabase, a.cfg.TaskDatabase, a.cfg.MetricsDatabase); err != nil {
		a.fail("Unable to checkpoint databases")
		return
	}
	a.set(PhaseArchiving, 45, "")
	plain, manifest, err := buildArchive(ArchiveConfig{
		DataRoot:        a.cfg.DataRoot,
		AppDatabase:     a.cfg.AppDatabase,
		TaskDatabase:    a.cfg.TaskDatabase,
		MetricsDatabase: a.cfg.MetricsDatabase,
		PanelVersion:    buildinfo.Version,
	}, marker.Encrypt)
	if err != nil {
		a.fail("Unable to build backup archive")
		return
	}
	raw := plain
	if marker.Encrypt {
		a.set(PhaseEncrypting, 75, "")
		raw, err = encryptBytes(plain, password)
		if err != nil {
			a.fail("Unable to encrypt backup archive")
			return
		}
	}
	dir := filepath.Join(a.cfg.DataRoot, "tmp", "backups")
	if err := os.MkdirAll(dir, 0700); err != nil {
		a.fail("Unable to prepare backup storage")
		return
	}
	if err := os.WriteFile(filepath.Join(dir, marker.ExportID+".panel-backup"), raw, 0600); err != nil {
		a.fail("Unable to store backup archive")
		return
	}
	_ = os.RemoveAll(exportPendingDir(a.cfg.DataRoot))
	a.mu.Lock()
	a.status.Phase = PhaseCompleted
	a.status.Progress = 100
	a.status.FinishedAt = time.Now().UTC()
	a.status.DownloadAvailable = true
	a.status.Manifest = &manifest
	a.mu.Unlock()
}

func checkpointSQLiteFiles(ctx context.Context, paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			_ = db.Close()
			return err
		}
		if err := db.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (a *ExportApp) currentStatus() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *ExportApp) set(phase string, progress int, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.Phase = phase
	a.status.Progress = progress
	a.status.Error = message
	if phase == PhaseCompleted || phase == PhaseFailed {
		a.status.FinishedAt = time.Now().UTC()
	}
}

func (a *ExportApp) setExportID(exportID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.ExportID = exportID
}

func (a *ExportApp) fail(message string) {
	a.set(PhaseFailed, 100, message)
}

func (a *ExportApp) static(w http.ResponseWriter, r *http.Request) {
	dist := filepath.Join("web", "dist")
	index := filepath.Join(dist, "index.html")
	if _, err := os.Stat(index); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Panel backup export mode is running. Build the frontend into web/dist to serve the maintenance UI.\n"))
		return
	}
	rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
	if rel == "." || rel == "" {
		http.ServeFile(w, r, index)
		return
	}
	path := filepath.Join(dist, rel)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, index)
}
