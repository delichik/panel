package panel

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentclient "panel/internal/agent/client"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/certs"
	"panel/internal/modules/certificates/dns"
	"panel/internal/modules/containers"
	"panel/internal/modules/identity"
	"panel/internal/modules/keyassets"
	"panel/internal/modules/observability/diagnostics"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/observability/overview"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/settings"
	"panel/internal/modules/systeminfo"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/logging"
	"panel/internal/platform/secrets"
	"panel/internal/platform/ssh"

	"go.uber.org/zap"
)

type App struct {
	cfg            config.Config
	store          *database.Store
	mux            *http.ServeMux
	auth           *auth.Service
	tasks          *tasks.Worker
	metricsCleanup *metrics.CleanupWorker
	system         *systeminfo.Service
}

func New(cfg config.Config) (*App, error) {
	store, err := database.Open(cfg)
	if err != nil {
		return nil, err
	}
	agentTLS, err := agentsecurity.EnsureTLSAssets(cfg.DataRoot)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	secretStore, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	taskSvc := tasks.NewService(store.TaskDB())
	certBridge := &applicationCertificateBridge{}
	containerBridge := &applicationContainerBridge{}
	credSvc := credential.NewService(store.AppDB(), secretStore)
	if err := credSvc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	keyAssetSvc := keyassets.NewService(store.AppDB(), cfg, secretStore, taskSvc,
		keyassets.WithApplicationRefresher(certBridge),
	)
	if err := keyAssetSvc.EnsureLegacySelfSignedMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	logging.L().Info("runtime settings loaded", zap.String("log_level", settingsSvc.Runtime().LogLevel))
	authSvc, err := auth.NewService(store.AppDB(), cfg, settingsSvc)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if _, err := taskSvc.FailRunningWithoutExecution(context.Background(), time.Now().UTC()); err != nil {
		_ = store.Close()
		return nil, err
	}
	executor := sshx.NewSSHExecutorWithTimeoutProvider(credSvc, cfg.RemoteTimeout(), settingsSvc.RemoteTimeout)
	agentClient, err := agentclient.NewHTTPClient(agentTLS, cfg.RemoteTimeout())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	serverSvc := server.NewService(store.AppDB(), executor, taskSvc,
		server.WithAgentClient(agentClient),
		server.WithAgentTLSAssets(agentTLS),
		server.WithMetricsDB(store.MetricsDB()),
	)
	applicationSvc := applications.NewServiceWithOptions(store.AppDB(), agentClient, taskSvc, applications.Config{
		SaveSessionDir: applicationSaveSessionDir(cfg),
	},
		applications.WithServerProvider(serverSvc),
		applications.WithBuiltinVariableResolver(certBridge),
		applications.WithPanelFileProvider(certBridge),
		applications.WithContainerOperationQueue(containerBridge),
	)
	certBridge.apps = applicationSvc
	containerBridge.apps = applicationSvc
	containerSvc := containerization.NewService(store.AppDB(), serverSvc, agentClient, taskSvc,
		containerization.WithApplicationUpdater(containerBridge),
	)
	containerBridge.containers = containerSvc
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, executor, metrics.WithAgentClient(agentClient))
	packageSvc := packages.NewService(store.AppDB(), serverSvc, executor, taskSvc, agentClient)
	overviewSvc := overview.NewService(store.AppDB(), serverSvc, metricsSvc, packageSvc)
	if err := dns.MigrateProviderCredentials(context.Background(), store.AppDB(), secretStore); err != nil {
		_ = store.Close()
		return nil, err
	}
	dnsSvc := dns.NewService(store.AppDB(), secretStore)
	certSvc := certs.NewService(store.AppDB(), cfg, dnsSvc, taskSvc,
		certs.WithConfigProvider(settingsSvc.ApplyToConfig),
		certs.WithKeyAssetProvider(keyAssetSvc),
		certs.WithApplicationRefresher(certBridge),
	)
	certBridge.certs = certSvc
	registerTaskDefinitions(taskSvc, settingsSvc, keyAssetSvc, serverSvc, applicationSvc, containerSvc, metricsSvc, packageSvc, certSvc)
	systemSvc := systeminfo.NewService(nil)
	systemSvc.Start(context.Background())
	taskWorker := tasks.NewWorker(taskSvc)
	diagnosticsSvc := diagnostics.NewServiceWithTaskRuntime(
		taskWorker,
		diagnostics.DatabaseSource{Name: "app", DB: store.AppDB(), Path: cfg.AppDatabase},
		diagnostics.DatabaseSource{Name: "task", DB: store.TaskDB(), Path: cfg.TaskDatabase},
		diagnostics.DatabaseSource{Name: "metrics", DB: store.MetricsDB(), Path: cfg.MetricsDatabase},
	)

	metricsCleanup := metrics.NewCleanupWorker(metricsSvc, func() metrics.CleanupSettings {
		runtime := settingsSvc.Runtime()
		return metrics.CleanupSettings{
			RetentionDays: runtime.MetricsRetentionDays,
			Schedule:      runtime.CleanupSchedule,
		}
	})
	a := &App{
		cfg:            cfg,
		store:          store,
		mux:            http.NewServeMux(),
		auth:           authSvc,
		tasks:          taskWorker,
		metricsCleanup: metricsCleanup,
		system:         systemSvc,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		serverSvc.CheckConfiguredAgents(ctx)
	}()
	taskWorker.Start(context.Background())
	metricsCleanup.Start(context.Background())
	logging.L().Info("background services started")
	a.routes(auth.NewHandler(authSvc), credential.NewHandler(credSvc), dns.NewHandler(dnsSvc), certs.NewHandler(certSvc), keyassets.NewHandler(keyAssetSvc), server.NewHandler(serverSvc), tasks.NewHandler(taskSvc, taskWorker), metrics.NewHandler(metricsSvc), packages.NewHandler(packageSvc), applications.NewHandler(applicationSvc), containerization.NewHandler(containerSvc), overview.NewHandler(overviewSvc), settings.NewHandler(settingsSvc), systeminfo.NewHandler(systemSvc), diagnostics.NewHandler(diagnosticsSvc))
	logging.L().Info("application initialized")
	return a, nil
}

