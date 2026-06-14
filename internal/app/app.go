package app

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/applications"
	"panel/internal/auth"
	"panel/internal/certs"
	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/dns"
	"panel/internal/httpx"
	"panel/internal/keyassets"
	"panel/internal/logging"
	"panel/internal/metrics"
	"panel/internal/nomad"
	"panel/internal/overview"
	"panel/internal/packages"
	"panel/internal/panelerr"
	"panel/internal/scheduler"
	"panel/internal/secretstore"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/systeminfo"
	"panel/internal/tasks"

	"go.uber.org/zap"
)

type App struct {
	cfg    config.Config
	store  *storage.Store
	mux    *http.ServeMux
	auth   *auth.Service
	sched  *scheduler.Scheduler
	system *systeminfo.Service
}

func New(cfg config.Config) (*App, error) {
	store, err := storage.Open(cfg)
	if err != nil {
		return nil, err
	}
	nomadTLS, err := nomad.EnsureTLSAssets(cfg.DataRoot)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	secretStore, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	taskSvc := tasks.NewService(store.AppDB())
	credSvc := credential.NewService(store.AppDB(), secretStore)
	if err := credSvc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	keyAssetSvc := keyassets.NewService(store.AppDB(), cfg, secretStore, taskSvc)
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
	serverSvc := server.NewService(store.AppDB(), executor, taskSvc)
	serverSvc.SetMetricsDB(store.MetricsDB())
	runtimeNomad := settingsSvc.NomadConfig(cfg.Nomad)
	nomadClientCfg := nomad.Config{
		Address:    runtimeNomad.Address,
		Token:      runtimeNomad.Token,
		Namespace:  runtimeNomad.Namespace,
		Region:     runtimeNomad.Region,
		Datacenter: runtimeNomad.Datacenter,
	}
	if usesManagedNomadTLS(cfg.Nomad.Address) {
		nomadClientCfg.TLS = &nomad.TLSConfig{
			CAFile:             nomadTLS.CAPath,
			CertFile:           nomadTLS.ClientCertPath,
			KeyFile:            nomadTLS.ClientKeyPath,
			SkipVerifyHostname: true,
		}
	}
	nomadClient := nomad.NewClient(nomadClientCfg, nil)
	nomadClient.SetConfigProvider(func(base nomad.Config) nomad.Config {
		runtime := settingsSvc.NomadConfig(config.NomadConfig{
			Address:    base.Address,
			Token:      base.Token,
			Namespace:  base.Namespace,
			Region:     base.Region,
			Datacenter: base.Datacenter,
		})
		base.Namespace = runtime.Namespace
		base.Region = runtime.Region
		base.Datacenter = runtime.Datacenter
		return base
	})
	appNomad := settingsSvc.ApplicationNomadConfig()
	applicationSvc := applications.NewService(store.AppDB(), nomadClient, taskSvc, applications.Config{
		Namespace:  appNomad.Namespace,
		Region:     appNomad.Region,
		Datacenter: appNomad.Datacenter,
	})
	applicationSvc.SetConfigProvider(func() applications.Config {
		runtime := settingsSvc.ApplicationNomadConfig()
		return applications.Config{Namespace: runtime.Namespace, Region: runtime.Region, Datacenter: runtime.Datacenter}
	})
	nomadJoinSvc := nomad.NewJoinService(serverSvc, nomadClient, executor, taskSvc, runtimeNomad, nomadTLS)
	nomadJoinSvc.SetDataRoot(cfg.DataRoot)
	nomadJoinSvc.SetConfigProvider(settingsSvc.NomadConfig)
	metricsSvc := metrics.NewService(store.MetricsDB(), serverSvc, executor)
	packageSvc := packages.NewService(store.AppDB(), serverSvc, executor, taskSvc)
	overviewSvc := overview.NewService(store.AppDB(), serverSvc, metricsSvc, packageSvc)
	if err := dns.MigrateProviderCredentials(context.Background(), store.AppDB(), secretStore); err != nil {
		_ = store.Close()
		return nil, err
	}
	dnsSvc := dns.NewService(store.AppDB(), secretStore)
	certSvc := certs.NewService(store.AppDB(), cfg, dnsSvc, taskSvc)
	certSvc.SetNomadTLSAssets(nomadTLS)
	certSvc.SetConfigProvider(settingsSvc.ApplyToConfig)
	certSvc.SetKeyAssetProvider(keyAssetSvc)
	systemSvc := systeminfo.NewService(nil)
	systemSvc.Start(context.Background())

	a := &App{cfg: cfg, store: store, mux: http.NewServeMux(), auth: authSvc, system: systemSvc}
	applicationSvc.SetBuiltinVariableResolver(certSvc)
	applicationSvc.SetPanelFileProvider(certSvc)
	applicationSvc.SetReverseProxyReconciler(nomadJoinSvc)
	keyAssetSvc.SetApplicationRefresher(applicationSvc)
	nomadJoinSvc.SetApplicationProxySource(applicationSvc)
	nomadJoinSvc.SetEnabledApplicationRestorer(applicationSvc)
	nomadJoinSvc.SetReverseProxyCertificateSource(certSvc)
	certSvc.SetApplicationRefresher(applicationSvc)
	sched := scheduler.New(settingsSvc, serverSvc, metricsSvc, packageSvc, taskSvc)
	sched.SetCertificateRenewer(certSvc)
	a.sched = sched
	nomadJoinSvc.RestoreNomadAddressFromBootstrap(context.Background())
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := nomadJoinSvc.ReconcileReverseProxy(ctx); err != nil {
			logging.L().Warn("reverse proxy reconciliation failed", zap.Error(err))
		}
	}()
	sched.Start(context.Background())
	logging.L().Info("background services started")
	a.routes(auth.NewHandler(authSvc), credential.NewHandler(credSvc), dns.NewHandler(dnsSvc), certs.NewHandler(certSvc), keyassets.NewHandler(keyAssetSvc), server.NewHandler(serverSvc), tasks.NewHandler(taskSvc, sched), metrics.NewHandler(metricsSvc), packages.NewHandler(packageSvc), applications.NewHandler(applicationSvc), nomad.NewHandler(nomadClient, nomadJoinSvc), overview.NewHandler(overviewSvc), settings.NewHandler(settingsSvc), systeminfo.NewHandler(systemSvc))
	logging.L().Info("application initialized")
	return a, nil
}

