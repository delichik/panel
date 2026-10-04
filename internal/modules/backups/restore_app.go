package backups

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"panel/internal/platform/config"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/paneltls"
)

type RestoreApp struct {
	cfg             config.Config
	mux             *http.ServeMux
	listener        maintenanceListener
	mu              sync.RWMutex
	status          Status
	restarter       Restarter
	auth            *maintenanceAuth
	operations      map[string]maintenanceOperation
	applyFn         func(context.Context, string)
	transactionHook func(string) error
}

func PendingRestoreExists(dataRoot string) bool {
	if err := resolvePendingPublication(dataRoot); err != nil {
		// The pending publication could not be resolved to a known-good state.
		// If any restore media is still present, fail closed by forcing restore
		// mode instead of treating the error as "no restore pending" and
		// starting normally with potentially torn or unverified media.
		if pathExists(pendingDir(dataRoot)) || pathExists(restoreTransactionMediaDir(dataRoot)) || pathExists(restoreTransactionStatePath(dataRoot)) {
			return true
		}
		return false
	}
	return pathExists(filepath.Join(pendingLocation(dataRoot), "pending.json")) || pathExists(restoreTransactionStatePath(dataRoot))
}

func RestoreRecoveryRequired(dataRoot string) bool {
	for _, path := range []string{restoreTransactionStatePath(dataRoot), restoreTransactionMediaDir(dataRoot)} {
		_, err := os.Stat(path)
		if err == nil || !os.IsNotExist(err) {
			return true
		}
	}
	return false
}

func NewRestoreApp(cfg config.Config) (*RestoreApp, error) {
	restarter := NewPanelInitRestarter(cfg.DataRoot)
	app := &RestoreApp{
		cfg:        cfg,
		mux:        http.NewServeMux(),
		restarter:  restarter,
		operations: make(map[string]maintenanceOperation),
		status: Status{
			SchemaVersion:    MaintenanceStatusSchemaVersion,
			Revision:         1,
			Mode:             ModeRestoreRunning,
			Phase:            PhaseIdle,
			Progress:         0,
			StartedAt:        time.Now().UTC(),
			RestartSupported: restarter.Supported(),
		},
	}
	marker, markerErr := readPending(cfg.DataRoot)
	var auth *maintenanceAuth
	if markerErr == nil && marker.MaintenanceAuth != nil && validMaintenanceCredential(*marker.MaintenanceAuth) {
		auth = newMaintenanceAuthWithCredential(maintenanceAuthRestore, *marker.MaintenanceAuth)
	} else {
		var err error
		auth, err = newMaintenanceAuth(context.Background(), maintenanceAuthRestore, cfg.AppDatabase, maintenanceCredential{
			Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash,
		})
		if err != nil {
			return nil, err
		}
	}
	app.auth = auth
	app.routes()
	if markerErr == nil {
		app.status.Manifest = &marker.Manifest
	}
	recovery := recoverRestoreTransaction(cfg.DataRoot)
	if recovery.Found {
		switch {
		case recovery.Committed:
			cleanupErr := os.RemoveAll(pendingDir(cfg.DataRoot))
			if cleanupErr == nil {
				cleanupErr = cleanupRestoreTransaction(cfg.DataRoot)
			}
			if cleanupErr != nil {
				app.status.ClearPendingBlocked = true
				transitionStatus(&app.status, PhaseFailed, 100, "restore_commit_cleanup_failed", "Restore committed but cleanup is incomplete", false)
			} else {
				transitionStatus(&app.status, PhaseCompleted, 100, "", "", false)
				if restarter.Supported() {
					restarter.RestartSoon(MaintenanceModeNormal)
				}
			}
		case recovery.RollbackBlocked:
			app.status.ClearPendingBlocked = true
			transitionStatus(&app.status, PhaseFailed, 100, "restore_rollback_failed", "Restore rollback could not be completed", false)
		case recovery.RolledBack:
			if markerErr != nil {
				transitionStatus(&app.status, PhaseFailed, 100, "restore_media_unavailable", "Interrupted restore was rolled back but restore media is unavailable", false)
			} else {
				transitionStatus(&app.status, PhaseFailed, 100, "restore_interrupted_rolled_back", "Interrupted restore was rolled back safely", true)
			}
		}
		return app, nil
	}
	if markerErr != nil {
		app.fail("restore_pending_unavailable", "Unable to read pending restore marker", false)
		return app, nil
	}
	if marker.Manifest.Encrypted {
		transitionStatus(&app.status, PhasePassword, 10, "", "", false)
	} else {
		transitionStatus(&app.status, PhaseExtracting, 15, "", "", false)
		go app.applyCommand(context.Background(), "")
	}
	return app, nil
}

