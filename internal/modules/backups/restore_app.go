package backups

import (
	"context"
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
)

type RestoreApp struct {
	cfg       config.Config
	mux       *http.ServeMux
	mu        sync.RWMutex
	status    Status
	restarter Restarter
}

func PendingRestoreExists(dataRoot string) bool {
	_, err := os.Stat(filepath.Join(pendingDir(dataRoot), "pending.json"))
	return err == nil
}

func NewRestoreApp(cfg config.Config) (*RestoreApp, error) {
	restarter := NewContainerRestarter()
	app := &RestoreApp{
		cfg:       cfg,
		mux:       http.NewServeMux(),
		restarter: restarter,
		status: Status{
			Mode:             ModeRestoreRunning,
			Phase:            PhaseIdle,
			Progress:         0,
			StartedAt:        time.Now().UTC(),
			RestartSupported: restarter.Supported(),
		},
	}
	app.routes()
	marker, err := readPending(cfg.DataRoot)
	if err != nil {
		app.fail("Unable to read pending restore marker")
		return app, nil
	}
	app.status.Manifest = &marker.Manifest
	if marker.Manifest.Encrypted {
		app.status.Phase = PhasePassword
	} else {
		go app.apply(context.Background(), "")
	}
	return app, nil
}

func (a *RestoreApp) Handler() http.Handler { return a.mux }
func (a *RestoreApp) Close() error          { return nil }

func (a *RestoreApp) routes() {
	a.mux.HandleFunc("GET /api/v1/restore/status", a.statusAPI)
	a.mux.HandleFunc("POST /api/v1/restore/password", a.passwordAPI)
	a.mux.HandleFunc("POST /api/v1/restore/retry", a.retryAPI)
	a.mux.HandleFunc("POST /api/v1/restore/clear-pending", a.clearPendingAPI)
	a.mux.HandleFunc("/", a.page)
}

func (a *RestoreApp) statusAPI(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, a.currentStatus())
}

func (a *RestoreApp) passwordAPI(w http.ResponseWriter, r *http.Request) {
	var req RestorePasswordRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	go a.apply(context.Background(), req.Password)
	httpx.JSON(w, http.StatusAccepted, a.currentStatus())
}

func (a *RestoreApp) retryAPI(w http.ResponseWriter, r *http.Request) {
	go a.apply(r.Context(), "")
	httpx.JSON(w, http.StatusAccepted, a.currentStatus())
}

func (a *RestoreApp) clearPendingAPI(w http.ResponseWriter, r *http.Request) {
	_ = os.RemoveAll(pendingDir(a.cfg.DataRoot))
	a.mu.Lock()
	a.status.Phase = PhaseCompleted
	a.status.Progress = 100
	a.status.FinishedAt = time.Now().UTC()
	a.status.Error = ""
	a.mu.Unlock()
	status := a.currentStatus()
	httpx.JSON(w, http.StatusOK, status)
	if status.RestartSupported {
		a.restarter.RestartSoon()
	}
}

func (a *RestoreApp) apply(ctx context.Context, password string) {
	a.set(PhaseExtracting, 15, "")
	marker, err := readPending(a.cfg.DataRoot)
	if err != nil {
		a.fail("Unable to read pending restore marker")
		return
	}
	raw, err := os.ReadFile(filepath.Join(pendingDir(a.cfg.DataRoot), marker.ArchiveFilename))
	if err != nil {
		a.fail("Unable to read pending backup archive")
		return
	}
	_, plain, err := readManifest(raw, password)
	if err != nil {
		if errors.Is(err, errPasswordRequired) {
			a.set(PhasePassword, 10, "")
			return
		}
		if errors.Is(err, errPasswordInvalid) {
			a.set(PhasePassword, 10, "Backup password is invalid")
			return
		}
		a.fail("Backup archive is invalid")
		return
	}
	staging, err := os.MkdirTemp("", "panel-restore-staging-*")
	if err != nil {
		a.fail("Unable to prepare restore staging directory")
		return
	}
	defer os.RemoveAll(staging)
	if err := extractArchive(plain, staging); err != nil {
		a.fail("Unable to extract backup archive")
		return
	}
	a.set(PhaseApplying, 60, "")
	if err := a.applyStaged(ctx, staging); err != nil {
		a.fail("Unable to apply restored data")
		return
	}
	_ = os.RemoveAll(pendingDir(a.cfg.DataRoot))
	a.set(PhaseCompleted, 100, "")
	if a.restarter.Supported() {
		a.restarter.RestartSoon()
	}
}