func (a *App) Close() error {
	if a.sched != nil {
		a.sched.Stop()
	}
	if a.system != nil {
		a.system.Close()
	}
	return a.store.Close()
}
func (a *App) Handler() http.Handler { return logging.HTTPMiddleware(a.mux) }

func (a *App) routes(authH *auth.Handler, credH *credential.Handler, dnsH *dns.Handler, certH *certs.Handler, keyAssetH *keyassets.Handler, serverH *server.Handler, taskH *tasks.Handler, metricsH *metrics.Handler, packageH *packages.Handler, applicationH *applications.Handler, nomadH *nomad.Handler, overviewH *overview.Handler, settingsH *settings.Handler, systemH *systeminfo.Handler) {
	a.mux.HandleFunc("POST /api/v1/auth/login", authH.Login)
	a.mux.Handle("POST /api/v1/auth/logout", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.Logout)))
	a.mux.Handle("POST /api/v1/auth/account", a.auth.RequireAuthAllowPasswordChange(http.HandlerFunc(authH.UpdateAccount)))
	a.mux.Handle("POST /api/v1/auth/jwt-secret", a.auth.RequireAuth(http.HandlerFunc(authH.UpdateJWTSecret)))
	a.mux.HandleFunc("GET /api/v1/auth/session", authH.Session)
	a.mux.HandleFunc("GET /api/v1/settings/public-branding", settingsH.PublicBranding)

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
		case r.Method == http.MethodGet && path == "/api/v1/dns/domains":
			dnsH.ListDomains(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/dns/domains":
			dnsH.CreateDomain(w, r)
		case r.Method == http.MethodGet && dnsRecordCollectionPath(path):
			dnsH.ListRecords(w, r)
		case r.Method == http.MethodPost && dnsRecordCollectionPath(path):
			dnsH.CreateRecord(w, r)
		case r.Method == http.MethodPut && dnsRecordResourcePath(path):
			dnsH.UpdateRecord(w, r)
		case r.Method == http.MethodDelete && dnsRecordResourcePath(path):
			dnsH.DeleteRecord(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/dns/domains/"):
			dnsH.UpdateDomain(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/dns/domains/"):
			dnsH.DeleteDomain(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/certificates":
			certH.List(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/certificates":
			certH.Issue(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/certificates/") && strings.HasSuffix(path, "/renew"):
			certH.Renew(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/certificates/"):
			certH.Delete(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/self-signed-certificates":
			certH.ListSelfSigned(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/self-signed-cas":
			certH.CreateSelfSignedCA(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/self-signed-certificates":
			certH.CreateSelfSignedLeaf(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/self-signed-certificates/") && strings.HasSuffix(path, "/renew"):
			certH.RenewSelfSignedLeaf(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/self-signed-certificates/"):
			certH.DeleteSelfSigned(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/key-assets":
			keyAssetH.List(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/ca":
			keyAssetH.CreateCA(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/tls":
			keyAssetH.CreateTLS(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/ssh/generate":
			keyAssetH.GenerateSSH(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/import":
			keyAssetH.Import(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/exports":
			keyAssetH.CreateExport(w, r)
		case r.Method == http.MethodGet && keyAssetExportDownloadPath(path):
			keyAssetH.DownloadExport(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/key-assets/imports/preflight":
			keyAssetH.PreflightImport(w, r)
		case r.Method == http.MethodPost && keyAssetImportExecutePath(path):
			keyAssetH.ExecuteImport(w, r)
		case r.Method == http.MethodGet && keyAssetFilePath(path):
			keyAssetH.DownloadFile(w, r)
		case r.Method == http.MethodPost && keyAssetActionPath(path, "reissue"):
			keyAssetH.Reissue(w, r)
		case r.Method == http.MethodPost && keyAssetActionPath(path, "regenerate"):
			keyAssetH.Regenerate(w, r)
		case r.Method == http.MethodGet && keyAssetResourcePath(path):
			keyAssetH.Get(w, r)
		case r.Method == http.MethodDelete && keyAssetResourcePath(path):
			keyAssetH.Delete(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/servers":
			serverH.List(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/servers/probe":
			serverH.Probe(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/servers":
			serverH.Create(w, r)
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/api/v1/servers/") && serverResourcePath(path):
			serverH.Update(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/servers/") && serverResourcePath(path):
			serverH.Delete(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/test"):
			serverH.Test(w, r)
		case r.Method == http.MethodPost && serverActionPath(path, "restart"):
			serverH.Restart(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/ufw/install"):
			serverH.InstallUFW(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/ufw"):
			serverH.UFWState(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/ufw/rules"):
			serverH.AllowUFW(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/servers/") && strings.HasSuffix(path, "/ufw/enable"):
			serverH.EnableUFW(w, r)
		case r.Method == http.MethodDelete && serverUFWRulePath(path):
			serverH.DeleteUFWRule(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/overview":
			overviewH.Get(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/overview/cards":
			overviewH.GetCards(w, r)
		case r.Method == http.MethodPut && path == "/api/v1/overview/cards":
			overviewH.UpdateCards(w, r)
		case r.Method == http.MethodGet && overviewCardDataPath(path):
			overviewH.GetCardData(w, r)
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
		case r.Method == http.MethodPost && path == "/api/v1/application-save-sessions":
			applicationH.BeginSaveSession(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/application-save-sessions/") && strings.HasSuffix(path, "/files") && applicationSaveSessionFilesPath(path):
			applicationH.UploadSaveSessionFile(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/application-save-sessions/") && strings.HasSuffix(path, "/files/delete") && applicationSaveSessionFileDeletePath(path):
			applicationH.DeleteSaveSessionFile(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/application-save-sessions/") && strings.HasSuffix(path, "/commit") && applicationSaveSessionCommitPath(path):
			applicationH.CommitSaveSession(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/applications":
			applicationH.List(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/application-template-catalog":
			applicationH.TemplateCatalog(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/applications":
			applicationH.Create(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/files") && applicationFilesPath(path):
			applicationH.ListFiles(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/files") && applicationFilesPath(path):
			applicationH.SaveFile(w, r)
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api/v1/applications/") && applicationFileResourcePath(path):
			applicationH.DeleteFile(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/applications/") && applicationPackagePath(path):
			applicationH.Package(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/validate"):
			applicationH.Validate(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/plan"):
			applicationH.Plan(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/image/check"):
			applicationH.CheckImageUpdate(w, r)
		case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v1/applications/") && strings.HasSuffix(path, "/image/update"):
			applicationH.UpdateImage(w, r)
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
		case r.Method == http.MethodGet && path == "/api/v1/nomad/status":
			nomadH.Status(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/certificates/builtin":
			nomadH.BuiltinCertificates(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/certificates/builtin/rotate":
			nomadH.RotateTLS(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/nomad/nodes":
			nomadH.Nodes(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/nomad/control-plane":
			nomadH.ControlPlane(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/nomad/join-candidates":
			nomadH.JoinCandidates(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/join":
			nomadH.JoinClient(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/bootstrap-server":
			nomadH.BootstrapServer(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/redeploy-node":
			nomadH.RedeployNode(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/rebuild-cluster":
			nomadH.RebuildCluster(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/switch-server":
			nomadH.SwitchServer(w, r)
		case r.Method == http.MethodPost && path == "/api/v1/nomad/remove-node":
			nomadH.RemoveNode(w, r)
		case r.Method == http.MethodPut && path == "/api/v1/nomad/reverse-proxy":
			nomadH.UpdateReverseProxy(w, r)
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
		case r.Method == http.MethodGet && path == "/api/v1/settings/server-variables":
			settingsH.ServerVariableDefinitions(w, r)
		case r.Method == http.MethodPut && path == "/api/v1/settings/server-variables":
			settingsH.UpdateServerVariableDefinitions(w, r)
		case r.Method == http.MethodGet && path == "/api/v1/system/version":
			systemH.Version(w, r)
		default:
			httpx.Error(w, panelerr.NotFound("route"))
		}
	})
	a.mux.Handle("/api/v1/", a.auth.RequireAuth(api))
	a.mux.HandleFunc("/", a.static)
}

func routeParts(path string) []string {
	return strings.Split(strings.Trim(path, "/"), "/")
}

func serverResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[3] != ""
}

func overviewCardDataPath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "overview" && parts[3] == "cards" && parts[4] != "" && parts[5] == "data"
}

func dnsRecordCollectionPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "dns" && parts[3] == "domains" && parts[4] != "" && parts[5] == "records"
}

func dnsRecordResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "dns" && parts[3] == "domains" && parts[4] != "" && parts[5] == "records" && parts[6] != ""
}

func serverActionPath(path string, action string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[3] != "" && parts[4] == action
}

func serverUFWRulePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 7 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "servers" && parts[3] != "" && parts[4] == "ufw" && parts[5] == "rules" && parts[6] != ""
}

func applicationResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "applications" && parts[3] != ""
}

func applicationFilesPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "applications" && parts[3] != "" && parts[4] == "files"
}

func applicationPackagePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "applications" && parts[3] != "" && parts[4] == "package"
}

func applicationFileResourcePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "applications" && parts[3] != "" && parts[4] == "files" && parts[5] != ""
}

func applicationSaveSessionFilesPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "application-save-sessions" && parts[3] != "" && parts[4] == "files"
}

func applicationSaveSessionFileDeletePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "application-save-sessions" && parts[3] != "" && parts[4] == "files" && parts[5] == "delete"
}

func applicationSaveSessionCommitPath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "application-save-sessions" && parts[3] != "" && parts[4] == "commit"
}

func keyAssetResourcePath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "key-assets" && parts[3] != ""
}

func keyAssetActionPath(path, action string) bool {
	parts := routeParts(path)
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "key-assets" && parts[3] != "" && parts[4] == action
}

func keyAssetFilePath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "key-assets" && parts[3] != "" && parts[4] == "files" && parts[5] != ""
}

func keyAssetExportDownloadPath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "key-assets" && parts[3] == "exports" && parts[4] != "" && parts[5] == "download"
}

func keyAssetImportExecutePath(path string) bool {
	parts := routeParts(path)
	return len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "key-assets" && parts[3] == "imports" && parts[4] != "" && parts[5] == "execute"
}

func usesManagedNomadTLS(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" || host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
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