func (a *RestoreApp) Handler() http.Handler { return a.mux }

func (a *RestoreApp) ListenAndServeTLS(address string) error {
	return a.listener.listenAndServeTLS(address, a.Handler(), a.tlsConfig())
}

func (a *RestoreApp) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		certificate, err := paneltls.FixedCertificate(a.cfg.DataRoot, "")
		return &certificate, err
	}}
}

func (a *RestoreApp) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.listener.shutdown(ctx)
}

func (a *RestoreApp) routes() {
	a.mux.HandleFunc("POST /api/v1/auth/login", a.auth.loginAPI)
	a.mux.HandleFunc("GET /api/v1/auth/session", a.auth.sessionAPI)
	a.mux.HandleFunc("POST /api/v1/auth/logout", a.auth.logoutAPI)
	a.mux.HandleFunc("GET /api/v1/restore/status", a.auth.require(a.statusAPI))
	a.mux.HandleFunc("POST /api/v1/restore/password", a.auth.require(a.passwordAPI))
	a.mux.HandleFunc("POST /api/v1/restore/retry", a.auth.require(a.retryAPI))
	a.mux.HandleFunc("POST /api/v1/restore/clear-pending", a.auth.require(a.clearPendingAPI))
	a.mux.HandleFunc("GET /maintenance/restore", a.page)
	a.mux.HandleFunc("GET /maintenance/restore/", a.page)
	a.mux.HandleFunc("/", a.fallback)
}

func (a *RestoreApp) statusAPI(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, a.currentStatus())
}

func (a *RestoreApp) passwordAPI(w http.ResponseWriter, r *http.Request) {
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
	transitionStatus(&a.status, PhaseExtracting, 15, "", "", false)
	accepted := prepareStatus(a.status)
	if operationID != "" {
		a.operations[operationID] = maintenanceOperation{Command: "password", HTTPStatus: http.StatusAccepted, Status: accepted}
	}
	a.mu.Unlock()
	go a.applyCommand(context.Background(), req.Password)
	httpx.JSON(w, http.StatusAccepted, accepted)
}

