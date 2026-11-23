package panel

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/activity"
	"panel/internal/modules/appearance"
	"panel/internal/modules/applications"
	"panel/internal/modules/autoupdate"
	"panel/internal/modules/backups"
	"panel/internal/modules/certificates/certs"
	"panel/internal/modules/certificates/dns"
	"panel/internal/modules/containers"
	modevents "panel/internal/modules/events"
	"panel/internal/modules/facilityapps"
	"panel/internal/modules/identity"
	"panel/internal/modules/keyassets"
	"panel/internal/modules/networkprobe"
	"panel/internal/modules/observability/diagnostics"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/observability/metricsfacility"
	"panel/internal/modules/observability/overview"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/settings"
	"panel/internal/modules/systeminfo"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/events"
	"panel/internal/platform/logging"
	"panel/internal/platform/maintenance"
	"panel/internal/platform/paneltls"
	"panel/internal/platform/secrets"
	"panel/internal/platform/ssh"

	"go.uber.org/zap"
)

const (
	// Vite fingerprints files emitted below assets/, so they can safely stay
	// in a browser cache for the lifetime of the build.
	staticImmutableCacheControl = "public, max-age=31536000, immutable"
	staticResourceCacheControl  = "public, max-age=86400"
	// The entry document is intentionally revalidated so a new frontend build
	// can point the browser at its new asset filenames immediately.
	staticDocumentCacheControl = "no-cache"
)

type App struct {
	cfg            config.Config
	store          *database.Store
	mux            *http.ServeMux
	auth           *auth.Service
	tasks          *tasks.Worker
	metricsCleanup *metrics.CleanupWorker
	activity       *activity.Service
	eventLogs      *systemEventWriter
	applicationSvc *applications.Service
	settings       *settings.Service
	keyAssets      *keyassets.Service
	system         *systeminfo.Service
	agentReports   *agentReportCollector
	diagnostics    *diagnostics.Service
	checkCancel    context.CancelFunc
	checkDone      chan struct{}
	periodicCancel context.CancelFunc
	runtimeWriters maintenance.Gate
}

