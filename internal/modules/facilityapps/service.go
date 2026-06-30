package facilityapps

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const reverseProxyEnabledTrait = "agent.reverse_proxy.enabled"

const proxyBridgeLocalHost = "host.docker.internal"

type AgentRuntimeClient interface {
	RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error
	RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error)
	RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error)
	DockerImagePull(ctx context.Context, baseURL, reference string) error
	DockerContainerAction(ctx context.Context, baseURL, id, action string) error
}

type ApplicationProvider interface {
	ApplicationReverseProxyConfigs(ctx context.Context) ([]applications.ApplicationReverseProxyConfig, error)
}

type ServerProvider interface {
	List(ctx context.Context) ([]server.Server, error)
	Get(ctx context.Context, id string) (server.Server, error)
}

type AgentErrorHandler interface {
	HandleAgentError(ctx context.Context, srv server.Server, cause error) bool
}

type ContainerOperationQueue interface {
	Execute(ctx context.Context, serverID string, run func(context.Context) error) error
}

type ApplicationReconcileTrigger interface {
	TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error)
}

type CertificateProvider interface {
	ReverseProxyCertificates(ctx context.Context) ([]proxycert.Certificate, error)
}

type Service struct {
	db           *sql.DB
	dataRoot     string
	agent        AgentRuntimeClient
	servers      ServerProvider
	apps         ApplicationProvider
	certificates CertificateProvider
	agentErrors  AgentErrorHandler
	queue        ContainerOperationQueue
	reconciler   ApplicationReconcileTrigger
}

type Option func(*Service)

func WithContainerOperationQueue(queue ContainerOperationQueue) Option {
	return func(s *Service) { s.queue = queue }
}

func WithDataRoot(dataRoot string) Option {
	return func(s *Service) { s.dataRoot = dataRoot }
}

func WithCertificateProvider(provider CertificateProvider) Option {
	return func(s *Service) { s.certificates = provider }
}

func WithApplicationReconcileTrigger(trigger ApplicationReconcileTrigger) Option {
	return func(s *Service) { s.reconciler = trigger }
}