func (a *RestoreApp) retryAPI(w http.ResponseWriter, r *http.Request) {
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
	if record, replay, mismatch := operationReplay(a.operations, operationID, "retry"); replay {
		a.mu.Unlock()
		if mismatch {
			httpx.JSON(w, http.StatusConflict, a.currentStatus())
			return
		}
		httpx.JSON(w, record.HTTPStatus, record.Status)
		return
	}
	if !revisionMatches(a.status, req.ExpectedRevision) || a.status.Phase != PhaseFailed || a.status.ErrorDetail == nil || !a.status.ErrorDetail.Retryable {
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	requiresPassword := a.status.Manifest != nil && a.status.Manifest.Encrypted
	if requiresPassword {
		transitionStatus(&a.status, PhasePassword, 10, "", "", false)
	} else {
		transitionStatus(&a.status, PhaseExtracting, 15, "", "", false)
	}
	accepted := prepareStatus(a.status)
	if operationID != "" {
		a.operations[operationID] = maintenanceOperation{Command: "retry", HTTPStatus: http.StatusAccepted, Status: accepted}
	}
	a.mu.Unlock()
	if requiresPassword {
		httpx.JSON(w, http.StatusAccepted, accepted)
		return
	}
	go a.applyCommand(context.Background(), "")
	httpx.JSON(w, http.StatusAccepted, accepted)
}

func (a *RestoreApp) clearPendingAPI(w http.ResponseWriter, r *http.Request) {
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
	if record, replay, mismatch := operationReplay(a.operations, operationID, "clear-pending"); replay {
		a.mu.Unlock()
		if mismatch {
			httpx.JSON(w, http.StatusConflict, a.currentStatus())
			return
		}
		httpx.JSON(w, record.HTTPStatus, record.Status)
		return
	}
	if !revisionMatches(a.status, req.ExpectedRevision) || a.status.ClearPendingBlocked || (a.status.Phase != PhasePassword && a.status.Phase != PhaseFailed) {
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusConflict, status)
		return
	}
	if err := os.RemoveAll(pendingDir(a.cfg.DataRoot)); err != nil {
		transitionStatus(&a.status, PhaseFailed, 100, "restore_pending_clear_failed", "Unable to clear pending restore", true)
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusInternalServerError, status)
		return
	}
	if err := cleanupRestoreTransaction(a.cfg.DataRoot); err != nil {
		a.status.ClearPendingBlocked = true
		transitionStatus(&a.status, PhaseFailed, 100, "restore_pending_clear_failed", "Unable to clear restore transaction", false)
		status := prepareStatus(a.status)
		a.mu.Unlock()
		httpx.JSON(w, http.StatusInternalServerError, status)
		return
	}
	transitionStatus(&a.status, PhaseCompleted, 100, "", "", false)
	status := prepareStatus(a.status)
	if operationID != "" {
		a.operations[operationID] = maintenanceOperation{Command: "clear-pending", HTTPStatus: http.StatusOK, Status: status}
	}
	a.mu.Unlock()
	httpx.JSON(w, http.StatusOK, status)
	if status.RestartSupported {
		a.restarter.RestartSoon(MaintenanceModeNormal)
	} else {
		a.listener.shutdownSoon(800 * time.Millisecond)
	}
}

func (a *RestoreApp) apply(ctx context.Context, password string) {
	marker, err := readPending(a.cfg.DataRoot)
	if err != nil {
		a.fail("restore_pending_unavailable", "Unable to read pending restore marker", true)
		return
	}
	raw, err := os.ReadFile(filepath.Join(pendingLocation(a.cfg.DataRoot), marker.ArchiveFilename))
	if err != nil {
		a.fail("restore_archive_unavailable", "Unable to read pending backup archive", true)
		return
	}
	_, plain, err := readManifest(raw, password)
	if err != nil {
		if errors.Is(err, errPasswordRequired) {
			a.set(PhasePassword, 10, "", "", false)
			return
		}
		if errors.Is(err, errPasswordInvalid) {
			a.set(PhasePassword, 10, "restore_password_invalid", "Backup password is invalid", true)
			return
		}
		a.fail("restore_archive_invalid", "Backup archive is invalid", false)
		return
	}
	staging, err := os.MkdirTemp("", "panel-restore-staging-*")
	if err != nil {
		a.fail("restore_staging_failed", "Unable to prepare restore staging directory", true)
		return
	}
	defer os.RemoveAll(staging)
	if err := extractArchive(plain, staging); err != nil {
		a.fail("restore_extract_failed", "Unable to extract backup archive", false)
		return
	}
	if err := ensureRestoreTransactionMedia(a.cfg.DataRoot); err != nil {
		a.fail("restore_media_protection_failed", "Unable to protect restore media", true)
		return
	}
	state, err := prepareRestoreTransaction(a.cfg, staging)
	if err != nil {
		a.fail("restore_prepare_failed", "Unable to prepare restore transaction", true)
		return
	}
	a.set(PhaseApplying, 60, "", "", false)
	if err := applyRestoreTransaction(a.cfg.DataRoot, state, a.transactionHook); err != nil {
		if errors.Is(err, errSimulatedRestoreCrash) {
			return
		}
		recovery := recoverRestoreTransaction(a.cfg.DataRoot)
		if recovery.RollbackBlocked {
			a.mu.Lock()
			a.status.ClearPendingBlocked = true
			transitionStatus(&a.status, PhaseFailed, 100, "restore_rollback_failed", "Restore rollback could not be completed", false)
			a.mu.Unlock()
			return
		}
		a.fail("restore_apply_failed", "Unable to apply restored data; previous data was restored", true)
		return
	}
	if err := os.RemoveAll(pendingDir(a.cfg.DataRoot)); err != nil {
		a.mu.Lock()
		a.status.ClearPendingBlocked = true
		transitionStatus(&a.status, PhaseFailed, 100, "restore_commit_cleanup_failed", "Restore committed but cleanup is incomplete", false)
		a.mu.Unlock()
		return
	}
	if err := cleanupRestoreTransaction(a.cfg.DataRoot); err != nil {
		a.mu.Lock()
		a.status.ClearPendingBlocked = true
		transitionStatus(&a.status, PhaseFailed, 100, "restore_commit_cleanup_failed", "Restore committed but cleanup is incomplete", false)
		a.mu.Unlock()
		return
	}
	a.set(PhaseCompleted, 100, "", "", false)
	if a.restarter.Supported() {
		a.restarter.RestartSoon(MaintenanceModeNormal)
	} else {
		a.listener.shutdownSoon(5 * time.Second)
	}
}