func New(cfg config.Config) (*App, error) {
	if err := agentcontract.ValidateGeneratedHash(); err != nil {
		return nil, err
	}
	store, err := database.Open(cfg)
	if err != nil {
		return nil, err
	}
	if err := logging.Configure(cfg.DataRoot); err != nil {
		_ = store.Close()
		return nil, err
	}
	logging.SetContextStore(events.NewStore(store.AppDB()))
	secretStore, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	taskSvc := tasks.NewService(store.AppDB())
	activitySvc := activity.NewService(store.AppDB(), store.LogDB())
	if err := activitySvc.Init(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	activitySvc.Commands = activityCommands(taskSvc, store.AppDB())
	eventWriter := &systemEventWriter{service: activitySvc}
	taskSvc.SetRuntimeEvents(eventWriter)
	certBridge := &applicationCertificateBridge{}
	containerBridge := &applicationContainerBridge{}
	credSvc := credential.NewService(store.AppDB(), secretStore)
	if err := credSvc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	keyAssetSvc := keyassets.NewService(store.AppDB(), cfg, secretStore, taskSvc,
		keyassets.WithLogDB(store.LogDB()),
		keyassets.WithApplicationRefresher(certBridge),
	)
	if err := keyAssetSvc.EnsureLegacySelfSignedMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	agentTLS, err := keyAssetSvc.EnsureAgentTLSAssets(context.Background())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	settingsSvc, err := settings.NewService(store.AppDB(), cfg, settings.WithTLSAssetProvider(keyAssetSvc))
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	// Synchronize the built-in Panel RSA CA/leaf before any listener can accept
	// traffic. User-selected certificates remain selected and are only
	// validated/activated here.
	if err := keyAssetSvc.SyncPanelTLS(context.Background(), settingsSvc.Runtime().Panel.Domain, settingsSvc.Runtime().Panel.TLSCertificateID); err != nil {
		_ = store.Close()
		return nil, err
	}
	initCtx := logging.Background("panel")
	logging.From(initCtx).Info("runtime settings loaded", zap.String("log_level", settingsSvc.Runtime().LogLevel))
	// 外观设置是单行 AppDB 配置，背景图片字节落在 <dataRoot>/appearance/。
	// 启动时幂等 seed 默认行、修复 DB 与文件不一致，并清理崩溃遗留的孤儿目录。
	appearanceSvc := appearance.NewService(store.AppDB(), cfg.DataRoot)
	if err := appearanceSvc.EnsureSeeded(initCtx); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := appearanceSvc.Reconcile(initCtx); err != nil {
		logging.From(initCtx).Warn("appearance background reconcile failed", zap.Error(err))
	}
	if err := appearanceSvc.CleanupOrphans(initCtx, time.Now().UTC()); err != nil {
		logging.From(initCtx).Warn("appearance orphan cleanup failed", zap.Error(err))
	}
	authSvc, err := auth.NewService(store.AppDB(), cfg, settingsSvc)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	applications.RegisterDeployRunDefinition(taskSvc)
	if _, err := taskSvc.FailRunningWithoutExecution(context.Background(), time.Now().UTC()); err != nil {
		_ = store.Close()
		return nil, err
	}
	internalFileRegistry := applications.NewInternalFileRegistry()
	internalFileRegistry.Register("key_asset", keyAssetSvc)
	variableRegistry := applications.NewApplicationVariableRegistry()
	executor := sshx.NewSSHExecutorWithTimeoutProvider(credSvc, cfg.RemoteTimeout(), settingsSvc.RemoteTimeout, sshx.WithKnownHosts(filepath.Join(cfg.DataRoot, "known_hosts")))
	agentClient, err := agentclient.NewGRPCClient(agentTLS, cfg.RemoteTimeout())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	serverSvc := server.NewService(store.AppDB(), executor, taskSvc,
		server.WithAgentClient(agentClient),
		server.WithAgentTLSAssets(agentTLS),
		server.WithAgentTLSProvider(keyAssetSvc),
		server.WithPanelTLSProvider(keyAssetSvc),
		server.WithMetricsDB(store.MetricsDB()),
		server.WithSecretStore(secretStore),
		server.WithAgentDeliverySettings(func() server.AgentDeliverySettings {
			runtime := settingsSvc.Runtime()
			return server.AgentDeliverySettings{
				DownloadBaseURL: runtime.Agent.DownloadBaseURL,
				VerifyTLS:       runtime.Agent.DownloadVerifyTLS,
				TransferTimeout: time.Duration(runtime.Agent.TransferTimeoutSeconds) * time.Second,
			}
		}),
	)
	// 服务器代理链（SSH 隧道 / SOCKS5）同时作用于 SSH 执行器与 agent gRPC
	// 连接；解析器依赖 servers 服务本身，因此必须在构造完成后注入。
	executor.SetProxyResolver(serverSvc)
	agentClient.SetDialFunc(serverSvc.DialContext)
	// Panel 侧的镜像 digest 解析器：读取设置页「镜像仓库源」的 endpoint/scheme，
	// 支持受限网络下经镜像/私有仓库解析；provider 每次解析读取热更新设置。
	imageResolver := applications.NewRegistryImageResolverWithConfig(func() applications.ImageRegistryConfig {
		registry := settingsSvc.Runtime().ImageRegistry
		return applications.ImageRegistryConfig{Endpoint: registry.Endpoint, Scheme: registry.Scheme}
	})
	applicationSvc := applications.NewServiceWithOptions(store.AppDB(), agentClient, taskSvc, applications.Config{
		SaveSessionDir: applicationSaveSessionDir(cfg),
		DataRoot:       cfg.DataRoot,
	},
		applications.WithServerProvider(serverSvc),
		applications.WithBuiltinVariableResolver(variableRegistry),
		applications.WithInternalFileProvider(internalFileRegistry),
		applications.WithContainerOperationQueue(containerBridge),
		applications.WithRuntimeEvents(eventWriter),
		applications.WithImageDigestResolver(imageResolver),
	)
	if err := applicationSvc.BackfillApplicationFileBlobs(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	certBridge.apps = applicationSvc
	containerBridge.apps = applicationSvc
	containerSvc := containerization.NewService(store.AppDB(), serverSvc, agentClient, taskSvc,
		containerization.WithApplicationUpdater(containerBridge),
		containerization.WithImageDigestResolver(imageResolver),
	)
	containerBridge.containers = containerSvc
	applicationSvc.SetApplicationReconcileTrigger(containerSvc)
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc)
	probeSvc := networkprobe.NewService(store.AppDB(), serverSvc)
	// 指标设施保存采集范围/保留天数/采集间隔/清理计划。首次启动时从旧 runtime
	// settings 播种；失败会让后续采集节奏与清理参数不可靠，因此启动失败。
	metricsFacility := metricsfacility.NewService(store.AppDB(), serverSvc)
	if err := metricsFacility.EnsureMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	packageSvc := packages.NewService(store.AppDB(), serverSvc, executor, taskSvc, agentClient)
	overviewSvc := overview.NewService(store.AppDB(), serverSvc, metricsSvc, packageSvc, overview.WithNetworkProbe(probeSvc))
	if err := dns.MigrateProviderCredentials(context.Background(), store.AppDB(), secretStore); err != nil {
		_ = store.Close()
		return nil, err
	}
	dnsSvc := dns.NewService(store.AppDB(), secretStore, taskSvc)
	certSvc := certs.NewService(store.AppDB(), cfg, dnsSvc, taskSvc,
		certs.WithConfigProvider(settingsSvc.ApplyToConfig),
		certs.WithKeyAssetProvider(keyAssetSvc),
		certs.WithApplicationRefresher(certBridge),
	)
	if err := certSvc.EnsureLegacyACMEAssetsMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	facilitySvc := facilityapps.NewService(store.AppDB(), agentClient, serverSvc, applicationSvc,
		facilityapps.WithContainerOperationQueue(containerSvc),
		facilityapps.WithDataRoot(cfg.DataRoot),
		facilityapps.WithCertificateProvider(certSvc),
		facilityapps.WithApplicationReconcileTrigger(containerSvc),
		facilityapps.WithDNSProvider(dnsSvc),
		facilityapps.WithTaskService(taskSvc),
		facilityapps.WithSSHExecutor(executor),
		facilityapps.WithImageResolver(imageResolver),
	)
	serverSvc.SetDNSSyncTrigger(facilitySvc.SyncServersDNSEntries)
	// 节点删除后入口代理与存储共享必须按修剪后的配置重新渲染。
	serverSvc.SetServerReferenceReconciler(facilitySvc)
	// 自动更新协调器：设置页「自动更新」驱动；各能力均通过本包接口注入。
	autoUpdateSvc := autoupdate.NewService(taskSvc, settingsSvc,
		autoupdate.WithServerLister(serverSvc),
		autoupdate.WithApplicationLister(applicationSvc),
		autoupdate.WithApplicationUpgrader(containerSvc),
		autoupdate.WithApplicationImageRefresher(applicationSvc),
		autoupdate.WithImageRefresher(containerSvc),
		autoupdate.WithPackageMaintainer(packageSvc),
		autoupdate.WithReverseProxyImageUpdater(facilitySvc),
	)
	autoUpdateSvc.RegisterTasks(taskSvc)
	// 设置页“立即运行一次”通过 settings 服务转发到 autoupdate，避免 settings
	// 反向依赖 autoupdate 包。
	settingsSvc.SetAutoUpdateRunner(autoUpdateSvc)
	containerBridge.facility = facilitySvc
	applicationSvc.SetReverseProxyReconciler(facilitySvc)
	applicationSvc.SetReverseProxyPolicyProvider(facilitySvc)
	applicationSvc.SetDNSSyncTrigger(facilitySvc.SyncApplicationProxyDNSEntries)
	applicationSvc.SetFacilityRuntimeProvider(facilitySvc)
	applicationSvc.SetStorageShareResolver(facilitySvc)
	internalFileRegistry.Register("certificate", certSvc)
	variableRegistry.Register("certs", certSvc)
	registerTaskDefinitions(taskSvc, settingsSvc, keyAssetSvc, serverSvc, applicationSvc, containerSvc, packageSvc, certSvc)
	systemSvc := systeminfo.NewService(nil)
	systemSvc.Start(context.Background())
	taskWorker := tasks.NewWorker(taskSvc)
	diagnosticsSvc := diagnostics.NewServiceWithTaskRuntime(
		taskWorker,
		diagnostics.DatabaseSource{Name: "app", DB: store.AppDB(), Path: cfg.AppDatabase},
		diagnostics.DatabaseSource{Name: "log", DB: store.LogDB(), Path: cfg.LogDatabase},
		diagnostics.DatabaseSource{Name: "coord", DB: store.CoordDB(), Path: cfg.CoordinationDatabase},
		diagnostics.DatabaseSource{Name: "metrics", DB: store.MetricsDB(), Path: cfg.MetricsDatabase},
	)

	metricsCleanup := metrics.NewCleanupWorker(metricsSvc, func() metrics.CleanupSettings {
		// 保留天数与清理计划由指标设施配置驱动（读失败时设施内部回退默认值）。
		settings := metricsFacility.CleanupSettings()
		return metrics.CleanupSettings{
			RetentionDays: settings.RetentionDays,
			Schedule:      settings.Schedule,
		}
	})

	backupSvc := backups.NewService(backups.ArchiveConfig{
		DataRoot:             cfg.DataRoot,
		AppDatabase:          cfg.AppDatabase,
		LogDatabase:          cfg.LogDatabase,
		CoordinationDatabase: cfg.CoordinationDatabase,
		MetricsDatabase:      cfg.MetricsDatabase,
		PanelVersion:         systemSvc.Version().Version,
	})
	reportCollector := newAgentReportCollector(serverSvc, agentClient, settingsSvc, metricsSvc, containerSvc, packageSvc)
	reportCollector.SetSystemLogs(eventWriter)
	reportCollector.SetNetworkProbe(probeSvc)
	// 指标采集范围与间隔由设施按服务器下发；未注入时采集链路回退 enabled/60s。
	reportCollector.SetMetricsFacility(metricsFacility)
	a := &App{
		cfg:            cfg,
		store:          store,
		mux:            http.NewServeMux(),
		auth:           authSvc,
		tasks:          taskWorker,
		metricsCleanup: metricsCleanup,
		activity:       activitySvc,
		eventLogs:      eventWriter,
		system:         systemSvc,
		agentReports:   reportCollector,
		diagnostics:    diagnosticsSvc,
		applicationSvc: applicationSvc,
		settings:       settingsSvc,
		keyAssets:      keyAssetSvc,
	}
	diagnosticsSvc.SetClearRuntimeDataHook(a.clearRuntimeData)
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 30*time.Second)
	checkDone := make(chan struct{})
	a.checkCancel = checkCancel
	a.checkDone = checkDone
	go func() {
		defer close(checkDone)
		serverSvc.CheckConfiguredAgents(checkCtx)
	}()
	// RunEngine: unified worker owns deploy due/wake; claim/Execute goes
	// Manager.Run → application_deploy Execute → Controller.RunJob (ENG-RUN-001).
	applicationSvc.BindRunEngineWorker(taskWorker)
	if err := applicationSvc.StartOrchestrator(context.Background()); err != nil {
		checkCancel()
		<-checkDone
		a.stopBackgroundServices()
		_ = a.store.Close()
		return nil, err
	}
	taskWorker.Start(context.Background())
	periodicCtx, periodicCancel := context.WithCancel(context.Background())
	a.periodicCancel = periodicCancel
	serverSvc.StartPeriodic(periodicCtx, taskSvc)
	containerSvc.StartPeriodic(periodicCtx, taskSvc)
	packageSvc.StartPeriodic(periodicCtx, taskSvc)
	certSvc.StartPeriodic(periodicCtx, taskSvc)
	keyAssetSvc.StartPeriodic(periodicCtx, taskSvc)
	facilitySvc.StartPeriodic(periodicCtx, taskSvc)
	autoUpdateSvc.StartPeriodic(periodicCtx)
	appearanceSvc.StartPeriodic(periodicCtx)
	metricsCleanup.Start(context.Background())
	eventWriter.Start(context.Background())
	reportCollector.Start(context.Background())
	logging.From(initCtx).Info("background services started")
	taskHandler := tasks.NewHandler(taskSvc, taskWorker)
	taskHandler.SetExternalResolver(applicationSvc.ResolveExecutionManually)
	eventsSvc := modevents.NewService(store.AppDB(), filepath.Join(cfg.DataRoot, "logs"))
	a.routes(auth.NewHandler(authSvc), credential.NewHandler(credSvc), dns.NewHandler(dnsSvc), certs.NewHandler(certSvc), keyassets.NewHandler(keyAssetSvc), server.NewHandler(serverSvc), taskHandler, metrics.NewHandler(metricsSvc), packages.NewHandler(packageSvc), modevents.NewHandler(eventsSvc), applications.NewHandler(applicationSvc), containerization.NewHandler(containerSvc), facilityapps.NewHandler(facilitySvc), networkprobe.NewHandler(probeSvc), metricsfacility.NewHandler(metricsFacility), overview.NewHandler(overviewSvc), settings.NewHandler(settingsSvc), appearance.NewHandler(appearanceSvc), systeminfo.NewHandler(systemSvc), diagnostics.NewHandler(diagnosticsSvc), backups.NewHandler(backupSvc))
	logging.From(initCtx).Info("application initialized")
	return a, nil
}

