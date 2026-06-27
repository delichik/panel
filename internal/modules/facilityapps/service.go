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
	panelerr "panel/internal/platform/errors"
)

const reverseProxyEnabledTrait = "agent.reverse_proxy.enabled"

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
	cfg.EnabledServers = append([]string(nil), cfg.DeploymentServers...)
	assets, err := s.ListStaticAssets(ctx)
	if err == nil {
		cfg.StaticAssets = assets
	}
	if summaries, err := s.routeSummaries(ctx, cfg); err == nil {
		cfg.RouteSummaries = summaries
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
	if err := s.reconcileReverseProxy(ctx, removedServers(previous.DeploymentServers, next.DeploymentServers)); err != nil {
		_ = s.setLastError(ctx, err.Error())
		return ReverseProxyConfig{}, err
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
	return s.reconcileReverseProxy(ctx, nil)
}

func (s *Service) ReconcileReverseProxyNow(ctx context.Context) (ReconcileResult, error) {
	if err := s.reconcileReverseProxy(ctx, nil); err != nil {
		_ = s.setLastError(ctx, err.Error())
		return ReconcileResult{}, err
	}
	cfg, err := s.GetReverseProxy(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{Config: cfg}, nil
}

func (s *Service) reconcileReverseProxy(ctx context.Context, stopServers []string) error {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	failures := []string{}
	for _, serverID := range stopServers {
		if err := s.stopServerProxy(ctx, serverID); err != nil {
			failures = append(failures, serverID+": "+err.Error())
		}
	}
	if len(cfg.DeploymentServers) == 0 {
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		_ = s.setLastError(ctx, "")
		return nil
	}
	routes, err := s.routesByServer(ctx, cfg.DeploymentServers)
	if err != nil {
		return err
	}
	certificates, err := s.reverseProxyCertificates(ctx)
	if err != nil {
		return err
	}
	for _, serverID := range cfg.DeploymentServers {
		if err := s.deployServerProxy(ctx, cfg, serverID, routes[serverID], certificates); err != nil {
			failures = append(failures, serverID+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
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
	hash := specHash(serverID, cfg, routes, nginx)
	if managedFilesContainPrefix(files, "certs/") {
		mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: "certs", Target: proxyTLSMountRoot, ReadOnly: true})
	}
	return appruntime.Spec{
		ID:            proxyApplicationID,
		ApplicationID: proxyApplicationID,
		InstanceID:    instanceID(serverID),
		ContainerName: proxyContainerName,
		Name:          "reverse-proxy",
		Image:         cfg.Image,
		NetworkMode:   "host",
		Mounts: append([]appruntime.Mount{
			{Type: "managed_file", Source: proxyConfigPath, Target: proxyContainerConf, ReadOnly: true},
		}, mounts...),
		Files:      append([]appruntime.ManagedFile{{Path: proxyConfigPath, Content: []byte(nginx), Mode: "0644"}}, files...),
		Restart:    appruntime.Restart{Policy: "unless-stopped"},
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
	var b strings.Builder
	b.WriteString("events {}\nhttp {\n")
	b.WriteString("    map $http_upgrade $connection_upgrade { default upgrade; '' close; }\n")
	mounts := []appruntime.Mount{}
	files := []appruntime.ManagedFile{}
	certFiles := map[string]struct{}{}
	hosts := map[string]*proxyHost{}
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
			host := hostForDomain(hosts, domain)
			host.Proxy = append(host.Proxy, route)
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
		writeProxyServer(&b, domain, host, nil, false)
		if cert != nil {
			writeProxyServer(&b, domain, host, cert, true)
		}
	}
	b.WriteString("}\n")
	return b.String(), mounts, files, nil
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

func writeProxyServer(b *strings.Builder, domain string, host *proxyHost, cert *proxycert.Certificate, https bool) {
	b.WriteString("\n    server {\n")
	if https {
		b.WriteString("        listen 443 ssl;\n")
	} else {
		b.WriteString("        listen 80;\n")
	}
	b.WriteString("        server_name " + domain + ";\n")
	if cert != nil {
		b.WriteString("        ssl_certificate " + certPath(cert.ID, "certificate") + ";\n")
		b.WriteString("        ssl_certificate_key " + certPath(cert.ID, "private-key") + ";\n")
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
		writeProxyLocations(b, route, https)
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

func writeProxyLocations(b *strings.Builder, route applications.ReverseProxyRoute, https bool) {
	for _, routePath := range route.Paths {
		pathValue := sanitizeNginxPath(firstNonEmpty(routePath.Path, "/"))
		b.WriteString("        location " + pathValue + " {\n")
		b.WriteString("            proxy_pass http://127.0.0.1:" + strconv.Itoa(route.TargetPort) + ";\n")
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

func (s *Service) loadConfig(ctx context.Context) (ReverseProxyConfig, error) {
	cfg := ReverseProxyConfig{ID: ReverseProxyID, Image: defaultProxyImage, DeploymentServers: []string{}, StaticSites: []StaticSite{}}
	row := s.db.QueryRowContext(ctx, `SELECT deployment_server_ids_json,image,static_sites_json,last_error,updated_at FROM facility_app_configs WHERE id=?`, ReverseProxyID)
	var serversRaw, staticRaw, updated string
	if err := row.Scan(&serversRaw, &cfg.Image, &staticRaw, &cfg.LastError, &updated); err != nil {
		if err == sql.ErrNoRows {
			cfg.UpdatedAt = time.Now().UTC()
			return cfg, nil
		}
		return ReverseProxyConfig{}, err
	}
	_ = json.Unmarshal([]byte(serversRaw), &cfg.DeploymentServers)
	_ = json.Unmarshal([]byte(staticRaw), &cfg.StaticSites)
	if cfg.DeploymentServers == nil {
		cfg.DeploymentServers = []string{}
	}
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
	staticRaw, err := json.Marshal(cfg.StaticSites)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO facility_app_configs(id,deployment_server_ids_json,image,static_sites_json,last_error,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET deployment_server_ids_json=excluded.deployment_server_ids_json,image=excluded.image,static_sites_json=excluded.static_sites_json,updated_at=excluded.updated_at`,
		ReverseProxyID, string(serversRaw), cfg.Image, string(staticRaw), cfg.LastError, now)
	return err
}

func (s *Service) setLastError(ctx context.Context, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE facility_app_configs SET last_error=?,updated_at=? WHERE id=?`, message, time.Now().UTC().Format(time.RFC3339Nano), ReverseProxyID)
	return err
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
	return ReverseProxyConfig{ID: ReverseProxyID, DeploymentServers: servers, Image: image, StaticSites: sites}, nil
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

func specHash(serverID string, cfg ReverseProxyConfig, routes []applications.ApplicationReverseProxyConfig, nginx string) string {
	raw, _ := json.Marshal(map[string]any{"server": serverID, "config": cfg, "routes": routes, "nginx": nginx})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
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