func (a *RestoreApp) applyCommand(ctx context.Context, password string) {
	if a.applyFn != nil {
		a.applyFn(ctx, password)
		return
	}
	a.apply(ctx, password)
}

func (a *RestoreApp) currentStatus() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return prepareStatus(a.status)
}

func (a *RestoreApp) set(phase MaintenancePhase, progress int, code, message string, retryable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	transitionStatus(&a.status, phase, progress, code, message, retryable)
}

func (a *RestoreApp) fail(code, message string, retryable bool) {
	a.set(PhaseFailed, 100, code, message, retryable)
}

func readPending(dataRoot string) (pendingRestore, error) {
	location := pendingLocation(dataRoot)
	if location == pendingDir(dataRoot) {
		if err := resolvePendingPublication(dataRoot); err != nil {
			return pendingRestore{}, err
		}
		location = pendingLocation(dataRoot)
	}
	if err := validatePendingDirectory(location); err != nil {
		return pendingRestore{}, err
	}
	raw, err := os.ReadFile(filepath.Join(location, "pending.json"))
	if err != nil {
		return pendingRestore{}, err
	}
	var marker pendingRestore
	if err := json.Unmarshal(raw, &marker); err != nil {
		return pendingRestore{}, err
	}
	if err := upgradeLegacyPendingIntegrity(location, &marker); err != nil {
		return pendingRestore{}, err
	}
	return marker, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(in)
	return err
}

func (a *RestoreApp) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = restorePageTemplate.Execute(w, nil)
}

func (a *RestoreApp) fallback(w http.ResponseWriter, r *http.Request) {
	if maintenanceAPINotFound(w, r) {
		return
	}
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/maintenance/restore", http.StatusTemporaryRedirect)
		return
	}
	http.NotFound(w, r)
}