func NewService(db *sql.DB, agent AgentRuntimeClient, servers ServerProvider, apps ApplicationProvider, opts ...Option) *Service {
	s := &Service{db: db, agent: agent, servers: servers, apps: apps}
	if handler, ok := servers.(AgentErrorHandler); ok {
		s.agentErrors = handler
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) GetReverseProxy(ctx context.Context) (ReverseProxyConfig, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	cfg.Routes = len(cfg.StaticSites) + s.routeCount(ctx, cfg.DeploymentServers)
	if cfg.PanelEntry.Enabled {
		cfg.Routes++
	}
	cfg.EnabledServers = append([]string(nil), cfg.DeploymentServers...)
	assets, err := s.ListStaticAssets(ctx)
	if err == nil {
		cfg.StaticAssets = assets
	}
	if summaries, err := s.routeSummaries(ctx, cfg); err == nil {
		cfg.RouteSummaries = summaries
	}
	if operation, err := s.latestLifecycleOperation(ctx); err == nil && operation.ID != "" {
		cfg.Operation = &operation
	}
	return cfg, nil
}

func (s *Service) SaveReverseProxy(ctx context.Context, in ReverseProxySaveInput) (ReverseProxyConfig, error) {
	previous, err := s.loadConfig(ctx)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	next, err := normalizeInput(in)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	if err := s.saveConfig(ctx, next); err != nil {
		return ReverseProxyConfig{}, err
	}
	if err := s.syncReverseProxyTraits(ctx, previous.DeploymentServers, next.DeploymentServers); err != nil {
		return ReverseProxyConfig{}, err
	}
	if err := s.triggerReverseProxyReconcile(ctx, "facility_app", removedServers(previous.DeploymentServers, next.DeploymentServers)); err != nil {
		_ = s.setLastError(ctx, err.Error())
	}
	return s.GetReverseProxy(ctx)
}

func (s *Service) syncReverseProxyTraits(ctx context.Context, previous, next []string) error {
	ids := uniqueSorted(append(append([]string{}, previous...), next...))
	enabled := map[string]struct{}{}
	for _, id := range next {
		enabled[id] = struct{}{}
	}
	for _, serverID := range ids {
		var raw string
		if err := s.db.QueryRowContext(ctx, `SELECT traits FROM servers WHERE id=?`, serverID).Scan(&raw); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		traits := map[string]string{}
		_ = json.Unmarshal([]byte(raw), &traits)
		if _, ok := enabled[serverID]; ok {
			traits[reverseProxyEnabledTrait] = "true"
		} else {
			delete(traits, reverseProxyEnabledTrait)
		}
		nextRaw, err := json.Marshal(traits)
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE servers SET traits=?,updated_at=? WHERE id=?`, string(nextRaw), time.Now().UTC().Format(time.RFC3339Nano), serverID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ReconcileReverseProxy(ctx context.Context) error {
	return s.triggerReverseProxyReconcile(ctx, "application_change", nil)
}

func (s *Service) ReconcileReverseProxyNow(ctx context.Context) (ReconcileResult, error) {
	if err := s.triggerReverseProxyReconcile(ctx, "user", nil); err != nil {
		_ = s.setLastError(ctx, err.Error())
		return ReconcileResult{}, err
	}
	cfg, err := s.GetReverseProxy(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Config: cfg}, nil
}

func (s *Service) RuntimeSpecForServer(ctx context.Context, app applications.Application, srv server.Server) (appruntime.Spec, bool, error) {
	if app.ID != proxyApplicationID {
		return appruntime.Spec{}, false, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return appruntime.Spec{}, true, err
	}
	routes, err := s.routesByServer(ctx, []string{srv.ID})
	if err != nil {
		return appruntime.Spec{}, true, err
	}
	certificates, err := s.reverseProxyCertificates(ctx)
	if err != nil {
		return appruntime.Spec{}, true, err
	}
	spec, err := s.proxySpec(ctx, srv.ID, cfg, routes[srv.ID], certificates)
	if err != nil {
		return appruntime.Spec{}, true, err
	}
	spec.Generation = app.Generation
	spec.SpecHash = app.SpecHash
	return spec, true, nil
}

func (s *Service) triggerReverseProxyReconcile(ctx context.Context, triggerType string, stopServers []string) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if _, _, err := s.ensureReverseProxyApplication(ctx, cfg); err != nil {
		return err
	}
	if s.reconciler == nil {
		return s.reconcileReverseProxy(ctx, stopServers)
	}
	_, _, err = s.reconciler.TriggerApplicationReconcile(ctx, tasks.PeriodicTrigger{
		Type:                firstNonEmpty(triggerType, "facility_app"),
		Manual:              triggerType == "user",
		TriggerResourceType: "application",
		TriggerResourceID:   proxyApplicationID,
		Payload:             reverseProxyReconcilePayload(stopServers),
	})
	return err
}

func reverseProxyReconcilePayload(stopServers []string) map[string]any {
	return map[string]any{
		"applicationIds": []string{proxyApplicationID},
		"reason":         "reverse_proxy_changed",
		"stopServers":    uniqueSorted(stopServers),
	}
}

func (s *Service) reconcileReverseProxy(ctx context.Context, stopServers []string) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	generation, cfgHash, err := s.ensureReverseProxyApplication(ctx, cfg)
	if err != nil {
		return err
	}
	operation, err := s.createLifecycleOperation(ctx, cfg, stopServers, generation, cfgHash)
	if err != nil {
		return err
	}
	failures := []string{}
	for _, serverID := range stopServers {
		targetID := lifecycleTargetID(operation.ID, serverID)
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusDeploying, Stage: "stop_container", Started: true})
		if err := s.stopServerProxy(ctx, serverID); err != nil {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusFailed, Stage: "stop_container", Error: err.Error(), Finished: true})
			failures = append(failures, serverID+": "+err.Error())
		} else {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusRunning, Stage: "stop_container", Finished: true})
		}
	}
	if len(cfg.DeploymentServers) == 0 {
		if len(failures) > 0 {
			err := errors.New(strings.Join(failures, "; "))
			_ = s.finishLifecycleOperation(ctx, operation.ID, lifecycleStatusForFailures(len(stopServers), len(failures)), err)
			return err
		}
		_ = s.finishLifecycleOperation(ctx, operation.ID, applications.LifecycleStatusDeployed, nil)
		_ = s.setLastError(ctx, "")
		return nil
	}
	routes, err := s.routesByServer(ctx, cfg.DeploymentServers)
	if err != nil {
		_ = s.failDeployTargets(ctx, operation.ID, cfg.DeploymentServers, "render", err)
		_ = s.finishLifecycleOperation(ctx, operation.ID, applications.LifecycleStatusFailed, err)
		return err
	}
	certificates, err := s.reverseProxyCertificates(ctx)
	if err != nil {
		_ = s.failDeployTargets(ctx, operation.ID, cfg.DeploymentServers, "render", err)
		_ = s.finishLifecycleOperation(ctx, operation.ID, applications.LifecycleStatusFailed, err)
		return err
	}
	for _, serverID := range cfg.DeploymentServers {
		targetID := lifecycleTargetID(operation.ID, serverID)
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusPreparing, Stage: "validate_agent", Started: true})
		if err := s.deployServerProxy(ctx, cfg, serverID, routes[serverID], certificates); err != nil {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusFailed, Stage: "deploy_proxy", Error: err.Error(), Finished: true})
			failures = append(failures, serverID+": "+err.Error())
		} else {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusRunning, Stage: "deploy_proxy", InstanceID: instanceID(serverID), ContainerName: proxyContainerName, Finished: true})
		}
	}
	if len(failures) > 0 {
		err := errors.New(strings.Join(failures, "; "))
		_ = s.finishLifecycleOperation(ctx, operation.ID, lifecycleStatusForFailures(len(cfg.DeploymentServers)+len(stopServers), len(failures)), err)
		return err
	}
	_ = s.finishLifecycleOperation(ctx, operation.ID, applications.LifecycleStatusDeployed, nil)
	return s.setLastError(ctx, "")
}

func (s *Service) stopServerProxy(ctx context.Context, serverID string) error {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return err
	}
	run := func(runCtx context.Context) error {
		_, err := s.agent.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{
			ApplicationID: proxyApplicationID,
			InstanceID:    instanceID(serverID),
			ContainerName: proxyContainerName,
		})
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}
	if s.queue != nil {
		return s.queue.Execute(ctx, serverID, run)
	}
	return run(ctx)
}

func (s *Service) deployServerProxy(ctx context.Context, cfg ReverseProxyConfig, serverID string, routes []applications.ApplicationReverseProxyConfig, certificates []proxycert.Certificate) error {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return err
	}
	spec, err := s.proxySpec(ctx, serverID, cfg, routes, certificates)
	if err != nil {
		return err
	}
	run := func(runCtx context.Context) error {
		if err := s.agent.DockerImagePull(runCtx, baseURL, cfg.Image); err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		_, _ = s.agent.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{
			ApplicationID: proxyApplicationID,
			InstanceID:    instanceID(serverID),
			ContainerName: proxyContainerName,
			Purge:         true,
		})
		if err := s.agent.RuntimeWriteFiles(runCtx, baseURL, agentcontract.RuntimeWriteFilesRequest{Spec: spec}); err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		if _, err := s.agent.RuntimeCreateContainer(runCtx, baseURL, agentcontract.RuntimeCreateContainerRequest{ServerID: serverID, Spec: spec}); err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		if err := s.agent.DockerContainerAction(runCtx, baseURL, proxyContainerName, "start"); err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		return nil
	}
	if s.queue != nil {
		return s.queue.Execute(ctx, serverID, run)
	}
	return run(ctx)
}

func (s *Service) readyServer(ctx context.Context, serverID string) (server.Server, string, error) {
	if s.servers == nil {
		return server.Server{}, "", panelerr.Validation("server_provider_unavailable", "Server provider is unavailable")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, "", err
	}
	baseURL := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if baseURL == "" {
		return server.Server{}, "", panelerr.Validation("agent_required", "Agent is required for facility applications")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return server.Server{}, "", panelerr.Validation("agent_incompatible", "Agent is not compatible with facility applications")
	}
	return srv, baseURL, nil
}

func (s *Service) handleAgentError(ctx context.Context, srv server.Server, err error) bool {
	if s.agentErrors == nil {
		return false
	}
	return s.agentErrors.HandleAgentError(ctx, srv, err)
}

func (s *Service) routeCount(ctx context.Context, serverIDs []string) int {
	routes, err := s.routesByServer(ctx, serverIDs)
	if err != nil {
		return 0
	}
	count := 0
	for _, items := range routes {
		for _, app := range items {
			count += len(app.Routes)
		}
	}
	return count
}

func (s *Service) routesByServer(ctx context.Context, serverIDs []string) (map[string][]applications.ApplicationReverseProxyConfig, error) {
	out := map[string][]applications.ApplicationReverseProxyConfig{}
	for _, id := range serverIDs {
		out[id] = []applications.ApplicationReverseProxyConfig{}
	}
	if s.apps == nil {
		return out, nil
	}
	apps, err := s.apps.ApplicationReverseProxyConfigs(ctx)
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		for _, serverID := range serverIDs {
			if appTargetsServer(app, serverID) {
				out[serverID] = append(out[serverID], app)
			}
		}
	}
	return out, nil
}

func appTargetsServer(app applications.ApplicationReverseProxyConfig, serverID string) bool {
	if app.DeploymentMode == applications.DeploymentModeAll || strings.TrimSpace(app.DeploymentMode) == "" {
		return true
	}
	for _, id := range app.DeploymentServers {
		if id == serverID {
			return true
		}
	}
	return false
}

func (s *Service) proxySpec(ctx context.Context, serverID string, cfg ReverseProxyConfig, routes []applications.ApplicationReverseProxyConfig, certificates []proxycert.Certificate) (appruntime.Spec, error) {
	nginx, mounts, files, err := s.renderNginxConfig(ctx, serverID, cfg, routes, certificates)
	if err != nil {
		return appruntime.Spec{}, err
	}
	hash := specHash(serverID, cfg, routes, nginx, files)
	networkMode := "host"
	var ports []appruntime.Port
	if applicationRoutesNeedContainerNetwork(routes) {
		networkMode = "bridge"
		ports = []appruntime.Port{
			{Label: "http", ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
			{Label: "https", ContainerPort: 443, HostPort: 443, Protocol: "tcp"},
		}
	}
	if managedFilesContainPrefix(files, "certs/") {
		mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: "certs", Target: proxyTLSMountRoot, ReadOnly: true})
	}
	if managedFilesContainPrefix(files, proxyConfigDir+"/") {
		mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: proxyConfigDir, Target: proxyContainerConfDir, ReadOnly: true})
	}
	return appruntime.Spec{
		ID:            proxyApplicationID,
		ApplicationID: proxyApplicationID,
		InstanceID:    instanceID(serverID),
		ContainerName: proxyContainerName,
		Name:          "reverse-proxy",
		Image:         cfg.Image,
		Ports:         ports,
		NetworkMode:   networkMode,
		Mounts: append([]appruntime.Mount{
			{Type: "managed_file", Source: proxyConfigPath, Target: proxyContainerConf, ReadOnly: true},
		}, mounts...),
		Files:      append([]appruntime.ManagedFile{{Path: proxyConfigPath, Content: []byte(nginx), Mode: "0644"}}, files...),
		Restart:    appruntime.Restart{Policy: "no"},
		Generation: 1,
		SpecHash:   hash,
	}, nil
}

func managedFilesContainPrefix(files []appruntime.ManagedFile, prefix string) bool {
	for _, file := range files {
		if strings.HasPrefix(file.Path, prefix) {
			return true
		}
	}
	return false
}

func (s *Service) renderNginxConfig(ctx context.Context, serverID string, cfg ReverseProxyConfig, apps []applications.ApplicationReverseProxyConfig, certificates []proxycert.Certificate) (string, []appruntime.Mount, []appruntime.ManagedFile, error) {
	mainConfig := renderMainNginxConfig()
	mounts := []appruntime.Mount{}
	files := []appruntime.ManagedFile{}
	certFiles := map[string]struct{}{}
	hosts := map[string]*proxyHost{}
	localUpstreamHost := "127.0.0.1"
	if applicationRoutesNeedContainerNetwork(apps) {
		localUpstreamHost = proxyBridgeLocalHost
	}
	for i, site := range cfg.StaticSites {
		if !staticSiteTargetsServer(site, serverID) {
			continue
		}
		domain := sanitizeNginxToken(site.Domain)
		if domain == "" {
			continue
		}
		pathValue := sanitizeNginxPath(firstNonEmpty(site.Path, "/"))
		host := hostForDomain(hosts, domain)
		route := proxyFacilityRoute{
			Path:            pathValue,
			RuleType:        normalizedStaticRuleType(site.RuleType),
			RedirectURL:     site.RedirectURL,
			RedirectCode:    normalizedRedirectCode(site.RedirectCode),
			ProxyURL:        site.ProxyURL,
			ProxySourceMode: normalizedProxySourceMode(site.ProxySourceMode),
		}
		if route.RuleType == StaticRuleStatic {
			mountTarget := proxyStaticMountRoot + "/" + strconv.Itoa(i)
			asset, err := s.staticSiteMount(ctx, site, mountTarget, &mounts, &files)
			if err != nil {
				return "", nil, nil, err
			}
			if asset == nil {
				continue
			}
			route.MountTarget = mountTarget
			route.Asset = asset
		}
		host.Facility = append(host.Facility, route)
	}
	for _, app := range apps {
		for _, route := range app.Routes {
			domain := sanitizeNginxToken(route.Domain)
			if domain == "" || route.TargetPort <= 0 {
				continue
			}
			if cfg.PanelEntry.Enabled && cfg.PanelEntry.ServerID == serverID && domain == cfg.PanelEntry.Domain && reverseProxyRouteHasPath(route, "/") {
				return "", nil, nil, panelerr.Conflict("facility_panel_entry_route_conflict", "Panel entry domain conflicts with an application route")
			}
			host := hostForDomain(hosts, domain)
			host.Proxy = append(host.Proxy, route)
		}
	}
	if cfg.PanelEntry.Enabled && cfg.PanelEntry.ServerID == serverID {
		domain := sanitizeNginxToken(cfg.PanelEntry.Domain)
		if domain != "" {
			host := hostForDomain(hosts, domain)
			host.Facility = append(host.Facility, proxyFacilityRoute{
				Path:            "/",
				RuleType:        StaticRuleProxyPass,
				ProxyURL:        defaultPanelUpstream,
				ProxySourceMode: ProxySourcePreserve,
			})
		}
	}
	domains := make([]string, 0, len(hosts))
	for domain := range hosts {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	for _, domain := range domains {
		host := hosts[domain]
		cert := bestCertificate(domain, certificates)
		appendCertificateFiles(cert, &files, certFiles)
		var domainConfig strings.Builder
		writeProxyServer(&domainConfig, domain, host, nil, false, localUpstreamHost)
		if cert != nil {
			writeProxyServer(&domainConfig, domain, host, cert, true, localUpstreamHost)
		}
		appendManagedFile(&files, appruntime.ManagedFile{Path: nginxDomainConfigPath(domain), Content: []byte(domainConfig.String()), Mode: "0644"})
	}
	return mainConfig, mounts, files, nil
}

func applicationRoutesNeedContainerNetwork(apps []applications.ApplicationReverseProxyConfig) bool {
	for _, app := range apps {
		for _, route := range app.Routes {
			if strings.TrimSpace(route.TargetType) == applications.ReverseProxyTargetContainer {
				return true
			}
		}
	}
	return false
}

func renderMainNginxConfig() string {
	return `user nginx;
worker_processes auto;
error_log /dev/stderr;
pid /run/nginx.pid;

include /usr/share/nginx/modules/*.conf;

events {
    worker_connections 1024;
}

http {
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" "$http_x_forwarded_for"';

    access_log /dev/stdout main;

    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 65;
    types_hash_max_size 4096;
    client_max_body_size 50m;
    client_header_buffer_size 32k;
    large_client_header_buffers 4 32k;

    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    http2 on;
    http3 on;
    ssl_early_data on;
    quic_retry on;

    ssl_session_cache shared:SSL:50m;
    ssl_session_timeout 1h;
    ssl_ciphers EECDH+CHACHA20:EECDH+CHACHA20-draft:EECDH+AES128:RSA+AES128:EECDH+AES256:RSA+AES256;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers on;
    ssl_buffer_size 4k;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        '' "";
    }

    include /etc/nginx/conf.d/*.conf;
}
`
}

func nginxDomainConfigPath(domain string) string {
	return proxyConfigDir + "/" + nginxDomainConfigName(domain) + ".conf"
}

func nginxDomainConfigName(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	var b strings.Builder
	for _, r := range domain {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	name := strings.Trim(b.String(), ".-_")
	if name == "" {
		return "domain"
	}
	return name
}

type proxyHost struct {
	Facility []proxyFacilityRoute
	Proxy    []applications.ReverseProxyRoute
}

type proxyFacilityRoute struct {
	Path            string
	RuleType        string
	MountTarget     string
	Asset           *staticMountAsset
	RedirectURL     string
	RedirectCode    int
	ProxyURL        string
	ProxySourceMode string
}

type staticMountAsset struct {
	Kind     string
	Filename string
}

func hostForDomain(hosts map[string]*proxyHost, domain string) *proxyHost {
	host := hosts[domain]
	if host == nil {
		host = &proxyHost{}
		hosts[domain] = host
	}
	return host
}

func (s *Service) staticSiteMount(ctx context.Context, site StaticSite, mountTarget string, mounts *[]appruntime.Mount, files *[]appruntime.ManagedFile) (*staticMountAsset, error) {
	sourceType := normalizedStaticSourceType(site.SourceType)
	switch sourceType {
	case StaticSourceHostPath:
		root := strings.TrimSpace(site.RootPath)
		if root == "" {
			return nil, nil
		}
		*mounts = append(*mounts, appruntime.Mount{Type: "bind", Source: root, Target: mountTarget, ReadOnly: true})
		return &staticMountAsset{Kind: StaticSourceUploadedBundle}, nil
	case StaticSourceUploadedFile, StaticSourceUploadedBundle:
		assetID := strings.TrimSpace(site.AssetID)
		if assetID == "" {
			return nil, nil
		}
		asset, err := s.getStaticAsset(ctx, assetID)
		if err != nil {
			return nil, err
		}
		if asset.Kind != sourceType {
			return nil, panelerr.Validation("facility_static_site_asset_kind_invalid", "Static site asset kind does not match its source")
		}
		assetFiles, err := s.staticAssetFiles(assetID)
		if err != nil {
			return nil, err
		}
		base := "static-assets/" + assetID
		for _, file := range assetFiles {
			appendManagedFile(files, appruntime.ManagedFile{Path: base + "/" + file.Path, Content: file.Content, Mode: "0644"})
		}
		*mounts = append(*mounts, appruntime.Mount{Type: "managed_file", Source: base, Target: mountTarget, ReadOnly: true})
		return &staticMountAsset{Kind: sourceType, Filename: asset.Filename}, nil
	default:
		return nil, panelerr.Validation("facility_static_site_source_invalid", "Static site source is invalid")
	}
}

func appendManagedFile(files *[]appruntime.ManagedFile, file appruntime.ManagedFile) {
	*files = append(*files, file)
}

func writeProxyServer(b *strings.Builder, domain string, host *proxyHost, cert *proxycert.Certificate, https bool, localUpstreamHost string) {
	b.WriteString("\n    server {\n")
	if https {
		b.WriteString("        listen 443 ssl;\n")
		b.WriteString("        listen 443 quic;\n")
	} else {
		b.WriteString("        listen 80;\n")
	}
	b.WriteString("        server_name " + domain + ";\n")
	if cert != nil {
		b.WriteString("        ssl_certificate " + certPath(cert.ID, "certificate") + ";\n")
		b.WriteString("        ssl_certificate_key " + certPath(cert.ID, "private-key") + ";\n")
		b.WriteString("        add_header Alt-Svc 'h3=\":443\"; ma=86400' always;\n")
	}
	facilityRoutes := append([]proxyFacilityRoute(nil), host.Facility...)
	sort.SliceStable(facilityRoutes, func(i, j int) bool {
		return len(facilityRoutes[i].Path) > len(facilityRoutes[j].Path)
	})
	for _, route := range facilityRoutes {
		writeFacilityLocation(b, route, https)
	}
	proxyRoutes := append([]applications.ReverseProxyRoute(nil), host.Proxy...)
	sort.SliceStable(proxyRoutes, func(i, j int) bool {
		return len(firstNonEmptyPath(proxyRoutes[i].Paths)) > len(firstNonEmptyPath(proxyRoutes[j].Paths))
	})
	for _, route := range proxyRoutes {
		writeProxyLocations(b, route, https, localUpstreamHost)
	}
	b.WriteString("    }\n")
}

func firstNonEmptyPath(paths []applications.ReverseProxyPath) string {
	for _, path := range paths {
		if strings.TrimSpace(path.Path) != "" {
			return path.Path
		}
	}
	return "/"
}

func writeFacilityLocation(b *strings.Builder, route proxyFacilityRoute, https bool) {
	switch route.RuleType {
	case StaticRuleRedirect:
		writeRedirectLocation(b, route.Path, route.RedirectURL, route.RedirectCode)
	case StaticRuleProxyPass:
		writeFacilityProxyLocation(b, route.Path, route.ProxyURL, route.ProxySourceMode, https)
	default:
		if route.Asset != nil {
			writeStaticLocation(b, route.Path, route.MountTarget, *route.Asset)
		}
	}
}

func writeStaticLocation(b *strings.Builder, pathValue, mountTarget string, asset staticMountAsset) {
	pathValue = sanitizeNginxPath(pathValue)
	if asset.Kind == StaticSourceUploadedFile {
		b.WriteString("        location = " + pathValue + " {\n")
		b.WriteString("            alias " + strings.TrimRight(mountTarget, "/") + "/" + sanitizeNginxPathSegment(asset.Filename) + ";\n")
		b.WriteString("        }\n")
		return
	}
	target := strings.TrimRight(mountTarget, "/") + "/"
	if pathValue != "/" && !strings.HasSuffix(pathValue, "/") {
		b.WriteString("        location = " + pathValue + " {\n")
		b.WriteString("            return 301 " + pathValue + "/;\n")
		b.WriteString("        }\n")
		pathValue += "/"
	}
	b.WriteString("        location " + pathValue + " {\n")
	b.WriteString("            alias " + target + ";\n")
	b.WriteString("            index index.html;\n")
	b.WriteString("        }\n")
}

func writeRedirectLocation(b *strings.Builder, pathValue, target string, code int) {
	pathValue = sanitizeNginxPath(pathValue)
	code = normalizedRedirectCode(code)
	b.WriteString("        location " + pathValue + " {\n")
	b.WriteString("            return " + strconv.Itoa(code) + " " + target + ";\n")
	b.WriteString("        }\n")
}

func writeFacilityProxyLocation(b *strings.Builder, pathValue, target, sourceMode string, https bool) {
	pathValue = sanitizeNginxPath(pathValue)
	b.WriteString("        location " + pathValue + " {\n")
	b.WriteString("            proxy_pass " + target + ";\n")
	if normalizedProxySourceMode(sourceMode) == ProxySourceHide {
		b.WriteString("            proxy_set_header Host $proxy_host;\n")
		b.WriteString("            proxy_set_header X-Real-IP \"\";\n")
		b.WriteString("            proxy_set_header X-Forwarded-For \"\";\n")
		b.WriteString("            proxy_set_header X-Forwarded-Proto \"\";\n")
	} else {
		b.WriteString("            proxy_set_header Host $host;\n")
		b.WriteString("            proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		if https {
			b.WriteString("            proxy_set_header X-Forwarded-Proto https;\n")
		} else {
			b.WriteString("            proxy_set_header X-Forwarded-Proto $scheme;\n")
		}
	}
	b.WriteString("        }\n")
}

func writeProxyLocations(b *strings.Builder, route applications.ReverseProxyRoute, https bool, localUpstreamHost string) {
	for _, routePath := range route.Paths {
		pathValue := sanitizeNginxPath(firstNonEmpty(routePath.Path, "/"))
		b.WriteString("        location " + pathValue + " {\n")
		b.WriteString("            proxy_pass " + applicationProxyUpstream(route, localUpstreamHost) + ";\n")
		b.WriteString("            proxy_set_header Host $host;\n")
		b.WriteString("            proxy_set_header X-Real-IP $remote_addr;\n")
		b.WriteString("            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
		if https {
			b.WriteString("            proxy_set_header X-Forwarded-Proto https;\n")
		} else {
			b.WriteString("            proxy_set_header X-Forwarded-Proto $scheme;\n")
		}
		if routePath.WebSocket {
			b.WriteString("            proxy_http_version 1.1;\n")
			b.WriteString("            proxy_set_header Upgrade $http_upgrade;\n")
			b.WriteString("            proxy_set_header Connection $connection_upgrade;\n")
		}
		b.WriteString("        }\n")
	}
}

func applicationProxyUpstream(route applications.ReverseProxyRoute, localUpstreamHost string) string {
	host := strings.TrimSpace(localUpstreamHost)
	if host == "" {
		host = "127.0.0.1"
	}
	if strings.TrimSpace(route.TargetType) == applications.ReverseProxyTargetContainer {
		container := strings.TrimSpace(route.TargetContainer)
		if container != "" && validNginxValue(container) {
			host = container
		}
	}
	return "http://" + host + ":" + strconv.Itoa(route.TargetPort)
}

func (s *Service) loadConfig(ctx context.Context) (ReverseProxyConfig, error) {
	cfg := ReverseProxyConfig{ID: ReverseProxyID, Image: defaultProxyImage, DeploymentServers: []string{}, StaticSites: []StaticSite{}}
	row := s.db.QueryRowContext(ctx, `SELECT deployment_server_ids_json,image,panel_entry_json,static_sites_json,last_error,updated_at FROM facility_app_configs WHERE id=?`, ReverseProxyID)
	var serversRaw, panelRaw, staticRaw, updated string
	if err := row.Scan(&serversRaw, &cfg.Image, &panelRaw, &staticRaw, &cfg.LastError, &updated); err != nil {
		if err == sql.ErrNoRows {
			cfg.UpdatedAt = time.Now().UTC()
			return cfg, nil
		}
		return ReverseProxyConfig{}, err
	}
	_ = json.Unmarshal([]byte(serversRaw), &cfg.DeploymentServers)
	_ = json.Unmarshal([]byte(panelRaw), &cfg.PanelEntry)
	_ = json.Unmarshal([]byte(staticRaw), &cfg.StaticSites)
	if cfg.DeploymentServers == nil {
		cfg.DeploymentServers = []string{}
	}
	cfg.PanelEntry = normalizeStoredPanelEntry(cfg.PanelEntry)
	if cfg.StaticSites == nil {
		cfg.StaticSites = []StaticSite{}
	}
	for i := range cfg.StaticSites {
		cfg.StaticSites[i].RuleType = normalizedStaticRuleType(cfg.StaticSites[i].RuleType)
		cfg.StaticSites[i].SourceType = normalizedStaticSourceType(cfg.StaticSites[i].SourceType)
		cfg.StaticSites[i].ProxySourceMode = normalizedProxySourceMode(cfg.StaticSites[i].ProxySourceMode)
		cfg.StaticSites[i].DeploymentServers = uniqueSorted(cfg.StaticSites[i].DeploymentServers)
	}
	if cfg.Image == "" {
		cfg.Image = defaultProxyImage
	}
	cfg.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return cfg, nil
}

func (s *Service) saveConfig(ctx context.Context, cfg ReverseProxyConfig) error {
	serversRaw, err := json.Marshal(cfg.DeploymentServers)
	if err != nil {
		return err
	}
	panelRaw, err := json.Marshal(cfg.PanelEntry)
	if err != nil {
		return err
	}
	staticRaw, err := json.Marshal(cfg.StaticSites)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO facility_app_configs(id,deployment_server_ids_json,image,panel_entry_json,static_sites_json,last_error,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET deployment_server_ids_json=excluded.deployment_server_ids_json,image=excluded.image,panel_entry_json=excluded.panel_entry_json,static_sites_json=excluded.static_sites_json,updated_at=excluded.updated_at`,
		ReverseProxyID, string(serversRaw), cfg.Image, string(panelRaw), string(staticRaw), cfg.LastError, now)
	return err
}

func (s *Service) setLastError(ctx context.Context, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE facility_app_configs SET last_error=?,updated_at=? WHERE id=?`, message, time.Now().UTC().Format(time.RFC3339Nano), ReverseProxyID)
	return err
}

func (s *Service) ensureReverseProxyApplication(ctx context.Context, cfg ReverseProxyConfig) (int, string, error) {
	cfgHash := facilityConfigHash(cfg)
	now := time.Now().UTC()
	var generation int
	var currentHash string
	err := s.db.QueryRowContext(ctx, `SELECT generation,spec_hash FROM applications WHERE id=?`, proxyApplicationID).Scan(&generation, &currentHash)
	if err == sql.ErrNoRows {
		deploymentServers, _ := json.Marshal(cfg.DeploymentServers)
		reverseProxy, _ := json.Marshal([]applications.ReverseProxyRule{})
		_, err = s.db.ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,resolved_variables_json,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,image_reference,image_digest,image_latest_digest,image_checked_at,image_update_available,image_last_error,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			proxyApplicationID, "__panel_facility_reverse_proxy__", boolInt(len(cfg.DeploymentServers) > 0), facilitySpecYAML(cfg), "{}", "{}", applications.DeploymentModeSelected, string(deploymentServers), string(reverseProxy), 1, cfgHash, cfg.Image, "", "", nil, 0, "", proxyApplicationID, "facility", "", "", cfg.LastError, formatTime(now), formatTime(now))
		return 1, cfgHash, err
	}
	if err != nil {
		return 0, "", err
	}
	if currentHash != cfgHash {
		generation += 1
	}
	deploymentServers, _ := json.Marshal(cfg.DeploymentServers)
	_, err = s.db.ExecContext(ctx, `UPDATE applications SET enabled=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,generation=?,spec_hash=?,image_reference=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(len(cfg.DeploymentServers) > 0), facilitySpecYAML(cfg), applications.DeploymentModeSelected, string(deploymentServers), generation, cfgHash, cfg.Image, cfg.LastError, formatTime(now), proxyApplicationID)
	if err != nil {
		return 0, "", err
	}
	return generation, cfgHash, nil
}

func (s *Service) createLifecycleOperation(ctx context.Context, cfg ReverseProxyConfig, stopServers []string, generation int, cfgHash string) (applications.LifecycleOperation, error) {
	now := time.Now().UTC()
	operation := applications.LifecycleOperation{
		ID:            id.New("alop"),
		ApplicationID: proxyApplicationID,
		Type:          applications.LifecycleTypeDeploy,
		Status:        applications.LifecycleStatusDeploying,
		Generation:    generation,
		SpecHash:      cfgHash,
		Trigger:       "facility_app",
		CreatedAt:     now,
		StartedAt:     &now,
		UpdatedAt:     now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_lifecycle_operations(id,application_id,type,status,task_id,generation,spec_hash,trigger,error,created_at,started_at,finished_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		operation.ID, operation.ApplicationID, operation.Type, operation.Status, "", operation.Generation, operation.SpecHash, operation.Trigger, "", formatTime(now), formatTime(now), nil, formatTime(now))
	if err != nil {
		return applications.LifecycleOperation{}, err
	}
	desired := map[string]string{}
	for _, serverID := range stopServers {
		desired[serverID] = appruntime.DesiredStopped
	}
	for _, serverID := range cfg.DeploymentServers {
		desired[serverID] = appruntime.DesiredRunning
	}
	serverIDs := make([]string, 0, len(desired))
	for serverID := range desired {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)
	for _, serverID := range serverIDs {
		target := applications.LifecycleTarget{
			ID:            lifecycleTargetID(operation.ID, serverID),
			OperationID:   operation.ID,
			ApplicationID: proxyApplicationID,
			ServerID:      serverID,
			Status:        applications.LifecycleTargetStatusPending,
			DesiredState:  desired[serverID],
			InstanceID:    instanceID(serverID),
			ContainerName: proxyContainerName,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO application_lifecycle_targets(id,operation_id,application_id,server_id,status,desired_state,instance_id,container_name,container_id,stage,error,created_at,started_at,finished_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			target.ID, target.OperationID, target.ApplicationID, target.ServerID, target.Status, target.DesiredState, target.InstanceID, target.ContainerName, "", "", "", formatTime(now), nil, nil, formatTime(now))
		if err != nil {
			return applications.LifecycleOperation{}, err
		}
		operation.Targets = append(operation.Targets, target)
	}
	return operation, nil
}

type lifecycleTargetUpdate struct {
	Status        string
	Stage         string
	InstanceID    string
	ContainerName string
	ContainerID   string
	Error         string
	Started       bool
	Finished      bool
}

func (s *Service) updateLifecycleTarget(ctx context.Context, targetID string, in lifecycleTargetUpdate) error {
	updates := []string{}
	args := []any{}
	add := func(column string, value any) {
		updates = append(updates, column+"=?")
		args = append(args, value)
	}
	if in.Status != "" {
		add("status", in.Status)
	}
	if in.Stage != "" {
		add("stage", in.Stage)
	}
	if in.InstanceID != "" {
		add("instance_id", in.InstanceID)
	}
	if in.ContainerName != "" {
		add("container_name", in.ContainerName)
	}
	if in.ContainerID != "" {
		add("container_id", in.ContainerID)
	}
	if in.Error != "" {
		add("error", in.Error)
	}
	now := formatTime(time.Now().UTC())
	if in.Started {
		add("started_at", now)
	}
	if in.Finished {
		add("finished_at", now)
	}
	add("updated_at", now)
	args = append(args, targetID)
	_, err := s.db.ExecContext(ctx, `UPDATE application_lifecycle_targets SET `+strings.Join(updates, ",")+` WHERE id=?`, args...)
	return err
}

func (s *Service) failDeployTargets(ctx context.Context, operationID string, serverIDs []string, stage string, cause error) error {
	for _, serverID := range serverIDs {
		if err := s.updateLifecycleTarget(ctx, lifecycleTargetID(operationID, serverID), lifecycleTargetUpdate{Status: applications.LifecycleTargetStatusFailed, Stage: stage, Error: cause.Error(), Started: true, Finished: true}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finishLifecycleOperation(ctx context.Context, operationID, status string, cause error) error {
	errText := ""
	if cause != nil {
		errText = cause.Error()
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `UPDATE application_lifecycle_operations SET status=?,error=?,finished_at=?,updated_at=? WHERE id=?`, status, errText, now, now, operationID)
	return err
}

func (s *Service) latestLifecycleOperation(ctx context.Context) (applications.LifecycleOperation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,type,status,task_id,generation,spec_hash,trigger,error,created_at,started_at,finished_at,updated_at
		FROM application_lifecycle_operations WHERE application_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, proxyApplicationID)
	operation, err := scanLifecycleOperation(row)
	if err == sql.ErrNoRows {
		return applications.LifecycleOperation{}, panelerr.NotFound("facility_app_lifecycle_operation")
	}
	if err != nil {
		return applications.LifecycleOperation{}, err
	}
	targets, err := s.lifecycleTargets(ctx, operation.ID)
	if err != nil {
		return applications.LifecycleOperation{}, err
	}
	operation.Targets = targets
	return operation, nil
}

func (s *Service) lifecycleTargets(ctx context.Context, operationID string) ([]applications.LifecycleTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.operation_id,t.application_id,t.server_id,COALESCE(s.name,''),t.status,t.desired_state,t.instance_id,t.container_name,t.container_id,t.stage,t.error,t.created_at,t.started_at,t.finished_at,t.updated_at
		FROM application_lifecycle_targets t LEFT JOIN servers s ON s.id=t.server_id WHERE t.operation_id=? ORDER BY t.server_id ASC`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []applications.LifecycleTarget{}
	for rows.Next() {
		target, err := scanLifecycleTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func normalizeInput(in ReverseProxySaveInput) (ReverseProxyConfig, error) {
	image := strings.TrimSpace(in.Image)
	if image == "" {
		image = defaultProxyImage
	}
	servers := uniqueSorted(in.DeploymentServers)
	serverSet := map[string]struct{}{}
	for _, serverID := range servers {
		serverSet[serverID] = struct{}{}
	}
	sites := make([]StaticSite, 0, len(in.StaticSites))
	routeKeys := map[string]struct{}{}
	for _, site := range in.StaticSites {
		domain := strings.TrimSpace(site.Domain)
		ruleType := normalizedStaticRuleType(site.RuleType)
		sourceType := normalizedStaticSourceType(site.SourceType)
		root := strings.TrimSpace(site.RootPath)
		assetID := strings.TrimSpace(site.AssetID)
		redirectURL := strings.TrimSpace(site.RedirectURL)
		redirectCode := normalizedRedirectCode(site.RedirectCode)
		proxyURL := strings.TrimSpace(site.ProxyURL)
		proxySourceMode := normalizedProxySourceMode(site.ProxySourceMode)
		if domain == "" && root == "" && assetID == "" && redirectURL == "" && proxyURL == "" {
			continue
		}
		if domain == "" || !validNginxToken(domain) {
			return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_domain_invalid", "Static site domain is invalid")
		}
		switch ruleType {
		case StaticRuleStatic:
			switch sourceType {
			case StaticSourceHostPath:
				if root == "" || strings.Contains(root, "\x00") {
					return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_root_invalid", "Static site root path is invalid")
				}
				assetID = ""
			case StaticSourceUploadedFile, StaticSourceUploadedBundle:
				if assetID == "" || strings.ContainsAny(assetID, `/\`) {
					return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_asset_required", "Static site asset is required")
				}
				root = ""
			default:
				return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_source_invalid", "Static site source is invalid")
			}
			redirectURL = ""
			redirectCode = 0
			proxyURL = ""
			proxySourceMode = ""
		case StaticRuleRedirect:
			if !validNginxValue(redirectURL) {
				return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_redirect_invalid", "Redirect target is invalid")
			}
			root = ""
			assetID = ""
			proxyURL = ""
			proxySourceMode = ""
		case StaticRuleProxyPass:
			if !validNginxValue(proxyURL) || !validProxyURL(proxyURL) {
				return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_proxy_invalid", "Proxy target is invalid")
			}
			if proxySourceMode != ProxySourcePreserve && proxySourceMode != ProxySourceHide {
				return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_proxy_mode_invalid", "Proxy request information mode is invalid")
			}
			root = ""
			assetID = ""
			redirectURL = ""
			redirectCode = 0
		default:
			return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_rule_invalid", "Reverse proxy route type is invalid")
		}
		pathValue := strings.TrimSpace(site.Path)
		if pathValue == "" {
			pathValue = "/"
		}
		if !strings.HasPrefix(pathValue, "/") || !validNginxPath(pathValue) {
			return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_path_invalid", "Static site path must start with /")
		}
		siteServers := uniqueSorted(site.DeploymentServers)
		for _, serverID := range siteServers {
			if _, ok := serverSet[serverID]; !ok {
				return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_server_invalid", "Static site server must be selected for reverse proxy deployment")
			}
		}
		routeKey := domain + "\x00" + pathValue + "\x00" + strings.Join(siteServers, ",")
		if _, ok := routeKeys[routeKey]; ok {
			return ReverseProxyConfig{}, panelerr.Validation("facility_static_site_path_duplicate", "Reverse proxy route path is duplicated for this domain")
		}
		routeKeys[routeKey] = struct{}{}
		sites = append(sites, StaticSite{Domain: domain, Path: pathValue, RuleType: ruleType, RootPath: root, SourceType: sourceType, AssetID: assetID, RedirectURL: redirectURL, RedirectCode: redirectCode, ProxyURL: proxyURL, ProxySourceMode: proxySourceMode, DeploymentServers: siteServers})
	}
	panelEntry, err := normalizePanelEntry(in.PanelEntry, serverSet, sites)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	return ReverseProxyConfig{ID: ReverseProxyID, DeploymentServers: servers, Image: image, PanelEntry: panelEntry, StaticSites: sites}, nil
}

func normalizePanelEntry(in PanelEntry, serverSet map[string]struct{}, sites []StaticSite) (PanelEntry, error) {
	if !in.Enabled {
		return PanelEntry{}, nil
	}
	serverID := strings.TrimSpace(in.ServerID)
	domain := strings.TrimSpace(in.Domain)
	if serverID == "" {
		return PanelEntry{}, panelerr.Validation("facility_panel_entry_server_required", "Panel entry server is required")
	}
	if _, ok := serverSet[serverID]; !ok {
		return PanelEntry{}, panelerr.Validation("facility_panel_entry_server_invalid", "Panel entry server must be selected as a gateway node")
	}
	if domain == "" || !validNginxToken(domain) {
		return PanelEntry{}, panelerr.Validation("facility_panel_entry_domain_invalid", "Panel entry domain is invalid")
	}
	for _, site := range sites {
		if site.Domain == domain && sanitizeNginxPath(firstNonEmpty(site.Path, "/")) == "/" && staticSiteTargetsServer(site, serverID) {
			return PanelEntry{}, panelerr.Conflict("facility_panel_entry_static_conflict", "Panel entry domain conflicts with a static route")
		}
	}
	return PanelEntry{Enabled: true, ServerID: serverID, Domain: domain}, nil
}

func normalizeStoredPanelEntry(in PanelEntry) PanelEntry {
	if !in.Enabled {
		return PanelEntry{}
	}
	return PanelEntry{
		Enabled:  true,
		ServerID: strings.TrimSpace(in.ServerID),
		Domain:   strings.TrimSpace(in.Domain),
	}
}

func normalizedStaticRuleType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return StaticRuleStatic
	}
	return value
}

func normalizedStaticSourceType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return StaticSourceHostPath
	}
	return value
}

func normalizedRedirectCode(value int) int {
	switch value {
	case 301, 302, 307, 308:
		return value
	default:
		return 302
	}
}

func normalizedProxySourceMode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ProxySourcePreserve
	}
	return value
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func removedServers(previous, next []string) []string {
	nextSet := map[string]struct{}{}
	for _, id := range next {
		nextSet[id] = struct{}{}
	}
	out := []string{}
	for _, id := range previous {
		if _, ok := nextSet[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

func specHash(serverID string, cfg ReverseProxyConfig, routes []applications.ApplicationReverseProxyConfig, nginx string, files []appruntime.ManagedFile) string {
	raw, _ := json.Marshal(map[string]any{"server": serverID, "config": cfg, "routes": routes, "nginx": nginx, "nginxFiles": nginxConfigFileContents(files)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func nginxConfigFileContents(files []appruntime.ManagedFile) map[string]string {
	out := map[string]string{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, proxyConfigDir+"/") {
			out[file.Path] = string(file.Content)
		}
	}
	return out
}

func instanceID(serverID string) string {
	return proxyInstancePrefix + strings.TrimSpace(serverID)
}

func sanitizeNginxToken(value string) string {
	value = strings.TrimSpace(value)
	if !validNginxToken(value) {
		return ""
	}
	return value
}

func sanitizeNginxPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") || !validNginxPath(value) {
		return "/"
	}
	return value
}

func sanitizeNginxPathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n;{}") || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return "asset"
	}
	return value
}

func validNginxToken(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;{}")
}

func validNginxPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;{}")
}

func validNginxValue(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\x00\r\n;{}")
}

func validProxyURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func staticSiteTargetsServer(site StaticSite, serverID string) bool {
	if len(site.DeploymentServers) == 0 {
		return true
	}
	for _, id := range site.DeploymentServers {
		if id == serverID {
			return true
		}
	}
	return false
}

func reverseProxyRouteHasPath(route applications.ReverseProxyRoute, pathValue string) bool {
	pathValue = sanitizeNginxPath(pathValue)
	for _, routePath := range route.Paths {
		if sanitizeNginxPath(firstNonEmpty(routePath.Path, "/")) == pathValue {
			return true
		}
	}
	return false
}

func (s *Service) reverseProxyCertificates(ctx context.Context) ([]proxycert.Certificate, error) {
	if s.certificates == nil {
		return nil, nil
	}
	return s.certificates.ReverseProxyCertificates(ctx)
}

func bestCertificate(domain string, certificates []proxycert.Certificate) *proxycert.Certificate {
	var fallback *proxycert.Certificate
	for i := range certificates {
		if !certificateMatchesDomain(certificates[i], domain) {
			continue
		}
		if certificates[i].Source == HTTPSDomainCertificate {
			return &certificates[i]
		}
		if fallback == nil {
			fallback = &certificates[i]
		}
	}
	return fallback
}

func certificateMatchesDomain(cert proxycert.Certificate, domain string) bool {
	for _, pattern := range cert.Domains {
		if certificateDomainMatches(pattern, domain) {
			return true
		}
	}
	return false
}

func certificateDomainMatches(pattern, domain string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if pattern == "" || domain == "" {
		return false
	}
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(domain, suffix) && domain != strings.TrimPrefix(suffix, ".")
	}
	return false
}

func appendCertificateFiles(cert *proxycert.Certificate, files *[]appruntime.ManagedFile, seen map[string]struct{}) {
	if cert == nil {
		return
	}
	if _, ok := seen[cert.ID]; ok {
		return
	}
	seen[cert.ID] = struct{}{}
	*files = append(*files,
		appruntime.ManagedFile{Path: certFileSource(cert.ID, "certificate"), Content: []byte(cert.CertificatePEM), Mode: "0644"},
		appruntime.ManagedFile{Path: certFileSource(cert.ID, "private-key"), Content: []byte(cert.PrivateKeyPEM), Mode: "0600"},
	)
}

func certFileSource(certID, kind string) string {
	return "certs/" + sanitizeNginxPathSegment(certID) + "/" + kind + ".pem"
}

func certPath(certID, kind string) string {
	return proxyTLSMountRoot + "/" + sanitizeNginxPathSegment(certID) + "/" + kind + ".pem"
}

func (s *Service) routeSummaries(ctx context.Context, cfg ReverseProxyConfig) ([]RouteSummary, error) {
	certificates, err := s.reverseProxyCertificates(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.routesByServer(ctx, cfg.DeploymentServers)
	if err != nil {
		return nil, err
	}
	out := []RouteSummary{}
	for _, site := range cfg.StaticSites {
		domain := strings.TrimSpace(site.Domain)
		pathValue := firstNonEmpty(site.Path, "/")
		if domain == "" {
			continue
		}
		serverIDs := site.DeploymentServers
		if len(serverIDs) == 0 {
			serverIDs = cfg.DeploymentServers
		}
		out = append(out, routeSummary(domain, pathValue, "static_site", serverIDs, certificates))
	}
	if cfg.PanelEntry.Enabled {
		out = append(out, routeSummary(cfg.PanelEntry.Domain, "/", "system_panel", []string{cfg.PanelEntry.ServerID}, certificates))
	}
	for serverID, apps := range routes {
		for _, app := range apps {
			for _, route := range app.Routes {
				for _, routePath := range route.Paths {
					out = append(out, routeSummary(route.Domain, firstNonEmpty(routePath.Path, "/"), "application", []string{serverID}, certificates))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain == out[j].Domain {
			if out[i].Path == out[j].Path {
				return strings.Join(out[i].ServerIDs, ",") < strings.Join(out[j].ServerIDs, ",")
			}
			return out[i].Path < out[j].Path
		}
		return out[i].Domain < out[j].Domain
	})
	return out, nil
}

func routeSummary(domain, pathValue, source string, serverIDs []string, certificates []proxycert.Certificate) RouteSummary {
	serverIDs = uniqueSorted(serverIDs)
	summary := RouteSummary{
		Domain:      domain,
		Path:        pathValue,
		Source:      source,
		ServerIDs:   serverIDs,
		HTTPSStatus: HTTPSDisabled,
	}
	if cert := bestCertificate(domain, certificates); cert != nil {
		summary.HTTPSStatus = cert.Source
		summary.CertificateID = cert.ID
		summary.CertificateName = firstNonEmpty(cert.Name, cert.ID)
		summary.MatchedDomains = append([]string(nil), cert.Domains...)
	}
	return summary
}

type lifecycleScanner interface {
	Scan(dest ...any) error
}

func scanLifecycleOperation(row lifecycleScanner) (applications.LifecycleOperation, error) {
	var operation applications.LifecycleOperation
	var created, updated string
	var started, finished sql.NullString
	if err := row.Scan(&operation.ID, &operation.ApplicationID, &operation.Type, &operation.Status, &operation.TaskID, &operation.Generation, &operation.SpecHash, &operation.Trigger, &operation.Error, &created, &started, &finished, &updated); err != nil {
		return applications.LifecycleOperation{}, err
	}
	operation.CreatedAt = parseTime(created)
	operation.StartedAt = parseOptionalTime(started)
	operation.FinishedAt = parseOptionalTime(finished)
	operation.UpdatedAt = parseTime(updated)
	return operation, nil
}

func scanLifecycleTarget(row lifecycleScanner) (applications.LifecycleTarget, error) {
	var target applications.LifecycleTarget
	var created, updated string
	var started, finished sql.NullString
	if err := row.Scan(&target.ID, &target.OperationID, &target.ApplicationID, &target.ServerID, &target.ServerName, &target.Status, &target.DesiredState, &target.InstanceID, &target.ContainerName, &target.ContainerID, &target.Stage, &target.Error, &created, &started, &finished, &updated); err != nil {
		return applications.LifecycleTarget{}, err
	}
	target.CreatedAt = parseTime(created)
	target.StartedAt = parseOptionalTime(started)
	target.FinishedAt = parseOptionalTime(finished)
	target.UpdatedAt = parseTime(updated)
	return target, nil
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed := parseTime(value.String)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func lifecycleTargetID(operationID, serverID string) string {
	return operationID + "-" + strings.TrimSpace(serverID)
}

func lifecycleStatusForFailures(total, failures int) string {
	if failures <= 0 {
		return applications.LifecycleStatusDeployed
	}
	if total > 0 && failures >= total {
		return applications.LifecycleStatusFailed
	}
	return applications.LifecycleStatusPartiallyDeployed
}

func facilityConfigHash(cfg ReverseProxyConfig) string {
	raw, _ := json.Marshal(map[string]any{
		"image":             cfg.Image,
		"deploymentServers": cfg.DeploymentServers,
		"panelEntry":        cfg.PanelEntry,
		"staticSites":       cfg.StaticSites,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func facilitySpecYAML(cfg ReverseProxyConfig) string {
	return strings.Join([]string{
		"kind: facility/reverse-proxy",
		"name: entrance-gateway",
		"image: " + cfg.Image,
		"",
	}, "\n")
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func (cfg ReverseProxyConfig) String() string {
	return fmt.Sprintf("%s servers=%d static=%d", cfg.ID, len(cfg.DeploymentServers), len(cfg.StaticSites))
}