func (a *App) Close() error {
	if a.tasks != nil {
		a.tasks.Stop()
	}
	if a.metricsCleanup != nil {
		a.metricsCleanup.Stop()
	}
	if a.system != nil {
		a.system.Close()
	}
	return a.store.Close()
}
func (a *App) Handler() http.Handler { return logging.HTTPMiddleware(a.mux) }

func registerTaskDefinitions(taskSvc *tasks.Service, settingsSvc *settings.Service, keyAssetSvc *keyassets.Service, serverSvc *server.Service, applicationSvc *applications.Service, containerSvc *containerization.Service, metricsSvc *metrics.Service, packageSvc *packages.Service, certSvc *certs.Service) {
	collectionInterval := func() time.Duration {
		return time.Duration(settingsSvc.Runtime().MetricsCollectionIntervalSeconds) * time.Second
	}
	keyAssetSvc.RegisterTasks(taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	applicationSvc.RegisterTasks(taskSvc)
	containerSvc.RegisterTasks(taskSvc, collectionInterval)
	metricsSvc.RegisterTasks(taskSvc, collectionInterval)
	packageSvc.RegisterTasks(taskSvc, collectionInterval)
	certSvc.RegisterTasks(taskSvc)
}

func applicationSaveSessionDir(cfg config.Config) string {
	return filepath.Join(cfg.DataRoot, "tmp", "application-save-sessions")
}

func (a *App) routes(authH *auth.Handler, credH *credential.Handler, dnsH *dns.Handler, certH *certs.Handler, keyAssetH *keyassets.Handler, serverH *server.Handler, taskH *tasks.Handler, metricsH *metrics.Handler, packageH *packages.Handler, applicationH *applications.Handler, containerH *containerization.Handler, overviewH *overview.Handler, settingsH *settings.Handler, systemH *systeminfo.Handler, diagnosticsH *diagnostics.Handler) {
	a.mux.HandleFunc("POST /api/v1/auth/login", authH.Login)
	a.mux.Handle("POST /api/v1/auth/logout", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.Logout)))
	a.mux.Handle("POST /api/v1/auth/account", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.UpdateAccount)))
	a.mux.Handle("POST /api/v1/auth/jwt-secret", a.auth.RequireAuth(http.HandlerFunc(authH.UpdateJWTSecret)))
	a.mux.HandleFunc("GET /api/v1/auth/session", authH.Session)

	authenticated := a.auth.RequireAuth
	settingsH.RegisterPublicRoutes(a.mux)
	credH.RegisterRoutes(a.mux, authenticated)
	dnsH.RegisterRoutes(a.mux, authenticated)
	certH.RegisterRoutes(a.mux, authenticated)
	keyAssetH.RegisterRoutes(a.mux, authenticated)
	serverH.RegisterRoutes(a.mux, authenticated)
	metricsH.RegisterRoutes(a.mux, authenticated)
	packageH.RegisterRoutes(a.mux, authenticated)
	containerH.RegisterRoutes(a.mux, authenticated)
	applicationH.RegisterRoutes(a.mux, authenticated)
	taskH.RegisterRoutes(a.mux, authenticated)
	overviewH.RegisterRoutes(a.mux, authenticated)
	settingsH.RegisterRoutes(a.mux, authenticated)
	systemH.RegisterRoutes(a.mux, authenticated)
	diagnosticsH.RegisterRoutes(a.mux, authenticated)

	a.mux.HandleFunc("/", a.static)
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