func (a *App) Close() error {
	if a.checkCancel != nil {
		a.checkCancel()
		a.checkCancel = nil
	}
	if a.checkDone != nil {
		<-a.checkDone
		a.checkDone = nil
	}
	a.stopBackgroundServices()
	return a.store.Close()
}

// stopBackgroundServices stops every started background worker in dependency
// order. Every Stop/Close is nil-safe and idempotent, so it can be reused for
// both startup failure cleanup and normal shutdown.
func (a *App) stopBackgroundServices() {
	if a.periodicCancel != nil {
		a.periodicCancel()
		a.periodicCancel = nil
	}
	if a.applicationSvc != nil {
		_ = a.applicationSvc.StopOrchestrator()
	}
	if a.tasks != nil {
		a.tasks.Stop()
	}
	if a.metricsCleanup != nil {
		a.metricsCleanup.Stop()
	}
	if a.eventLogs != nil {
		a.eventLogs.Stop()
	}
	if a.system != nil {
		a.system.Close()
	}
	if a.agentReports != nil {
		a.agentReports.Stop()
	}
	if a.diagnostics != nil {
		_ = a.diagnostics.Close()
	}
}
func (a *App) Handler() http.Handler {
	return logging.HTTPMiddleware(a.runtimeMaintenanceMiddleware(a.mux))
}

