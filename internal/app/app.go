package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/auth"
	"panel/internal/compose"
	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/docker"
	"panel/internal/httpx"
	"panel/internal/metrics"
	"panel/internal/overview"
	"panel/internal/packages"
	"panel/internal/panelerr"
	"panel/internal/scheduler"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

type App struct {
	cfg   config.Config
	store *storage.Store
	mux   *http.ServeMux
	auth  *auth.Service
	sched *scheduler.Scheduler
}

func New(cfg config.Config) (*App, error) {
	store, err := storage.Open(cfg)
	if err != nil {
		return nil, err
	}
	authSvc := auth.NewService(cfg)
	taskSvc := tasks.NewService(store.AppDB())
	credSvc := credential.NewService(store.AppDB(), cfg)
	executor := sshx.NewSSHExecutor(credSvc, cfg.RemoteTimeout())
	serverSvc := server.NewService(store.AppDB(), executor, taskSvc)
	dockerSvc := docker.NewService(store.AppDB(), serverSvc, docker.NewCLIRuntime(executor), taskSvc)
	composeSvc := compose.NewService(store.AppDB(), cfg.DataRoot, serverSvc, taskSvc, executor)
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, executor)
	packageSvc := packages.NewService(store.AppDB(), serverSvc, executor, taskSvc)
	overviewSvc := overview.NewService(serverSvc, metricsSvc, packageSvc)
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	sched := scheduler.New(settingsSvc, serverSvc, metricsSvc, dockerSvc, composeSvc, packageSvc, taskSvc)
	sched.Start(context.Background())

	a := &App{cfg: cfg, store: store, mux: http.NewServeMux(), auth: authSvc, sched: sched}
	a.routes(auth.NewHandler(authSvc), credential.NewHandler(credSvc), server.NewHandler(serverSvc), tasks.NewHandler(taskSvc), metrics.NewHandler(metricsSvc), packages.NewHandler(packageSvc), docker.NewHandler(dockerSvc), compose.NewHandler(composeSvc), overview.NewHandler(overviewSvc), settings.NewHandler(settingsSvc))
	return a, nil
}

func (a *App) Close() error {
	if a.sched != nil {
		a.sched.Stop()
	}
	return a.store.Close()
}
func (a *App) Handler() http.Handler { return a.mux }

func (a *App) routes(authH *auth.Handler, credH *credential.Handler, serverH *server.Handler, taskH *tasks.Handler, metricsH *metrics.Handler, packageH *packages.Handler, dockerH *docker.Handler, composeH *compose.Handler, overviewH *overview.Handler, settingsH *settings.Handler) {
	a.mux.HandleFunc("POST /api/v1/auth/login", authH.Login)
	a.mux.Handle("POST /api/v1/auth/logout", a.auth.RequireAuth(http.HandlerFunc(authH.Logout)))
	a.mux.HandleFunc("GET /api/v1/auth/session", authH.Session)

	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/api/v1/credentials":
			credH.List(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/credentials":
			credH.Create(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/credentials/"):
			credH.Update(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/credentials/"):
			credH.Delete(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/servers":
			serverH.List(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/variables") && strings.HasPrefix(path, "/api/v1/servers/"):
			composeH.GetServerVariables(w, r)
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/variables") && strings.HasPrefix(path, "/api/v1/servers/"):
			composeH.PutServerVariables(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/servers":
			serverH.Create(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/servers/") && serverResourcePath(path):
			serverH.Update(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/servers/") && serverResourcePath(path):
			serverH.Delete(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/test"):
			serverH.Test(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/overview":
			overviewH.Get(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/metrics"):
			metricsH.Query(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/packages/updates"):
			packageH.List(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/packages/refresh"):
			packageH.Refresh(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/packages/upgrade-selected"):
			packageH.UpgradeSelected(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/packages/upgrade-all"):
			packageH.UpgradeAll(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/capability"):
			dockerH.Capability(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/refresh"):
			dockerH.Refresh(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/projects"):
			dockerH.Projects(w, r)
		case r.Method == http.MethodGet && strings.Contains(path, "/docker/projects/") && strings.HasSuffix(path, "/status"):
			dockerH.ProjectStatus(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/services"):
			dockerH.Services(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/networks"):
			dockerH.Networks(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/volumes"):
			dockerH.Volumes(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/docker/images"):
			dockerH.Images(w, r)
		case r.Method == http.MethodDelete && strings.Contains(path, "/docker/networks/"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/networks/prune"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodDelete && strings.Contains(path, "/docker/volumes/"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/volumes/prune"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodDelete && strings.Contains(path, "/docker/images/"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodPost && strings.Contains(path, "/docker/images/"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/service-templates":
			composeH.ListTemplates(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/service-templates":
			composeH.CreateTemplate(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/validate"):
			composeH.ValidateTemplate(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/render-preview"):
			composeH.RenderTemplate(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/services"):
			composeH.TemplateServices(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/files"):
			composeH.ListFiles(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/files/binary"):
			composeH.CreateBinaryFile(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.HasSuffix(path, "/files/template"):
			composeH.CreateTemplateFile(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.Contains(path, "/files/"):
			composeH.UpdateFile(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/service-templates/") && strings.Contains(path, "/files/"):
			composeH.DeleteFile(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/service-templates/") && serviceTemplateResourcePath(path):
			composeH.GetTemplate(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/service-templates/") && serviceTemplateResourcePath(path):
			composeH.UpdateTemplate(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/service-templates/") && serviceTemplateResourcePath(path):
			composeH.DeleteTemplate(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/services":
			composeH.ListServices(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/services":
			composeH.CreateService(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/services/") && strings.HasSuffix(path, "/render"):
			composeH.RenderService(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/services/") && serviceLifecyclePath(path):
			composeH.Lifecycle(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/services/") && serviceResourcePath(path):
			composeH.GetService(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/services/") && serviceResourcePath(path):
			composeH.UpdateService(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/services/") && serviceResourcePath(path):
			composeH.DeleteService(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/tasks":
			taskH.List(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/run-now") && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.RunNow(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/logs") && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.Logs(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.Get(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/settings/runtime":
			settingsH.Runtime(w, r)
		case r.Method == http.MethodPut && path == "/api/v1/settings/runtime":
			settingsH.UpdateRuntime(w, r)
		default:
			httpx.Error(w, panelerr.NotFound("route"))
		}
	})
	a.mux.Handle("/api/v1/", a.auth.RequireAuth(api))
	a.mux.HandleFunc("/", a.static)
}

func serverResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[3] != ""
}

func serviceTemplateResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "service-templates" && parts[3] != ""
}

func serviceResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "services" && parts[3] != ""
}

func serviceLifecyclePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "services" || parts[3] == "" {
		return false
	}
	switch parts[4] {
	case "deploy", "sync", "restart", "stop", "remove", "update-images":
		return true
	default:
		return false
	}
}

func (a *App) static(w http.ResponseWriter, r *http.Request) {
	dist := filepath.Join("web", "dist")
	if _, err := os.Stat(dist); err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Panel backend is running. Build the frontend into web/dist to serve the UI.\n"))
		return
	}
	rel := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
	path := filepath.Join(dist, rel)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(dist, "index.html"))
}