var restorePageTemplate = template.Must(template.New("restore").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Panel restore</title>
<style>
body{margin:0;font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f6f7fb;color:#172033}
.shell{min-height:100vh;display:grid;place-items:center;padding:24px}
.panel{width:min(720px,100%);background:#fff;border:1px solid #dfe3ea;border-radius:8px;padding:24px;box-shadow:0 8px 28px rgba(20,30,50,.08)}
h1{margin:0 0 8px;font-size:24px}.muted{color:#667085}.row{display:flex;justify-content:space-between;gap:16px;border-top:1px solid #edf0f5;padding:12px 0}
progress{width:100%;height:18px}.error{color:#b42318;background:#fff1f3;border:1px solid #fecdd6;padding:12px;border-radius:8px;margin-top:12px}
input{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #ccd3df;border-radius:8px;margin-top:10px}button{margin-top:10px;padding:10px 14px;border:0;border-radius:8px;background:#2563eb;color:white;font-weight:700;cursor:pointer}.secondary{background:#667085}.actions{display:flex;gap:8px;flex-wrap:wrap}
</style>
</head>
<body><main class="shell"><section class="panel">
<h1>Panel restore mode</h1><p class="muted">Normal startup is blocked while the pending restore is handled.</p>
<div id="loginBox"><input id="username" autocomplete="username" placeholder="Administrator username"><input id="loginPassword" type="password" autocomplete="current-password" placeholder="Administrator password"><button onclick="submitLogin()">Sign in</button><div id="loginError" class="error" style="display:none"></div></div>
<div id="statusBox" style="display:none"><progress id="progress" value="0" max="100"></progress>
<div class="row"><span>Phase</span><strong id="phase">loading</strong></div>
<div class="row"><span>Backup created</span><span id="created">-</span></div>
<div class="row"><span>Panel version</span><span id="version">-</span></div>
<div class="row"><span>Encrypted</span><span id="encrypted">-</span></div>
<div id="passwordBox" style="display:none"><input id="password" type="password" placeholder="Backup password"><button onclick="submitPassword()">Continue restore</button></div>
<div id="error" class="error" style="display:none"></div>
<p class="muted" id="done" style="display:none"></p>
<div class="actions"><button class="secondary" onclick="logout()">Sign out</button></div></div>
</section></main>
<script>
let token=sessionStorage.getItem('panelRestoreToken')||'';
function authHeaders(json=false){const h={Authorization:'Bearer '+token};if(json)h['Content-Type']='application/json';return h}
function showLogin(message=''){document.getElementById('loginBox').style.display='block';document.getElementById('statusBox').style.display='none';const err=document.getElementById('loginError');err.style.display=message?'block':'none';err.textContent=message}
async function submitLogin(){const r=await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:document.getElementById('username').value,password:document.getElementById('loginPassword').value})});const e=await r.json();if(!r.ok){showLogin(e.error?.message||'Authentication failed');return}token=e.data.token;sessionStorage.setItem('panelRestoreToken',token);document.getElementById('loginBox').style.display='none';document.getElementById('statusBox').style.display='block';load()}
async function load(){if(!token){showLogin();return}const r=await fetch('/api/v1/restore/status',{headers:authHeaders()});if(r.status===401){token='';sessionStorage.removeItem('panelRestoreToken');showLogin('Your maintenance session expired. Sign in again.');return}const e=await r.json();const s=e.data;document.getElementById('loginBox').style.display='none';document.getElementById('statusBox').style.display='block';document.getElementById('progress').value=s.progress||0;document.getElementById('phase').textContent=s.phase||'-';document.getElementById('created').textContent=s.manifest?.createdAt?new Date(s.manifest.createdAt).toLocaleString():'-';document.getElementById('version').textContent=s.manifest?.panelVersion||'-';document.getElementById('encrypted').textContent=s.manifest?.encrypted?'yes':'no';document.getElementById('passwordBox').style.display=s.capabilities?.canSubmitPassword?'block':'none';const done=document.getElementById('done');done.style.display=s.phase==='completed'?'block':'none';done.textContent=s.restartSupported?'Restore completed. Panel is restarting.':'Restore completed. Restart Panel to continue.';const err=document.getElementById('error');err.style.display=s.error?'block':'none';err.textContent=s.error||'';if(s.phase!=='completed')setTimeout(load,s.pollAfterMs||1500)}
async function submitPassword(){await fetch('/api/v1/restore/password',{method:'POST',headers:authHeaders(true),body:JSON.stringify({password:document.getElementById('password').value})});load()}
async function logout(){if(token)await fetch('/api/v1/auth/logout',{method:'POST',headers:authHeaders()});token='';sessionStorage.removeItem('panelRestoreToken');showLogin()}
load()
</script></body></html>`))
