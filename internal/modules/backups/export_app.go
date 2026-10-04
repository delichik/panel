package backups

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/buildinfo"
	"panel/internal/platform/config"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/logging"
	"panel/internal/platform/paneltls"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

type ExportApp struct {
	cfg        config.Config
	mux        *http.ServeMux
	listener   maintenanceListener
	mu         sync.RWMutex
	status     Status
	restarter  Restarter
	auth       *maintenanceAuth
	operations map[string]maintenanceOperation
	runFn      func(context.Context, string)
	// downloadedAt records the first successful download of the completed
	// export. It is intentionally not serialized into the status payload;
	// the server uses it to refuse exit until the archive has been fetched.
	downloadedAt time.Time
}

func PendingExportExists(dataRoot string) bool {
	_, err := os.Stat(filepath.Join(exportPendingDir(dataRoot), "pending.json"))
	return err == nil
}

func NewExportApp(cfg config.Config) (*ExportApp, error) {
	restarter := NewPanelInitRestarter(cfg.DataRoot)
	auth, err := newMaintenanceAuth(context.Background(), maintenanceAuthExport, cfg.AppDatabase, maintenanceCredential{
		Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash,
	})
	if err != nil {
		return nil, err
	}
	app := &ExportApp{
		cfg:        cfg,
		mux:        http.NewServeMux(),
		restarter:  restarter,
		auth:       auth,
		operations: make(map[string]maintenanceOperation),
		status: Status{
			SchemaVersion:    MaintenanceStatusSchemaVersion,
			Revision:         1,
			Mode:             ModeBackupExporting,
			Phase:            PhaseReady,
			Progress:         10,
			StartedAt:        time.Now().UTC(),
			RestartSupported: restarter.Supported(),
		},
	}
	app.routes()
	marker, err := readPendingExport(cfg.DataRoot)
	if err != nil {
		app.fail("backup_pending_unavailable", "Unable to read pending backup export marker", false)
		return app, nil
	}
	app.setExportID(marker.ExportID)
	if marker.Encrypt {
		app.set(PhasePassword, 10, "")
	}
	return app, nil
}

func (a *ExportApp) Handler() http.Handler { return a.mux }

func (a *ExportApp) ListenAndServeTLS(address string) error {
	return a.listener.listenAndServeTLS(address, a.Handler(), a.tlsConfig())
}

func (a *ExportApp) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		certificate, err := paneltls.FixedCertificate(a.cfg.DataRoot, "")
		return &certificate, err
	}}
}

func (a *ExportApp) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.listener.shutdown(ctx)
}

func (a *ExportApp) routes() {
	a.mux.HandleFunc("POST /api/v1/auth/login", a.auth.loginAPI)
	a.mux.HandleFunc("GET /api/v1/auth/session", a.auth.sessionAPI)
	a.mux.HandleFunc("POST /api/v1/auth/logout", a.auth.logoutAPI)
	a.mux.HandleFunc("GET /api/v1/backups/export/current", a.auth.require(a.statusAPI))
	a.mux.HandleFunc("POST /api/v1/backups/export/start", a.auth.require(a.startAPI))
	a.mux.HandleFunc("POST /api/v1/backups/export/password", a.auth.require(a.passwordAPI))
	a.mux.HandleFunc("GET /api/v1/backups/export/{id}/download", a.auth.require(a.downloadAPI))
	a.mux.HandleFunc("POST /api/v1/backups/export/exit", a.auth.require(a.exitAPI))
	a.mux.HandleFunc("/", a.static)
}

