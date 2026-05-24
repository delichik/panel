package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/applications"
	"panel/internal/auth"
	"panel/internal/config"
	"panel/internal/containerops"
	"panel/internal/containerservice"
	"panel/internal/credential"
	"panel/internal/docker"
	"panel/internal/httpx"
	"panel/internal/metrics"
	"panel/internal/nomad"
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
	containerSvc := containerservice.NewService(store.AppDB(), taskSvc)
	containerSvc.SetLogReader(containerservice.NewDockerLogReader(serverSvc, executor))
	dockerSvc.SetContainerServiceRestarter(containerSvc)
	containerWorker := containerops.NewWorker(taskSvc, containerops.NewLeaseService(store.AppDB(), time.Minute), containerSvc, serverSvc, executor)
	nomadClient := nomad.NewClient(nomad.Config{
		Address:    cfg.Nomad.Address,
		Token:      cfg.Nomad.Token,
		Namespace:  cfg.Nomad.Namespace,
		Region:     cfg.Nomad.Region,
		Datacenter: cfg.Nomad.Datacenter,
	}, nil)
	applicationSvc := applications.NewService(store.AppDB(), nomadClient, taskSvc, applications.Config{
		Namespace:  cfg.Nomad.Namespace,
		Region:     cfg.Nomad.Region,
		Datacenter: cfg.Nomad.Datacenter,
	})
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, executor)
	packageSvc := packages.NewService(store.AppDB(), serverSvc, executor, taskSvc)
	overviewSvc := overview.NewService(serverSvc, metricsSvc, packageSvc)
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	sched := scheduler.New(settingsSvc, serverSvc, metricsSvc, dockerSvc, packageSvc, taskSvc, containerWorker)
	sched.Start(context.Background())

	a := &App{cfg: cfg, store: store, mux: http.NewServeMux(), auth: authSvc, sched: sched}
	a.routes(auth.NewHandler(authSvc), credential.NewHandler(credSvc), server.NewHandler(serverSvc), tasks.NewHandler(taskSvc, sched), metrics.NewHandler(metricsSvc), packages.NewHandler(packageSvc), docker.NewHandler(dockerSvc), applications.NewHandler(applicationSvc), overview.NewHandler(overviewSvc), settings.NewHandler(settingsSvc))
	return a, nil
}

func (a *App) Close() error {
	if a.sched != nil {
		a.sched.Stop()
	}
	return a.store.Close()
}
func (a *App) Handler() http.Handler { return a.mux }

func (a *App) routes(authH *auth.Handler, credH *credential.Handler, serverH *server.Handler, taskH *tasks.Handler, metricsH *metrics.Handler, packageH *packages.Handler, dockerH *docker.Handler, applicationH *applications.Handler, overviewH *overview.Handler, settingsH *settings.Handler) {
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
		case r.Method == http.MethodPost && strings.Contains(path, "/docker/containers/"):
			dockerH.NotImplemented(w, r)
		case r.Method == http.MethodDelete && strings.Contains(path, "/docker/containers/"):
			dockerH.NotImplemented(w, r)
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
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/runtime-explorer/nodes/") && runtimeExplorerNodeResource(path):
			dockerH.RuntimeExplorer(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/runtime-explorer/nodes/") && strings.Contains(path, "/containers/"):
			dockerH.RuntimeExplorer(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/runtime-explorer/nodes/") && strings.Contains(path, "/containers/"):
			dockerH.RuntimeExplorer(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/runtime-explorer/nodes/") && strings.HasSuffix(path, "/prune"):
			dockerH.RuntimeExplorer(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/applications":
			applicationH.List(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/applications":
			applicationH.Create(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/validate"):
			applicationH.Validate(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/plan"):
			applicationH.Plan(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/deploy"):
			applicationH.Deploy(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/stop"):
			applicationH.Stop(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/restart"):
			applicationH.Restart(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/runtime"):
			applicationH.Runtime(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/logs"):
			applicationH.Logs(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/applications/") && applicationResourcePath(path):
			applicationH.Get(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/applications/") && applicationResourcePath(path):
			applicationH.Update(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/applications/") && applicationResourcePath(path):
			applicationH.Delete(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/tasks":
			taskH.List(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/retry") && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.Retry(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/run-now") && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.RunNow(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/steps") && strings.HasPrefix(path, "/api/v1/tasks/"):
			taskH.Steps(w, r)
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

func applicationResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "applications" && parts[3] != ""
}

func runtimeExplorerNodeResource(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "runtime-explorer" && parts[3] == "nodes" && parts[4] != ""
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