func (a *RestoreApp) applyStaged(ctx context.Context, staging string) error {
	_ = ctx
	if err := os.RemoveAll(a.cfg.DataRoot); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(staging, "dataRoot"), a.cfg.DataRoot); err != nil {
		return err
	}
	for _, item := range []struct {
		src string
		dst string
	}{
		{filepath.Join(staging, "databases", "app.db"), a.cfg.AppDatabase},
		{filepath.Join(staging, "databases", "tasks.db"), a.cfg.TaskDatabase},
		{filepath.Join(staging, "databases", "metrics.db"), a.cfg.MetricsDatabase},
	} {
		if _, err := os.Stat(item.src); err == nil {
			if err := os.MkdirAll(filepath.Dir(item.dst), 0700); err != nil {
				return err
			}
			if err := copyFile(item.src, item.dst, 0600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *RestoreApp) currentStatus() Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *RestoreApp) set(phase string, progress int, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.Phase = phase
	a.status.Progress = progress
	a.status.Error = message
	if phase == PhaseCompleted || phase == PhaseFailed {
		a.status.FinishedAt = time.Now().UTC()
	}
}

func (a *RestoreApp) fail(message string) {
	a.set(PhaseFailed, 100, message)
}

func readPending(dataRoot string) (pendingRestore, error) {
	raw, err := os.ReadFile(filepath.Join(pendingDir(dataRoot), "pending.json"))
	if err != nil {
		return pendingRestore{}, err
	}
	var marker pendingRestore
	if err := json.Unmarshal(raw, &marker); err != nil {
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
input{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #ccd3df;border-radius:8px;margin-top:10px}button{margin-top:10px;padding:10px 14px;border:0;border-radius:8px;background:#2563eb;color:white;font-weight:700;cursor:pointer}
</style>
</head>
<body><main class="shell"><section class="panel">
<h1>Panel restore mode</h1><p class="muted">Normal startup is blocked while the pending restore is handled.</p>
<progress id="progress" value="0" max="100"></progress>
<div class="row"><span>Phase</span><strong id="phase">loading</strong></div>
<div class="row"><span>Backup created</span><span id="created">-</span></div>
<div class="row"><span>Panel version</span><span id="version">-</span></div>
<div class="row"><span>Encrypted</span><span id="encrypted">-</span></div>
<div id="passwordBox" style="display:none"><input id="password" type="password" placeholder="Backup password"><button onclick="submitPassword()">Continue restore</button></div>
<div id="error" class="error" style="display:none"></div>
<p class="muted" id="done" style="display:none"></p>
</section></main>
<script>
async function load(){const r=await fetch('/api/v1/restore/status');const e=await r.json();const s=e.data;document.getElementById('progress').value=s.progress||0;document.getElementById('phase').textContent=s.phase||'-';document.getElementById('created').textContent=s.manifest?.createdAt?new Date(s.manifest.createdAt).toLocaleString():'-';document.getElementById('version').textContent=s.manifest?.panelVersion||'-';document.getElementById('encrypted').textContent=s.manifest?.encrypted?'yes':'no';document.getElementById('passwordBox').style.display=s.phase==='password_required'?'block':'none';const done=document.getElementById('done');done.style.display=s.phase==='completed'?'block':'none';done.textContent=s.restartSupported?'Restore completed. Panel is restarting.':'Restore completed. Restart Panel to continue.';const err=document.getElementById('error');err.style.display=s.error?'block':'none';err.textContent=s.error||'';if(s.phase!=='completed')setTimeout(load,1500)}
async function submitPassword(){await fetch('/api/v1/restore/password',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:document.getElementById('password').value})});load()}
load()
</script></body></html>`))