func (a *App) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		certificate, err := paneltls.FixedCertificate(a.cfg.DataRoot, a.settings.Runtime().Panel.Domain)
		if err != nil {
			return nil, err
		}
		return &certificate, nil
	}}
}

func registerTaskDefinitions(taskSvc *tasks.Service, settingsSvc *settings.Service, keyAssetSvc *keyassets.Service, serverSvc *server.Service, applicationSvc *applications.Service, containerSvc *containerization.Service, packageSvc *packages.Service, certSvc *certs.Service) {
	keyAssetSvc.RegisterTasks(taskSvc)
	serverSvc.RegisterTasks(taskSvc)
	applicationSvc.RegisterTasks(taskSvc)
	containerSvc.RegisterTasks(taskSvc)
	packageSvc.RegisterTasks(taskSvc)
	certSvc.RegisterTasks(taskSvc)
}

func applicationSaveSessionDir(cfg config.Config) string {
	return filepath.Join(cfg.DataRoot, "tmp", "application-save-sessions")
}

func (a *App) routes(authH *auth.Handler, credH *credential.Handler, dnsH *dns.Handler, certH *certs.Handler, keyAssetH *keyassets.Handler, serverH *server.Handler, taskH *tasks.Handler, metricsH *metrics.Handler, packageH *packages.Handler, eventsH *modevents.Handler, applicationH *applications.Handler, containerH *containerization.Handler, facilityH *facilityapps.Handler, probeH *networkprobe.Handler, metricsFacilityH *metricsfacility.Handler, overviewH *overview.Handler, settingsH *settings.Handler, appearanceH *appearance.Handler, systemH *systeminfo.Handler, diagnosticsH *diagnostics.Handler, backupH *backups.Handler) {
	a.mux.HandleFunc("GET /api/v1/setup/status", authH.SetupStatus)
	a.mux.HandleFunc("POST /api/v1/setup", authH.Setup)
	a.mux.HandleFunc("POST /api/v1/auth/login", authH.Login)
	a.mux.Handle("POST /api/v1/auth/logout", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.Logout)))
	a.mux.Handle("POST /api/v1/auth/account", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.UpdateAccount)))
	a.mux.Handle("POST /api/v1/auth/jwt-secret", a.auth.RequireAuth(a.auth.RequirePermission("settings.security", http.HandlerFunc(authH.UpdateJWTSecret))))
	a.mux.HandleFunc("GET /api/v1/auth/session", authH.Session)

	authenticated := a.activityAuth
	authH.RegisterRoutes(a.mux, authenticated)
	settingsH.RegisterPublicRoutes(a.mux)
	appearanceH.RegisterPublicRoutes(a.mux)
	backupH.RegisterRoutes(a.mux, authenticated)
	credH.RegisterRoutes(a.mux, authenticated)
	dnsH.RegisterRoutes(a.mux, authenticated)
	certH.RegisterRoutes(a.mux, authenticated)
	keyAssetH.RegisterRoutes(a.mux, authenticated)
	serverH.RegisterRoutes(a.mux, authenticated)
	// The agent bundle download is intentionally unauthenticated: targets have
	// no Panel session, and a per-server token would make every server use a
	// different URL, which would defeat CDN reuse.
	serverH.RegisterPublicRoutes(a.mux)
	metricsH.RegisterRoutes(a.mux, authenticated)
	// Product /api/v1/activity/* routes are no longer registered; process log
	// streams are served by /api/v1/events and /api/v1/logs. Activity Service remains for
	// append/audit middleware and internal projections.
	eventsH.RegisterRoutes(a.mux, authenticated)
	packageH.RegisterRoutes(a.mux, authenticated)
	containerH.RegisterRoutes(a.mux, authenticated)
	facilityH.RegisterRoutes(a.mux, authenticated)
	probeH.RegisterRoutes(a.mux, authenticated)
	metricsFacilityH.RegisterRoutes(a.mux, authenticated)
	applicationH.RegisterRoutes(a.mux, authenticated)
	taskH.RegisterRoutes(a.mux, authenticated)
	overviewH.RegisterRoutes(a.mux, authenticated)
	settingsH.RegisterRoutes(a.mux, authenticated)
	appearanceH.RegisterRoutes(a.mux, authenticated)
	systemH.RegisterRoutes(a.mux, authenticated)
	diagnosticsH.RegisterRoutes(a.mux, authenticated)

	a.mux.HandleFunc("/", a.static)
}
func (a *App) static(w http.ResponseWriter, r *http.Request) {
	dist := filepath.Join("web", "dist")
	if _, err := os.Stat(dist); err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Panel backend is running. Build the frontend into web/dist to serve the UI.\n"))
		return
	}
	rel := strings.TrimPrefix(filepath.Clean(r.URL.Path), string(filepath.Separator))
	path := filepath.Join(dist, rel)
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		w.Header().Set("Cache-Control", staticCacheControl(rel))
		http.ServeFile(w, r, path)
		return
	}
	w.Header().Set("Cache-Control", staticDocumentCacheControl)
	http.ServeFile(w, r, filepath.Join(dist, "index.html"))
}

func staticCacheControl(rel string) string {
	rel = filepath.ToSlash(filepath.Clean(rel))
	rel = strings.TrimPrefix(rel, "/")
	if strings.EqualFold(filepath.Ext(rel), ".html") {
		return staticDocumentCacheControl
	}
	if strings.HasPrefix(rel, "assets/") {
		return staticImmutableCacheControl
	}
	return staticResourceCacheControl
}