func (a *ExportApp) startAPI(w http.ResponseWriter, r *http.Request) {
	var req MaintenanceCommandRequest
	if !decodeOptionalCommand(w, r, &req) {
		return
	}
	operationID, ok := commandOperationID(w, r, req.ClientOperationID)
	if !ok {
		return
	}
	a.mu.Lock()
	if a.operations == nil {
		a.operations = make(map[string]maintenanceOperation)
	}
	if record, replay, mismatch := operationReplay(a.operations, operationID, "start"); replay {
		a.mu.Unlock()
		if mismatch {
			httpx.JSON(w, http.StatusConflict, a.currentStatus())
			return
		}
		httpx.JSON(w, record.HTTPStatus, record.Status)
		return
	}
	if !revisionMatches(a.status, req.ExpectedRevision) || a.status.Phase != PhaseReady {
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	transitionStatus(&a.status, PhaseCheckpointing, 20, "", "", false)
	accepted := prepareStatus(a.status)
	if operationID != "" {
		a.operations[operationID] = maintenanceOperation{Command: "start", HTTPStatus: http.StatusAccepted, Status: accepted}
	}
	a.mu.Unlock()
	go a.runCommand(context.Background(), "")
	httpx.JSON(w, http.StatusAccepted, accepted)
}

func (a *ExportApp) passwordAPI(w http.ResponseWriter, r *http.Request) {
	var req RestorePasswordRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	operationID, ok := commandOperationID(w, r, req.ClientOperationID)
	if !ok {
		return
	}
	a.mu.Lock()
	if a.operations == nil {
		a.operations = make(map[string]maintenanceOperation)
	}
	if record, replay, mismatch := operationReplay(a.operations, operationID, "password"); replay {
		a.mu.Unlock()
		if mismatch {
			httpx.JSON(w, http.StatusConflict, a.currentStatus())
			return
		}
		httpx.JSON(w, record.HTTPStatus, record.Status)
		return
	}
	if !revisionMatches(a.status, req.ExpectedRevision) || a.status.Phase != PhasePassword {
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	transitionStatus(&a.status, PhaseCheckpointing, 20, "", "", false)
	accepted := prepareStatus(a.status)
	if operationID != "" {
		a.operations[operationID] = maintenanceOperation{Command: "password", HTTPStatus: http.StatusAccepted, Status: accepted}
	}
	a.mu.Unlock()
	go a.runCommand(context.Background(), req.Password)
	httpx.JSON(w, http.StatusAccepted, accepted)
}

func (a *ExportApp) statusAPI(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, a.currentStatus())
}

func (a *ExportApp) downloadAPI(w http.ResponseWriter, r *http.Request) {
	status := a.currentStatus()
	if status.Phase != PhaseCompleted || status.ExportID != r.PathValue("id") || !status.DownloadAvailable {
		httpx.Error(w, panelerr.NotFound("backup export"))
		return
	}
	path := filepath.Join(a.cfg.DataRoot, "tmp", "backups", status.ExportID+".panel-backup")
	if _, err := os.Stat(path); err != nil {
		httpx.Error(w, panelerr.NotFound("backup export"))
		return
	}
	a.markDownloaded(status.ExportID)
	w.Header().Set("Content-Disposition", `attachment; filename="panel-`+filepath.Base(path)+`"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

// markDownloaded 记录归档已下载：内存标记之外再写一个持久化 marker 文件，
// 否则维护进程在用户下载后、退出前若被重启，内存标记丢失，用户会被
// backup_export_not_downloaded 锁在维护模式（归档实际已在客户端）。
func (a *ExportApp) markDownloaded(exportID string) {
	now := time.Now().UTC()
	a.mu.Lock()
	a.downloadedAt = now
	a.mu.Unlock()
	path := a.downloadMarkerPath(exportID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(now.Format(time.RFC3339Nano)), 0o600); err == nil {
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			logging.L().Warn("failed to persist backup download marker", zap.Error(err))
		}
	} else {
		logging.L().Warn("failed to persist backup download marker", zap.Error(err))
	}
}

func (a *ExportApp) downloadMarkerPath(exportID string) string {
	return filepath.Join(a.cfg.DataRoot, "tmp", "backups", exportID+".downloaded")
}

func (a *ExportApp) exitAPI(w http.ResponseWriter, r *http.Request) {
	status := a.currentStatus()
	if status.Phase != PhaseCompleted && status.Phase != PhaseFailed {
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	if status.DownloadAvailable && !a.downloadConfirmed() {
		// The archive has not been downloaded yet. Deleting it here would
		// silently destroy the only copy, so refuse and keep maintenance mode
		// running until the frontend confirms the download.
		httpx.Error(w, panelerr.Conflict("backup_export_not_downloaded", "Download the backup archive before exiting maintenance mode"))
		return
	}
	if err := a.cleanupTemporaryFiles(status); err != nil {
		// Cleanup failed: keep the files and the maintenance process so the
		// user can retry instead of silently losing or leaking the archive.
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, status)
	if status.RestartSupported {
		a.restarter.RestartSoon(MaintenanceModeNormal)
	} else {
		a.listener.shutdownSoon(800 * time.Millisecond)
	}
}

func (a *ExportApp) downloadConfirmed() bool {
	a.mu.RLock()
	confirmed := !a.downloadedAt.IsZero()
	a.mu.RUnlock()
	if confirmed {
		return true
	}
	// 内存标记在维护进程重启后丢失，回退检查持久化 marker。
	status := a.currentStatus()
	if status.ExportID == "" {
		return false
	}
	if _, err := os.Stat(a.downloadMarkerPath(status.ExportID)); err == nil {
		return true
	}
	return false
}

func (a *ExportApp) cleanupTemporaryFiles(status Status) error {
	var joined error
	if err := os.RemoveAll(exportPendingDir(a.cfg.DataRoot)); err != nil {
		joined = errors.Join(joined, fmt.Errorf("remove pending export marker: %w", err))
	}
	if status.ExportID != "" {
		path := filepath.Join(a.cfg.DataRoot, "tmp", "backups", status.ExportID+".panel-backup")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, fmt.Errorf("remove backup archive: %w", err))
		}
		// 下载确认 marker 与归档同生命周期，退出维护时一并清理。
		if err := os.Remove(a.downloadMarkerPath(status.ExportID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			joined = errors.Join(joined, fmt.Errorf("remove backup download marker: %w", err))
		}
	}
	return joined
}

func (a *ExportApp) run(ctx context.Context, password string) {
	marker, err := readPendingExport(a.cfg.DataRoot)
	if err != nil {
		a.fail("backup_pending_unavailable", "Unable to read pending backup export marker", false)
		return
	}
	a.setExportID(marker.ExportID)
	if err := checkpointSQLiteFiles(ctx, a.cfg.AppDatabase, a.cfg.LogDatabase, a.cfg.CoordinationDatabase, a.cfg.MetricsDatabase); err != nil {
		a.fail("backup_checkpoint_failed", "Unable to checkpoint databases", false)
		return
	}
	a.set(PhaseArchiving, 45, "")
	plain, manifest, err := buildArchive(ArchiveConfig{
		DataRoot:             a.cfg.DataRoot,
		AppDatabase:          a.cfg.AppDatabase,
		LogDatabase:          a.cfg.LogDatabase,
		CoordinationDatabase: a.cfg.CoordinationDatabase,
		MetricsDatabase:      a.cfg.MetricsDatabase,
		PanelVersion:         buildinfo.Version,
	}, marker.Encrypt)
	if err != nil {
		a.fail("backup_archive_failed", "Unable to build backup archive", false)
		return
	}
	raw := plain
	if marker.Encrypt {
		a.set(PhaseEncrypting, 75, "")
		raw, err = encryptBytes(plain, password)
		if err != nil {
			a.fail("backup_encrypt_failed", "Unable to encrypt backup archive", false)
			return
		}
	}
	dir := filepath.Join(a.cfg.DataRoot, "tmp", "backups")
	if err := os.MkdirAll(dir, 0700); err != nil {
		a.fail("backup_storage_failed", "Unable to prepare backup storage", false)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, marker.ExportID+".panel-backup"), raw, 0600); err != nil {
		a.fail("backup_storage_failed", "Unable to store backup archive", false)
		return
	}
	_ = os.RemoveAll(exportPendingDir(a.cfg.DataRoot))
	a.mu.Lock()
	transitionStatus(&a.status, PhaseCompleted, 100, "", "", false)
	a.status.DownloadAvailable = true
	a.status.Manifest = &manifest
	a.mu.Unlock()
}

func (a *ExportApp) runCommand(ctx context.Context, password string) {
	if a.runFn != nil {
		a.runFn(ctx, password)
		return
	}
	a.run(ctx, password)
}

func checkpointSQLiteFiles(ctx context.Context, paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		db, err := sql.Open("sqlite", sqliteFileDSN(path))
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
	return prepareStatus(a.status)
}

func (a *ExportApp) set(phase MaintenancePhase, progress int, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	transitionStatus(&a.status, phase, progress, "backup_operation_failed", message, phase == PhaseFailed)
}

func (a *ExportApp) setExportID(exportID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status.ExportID == exportID {
		return
	}
	a.status.ExportID = exportID
	a.status.Revision++
}

func (a *ExportApp) fail(code, message string, retryable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	transitionStatus(&a.status, PhaseFailed, 100, code, message, retryable)
}

func (a *ExportApp) static(w http.ResponseWriter, r *http.Request) {
	if maintenanceAPINotFound(w, r) {
		return
	}
	if redirectMaintenanceRoot(w, r) {
		return
	}
	if r.URL.Path != "/maintenance/backup" && r.URL.Path != "/maintenance/backup/" && !isMaintenanceAssetPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	dist := filepath.Join("web", "dist")
	index := filepath.Join(dist, "index.html")
	if _, err := os.Stat(index); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Panel backup export mode is running. Build the frontend into web/dist to serve the maintenance UI.\n"))
		return
	}
	if r.URL.Path == "/maintenance/backup" || r.URL.Path == "/maintenance/backup/" {
		http.ServeFile(w, r, index)
		return
	}
	rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
	path := filepath.Join(dist, rel)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.NotFound(w, r)
}

func isMaintenanceAssetPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.HasPrefix(clean, "/assets/") ||
		clean == "/favicon.ico" ||
		clean == "/manifest.webmanifest"
}
