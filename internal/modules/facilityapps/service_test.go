package facilityapps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/certificates/proxycert"
	server "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
)

func TestRenderNginxConfigGroupsRoutesByDomain(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/", RootPath: "/srv/www/root", SourceType: StaticSourceHostPath},
			{Domain: "static.example.test", Path: "/docs", RootPath: "/srv/www/docs", SourceType: StaticSourceHostPath},
			{Domain: "static.example.test", Path: "/old", RuleType: StaticRuleRedirect, RedirectURL: "https://static.example.test/new", RedirectCode: 301},
			{Domain: "static.example.test", Path: "/backend", RuleType: StaticRuleProxyPass, ProxyURL: "http://10.0.0.10:9000", ProxySourceMode: ProxySourceHide},
			{Domain: "static.example.test", Path: "/upstream", RuleType: StaticRuleProxyPass, ProxyURL: "https://upstream.example.test", ProxySourceMode: ProxySourcePreserve},
		},
	}
	apps := []applications.ApplicationReverseProxyConfig{
		{
			Routes: []applications.ReverseProxyRoute{
				{Domain: "static.example.test", TargetPort: 8080, Paths: []applications.ReverseProxyPath{{Path: "/api", Options: applications.HTTPRouteOptions{ReadTimeoutSeconds: 45, ResponseHeaders: []applications.HTTPHeader{{Name: "X-App-Route", Value: "enabled"}}}}}},
				{Domain: "static.example.test", TargetType: applications.ReverseProxyTargetContainer, TargetPort: 9000, TargetContainer: "panel-website", Paths: []applications.ReverseProxyPath{{Path: "/app"}}},
			},
		},
	}

	nginx, mounts, files, err := svc.renderNginxConfig(context.Background(), "srv-edge", cfg, apps, nil)
	if err != nil {
		t.Fatalf("render nginx config: %v", err)
	}
	if !strings.Contains(nginx, "include /etc/nginx/conf.d/*.conf;") {
		t.Fatalf("expected main nginx config to include domain config directory, got:\n%s", nginx)
	}
	domainConfig := managedFileContent(files, "conf.d/static.example.test.conf")
	if domainConfig == "" {
		t.Fatalf("expected per-domain nginx config file, files=%#v", files)
	}
	if strings.Count(domainConfig, "server_name static.example.test;") != 1 {
		t.Fatalf("expected one http server for domain, got config:\n%s", domainConfig)
	}
	for _, want := range []string{
		"location / {",
		"location = /docs {",
		"return 301 /docs/;",
		"location /docs/ {",
		"location /old {",
		"return 301 https://static.example.test/new;",
		"location /backend {",
		"proxy_pass http://10.0.0.10:9000;",
		"proxy_set_header Host $proxy_host;",
		"proxy_set_header X-Real-IP \"\";",
		"location /upstream {",
		"proxy_pass https://upstream.example.test;",
		"proxy_set_header Host $host;",
		"proxy_http_version 1.1;",
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header Connection $connection_upgrade;",
		"location /api {",
		"proxy_pass http://host.docker.internal:8080;",
		"proxy_read_timeout 45s;",
		"proxy_hide_header X-App-Route;",
		`add_header X-App-Route "enabled" always;`,
		"location /app {",
		"proxy_pass http://panel-website:9000;",
	} {
		if !strings.Contains(domainConfig, want) {
			t.Fatalf("expected nginx config to contain %q, got:\n%s", want, domainConfig)
		}
	}
	if len(mounts) != 2 {
		t.Fatalf("expected two static mounts, got %d", len(mounts))
	}
	if got := strings.Count(domainConfig, "proxy_cache off;"); got != 4 {
		t.Fatalf("proxy_cache off count = %d, want 4; config:\n%s", got, domainConfig)
	}
}

func TestRenderNginxConfigWritesAdvancedRouteOptions(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		StaticSites: []StaticSite{{
			Domain:          "proxy.example.test",
			Path:            "/api",
			RuleType:        StaticRuleProxyPass,
			ProxyURL:        "http://10.0.0.10:9000",
			ProxySourceMode: ProxySourcePreserve,
			Options: applications.HTTPRouteOptions{
				GzipMode:              applications.HTTPRouteModeOn,
				ClientMaxBodySizeMB:   25,
				ConnectTimeoutSeconds: 15,
				ReadTimeoutSeconds:    120,
				SendTimeoutSeconds:    90,
				BufferingMode:         applications.HTTPRouteModeOff,
				WebSocketMode:         applications.HTTPRouteModeOn,
				RequestHeaders:        []applications.HTTPHeader{{Name: "X-Panel-Request", Value: "edge"}},
				ResponseHeaders:       []applications.HTTPHeader{{Name: "X-Panel-Response", Value: "ready"}},
			},
		}},
		DomainPolicies: []DomainPolicy{{Domain: "proxy.example.test", EntryServerIDs: []string{"srv-edge"}, Strategy: DomainStrategyRoundRobin}},
	}

	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-edge", cfg, nil, nil)
	if err != nil {
		t.Fatalf("render nginx config: %v", err)
	}
	domainConfig := managedFileContent(files, "conf.d/proxy.example.test.conf")
	for _, want := range []string{
		"gzip on;",
		"proxy_cache off;",
		"client_max_body_size 25m;",
		"proxy_connect_timeout 15s;",
		"proxy_read_timeout 120s;",
		"proxy_send_timeout 90s;",
		"proxy_buffering off;",
		`proxy_set_header X-Panel-Request "edge";`,
		"proxy_hide_header X-Panel-Response;",
		`add_header X-Panel-Response "ready" always;`,
		`proxy_set_header Connection "upgrade";`,
	} {
		if !strings.Contains(domainConfig, want) {
			t.Fatalf("expected advanced route config to contain %q, got:\n%s", want, domainConfig)
		}
	}
}

func TestRenderNginxConfigSeparatesUpstreamAndRelayNodes(t *testing.T) {
	servers := facilityTestServers{items: map[string]server.Server{
		"srv-origin-a": {ID: "srv-origin-a", Host: "10.0.0.11"},
		"srv-origin-b": {ID: "srv-origin-b", Host: "10.0.0.12"},
		"srv-relay":    {ID: "srv-relay", Host: "10.0.0.20"},
	}}
	svc := &Service{servers: servers}
	base := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-origin-a", "srv-origin-b", "srv-relay"},
		StaticSites: []StaticSite{{
			Domain:            "relay.example.test",
			Path:              "/content",
			RuleType:          StaticRuleStatic,
			SourceType:        StaticSourceHostPath,
			RootPath:          "/srv/www",
			DeploymentServers: []string{"srv-origin-a", "srv-origin-b"},
		}},
	}

	tests := []struct {
		name            string
		strategy        string
		primaryServerID string
		want            []string
		notWant         []string
	}{
		{name: "round robin", strategy: DomainStrategyRoundRobin, want: []string{"server 10.0.0.11:80 max_fails=3 fail_timeout=30s;", "server 10.0.0.12:80 max_fails=3 fail_timeout=30s;"}, notWant: []string{"ip_hash;", " backup;"}},
		{name: "primary backup", strategy: DomainStrategyPrimaryBackup, primaryServerID: "srv-origin-a", want: []string{"server 10.0.0.11:80 max_fails=3 fail_timeout=30s;", "server 10.0.0.12:80 max_fails=3 fail_timeout=30s backup;"}},
		{name: "client ip hash", strategy: DomainStrategyIPHash, want: []string{"ip_hash;"}, notWant: []string{" backup;"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.DomainPolicies = []DomainPolicy{{Domain: "relay.example.test", EntryServerIDs: []string{"srv-origin-a", "srv-origin-b"}, UpstreamMode: true, Strategy: tt.strategy, PrimaryServerID: tt.primaryServerID}}
			_, originMounts, originFiles, err := svc.renderNginxConfig(context.Background(), "srv-origin-a", cfg, nil, nil)
			if err != nil {
				t.Fatalf("render origin config: %v", err)
			}
			originConfig := managedFileContent(originFiles, "conf.d/relay.example.test.conf")
			if !strings.Contains(originConfig, "location /content/") || strings.Contains(originConfig, "upstream panel_domain_relay.example.test") {
				t.Fatalf("origin node should retain local routes only, config:\n%s", originConfig)
			}
			if len(originMounts) != 1 {
				t.Fatalf("origin mounts = %#v, want local static mount", originMounts)
			}

			_, relayMounts, relayFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay", cfg, nil, nil)
			if err != nil {
				t.Fatalf("render relay config: %v", err)
			}
			relayConfig := managedFileContent(relayFiles, "conf.d/relay.example.test.conf")
			for _, want := range append([]string{"upstream panel_domain_relay.example.test", "location / {", "proxy_pass http://panel_domain_relay.example.test;", "proxy_cache off;", "proxy_redirect off;"}, tt.want...) {
				if !strings.Contains(relayConfig, want) {
					t.Fatalf("relay config missing %q:\n%s", want, relayConfig)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(relayConfig, notWant) {
					t.Fatalf("relay config unexpectedly contains %q:\n%s", notWant, relayConfig)
				}
			}
			if strings.Contains(relayConfig, "alias /srv/panel-static") || len(relayMounts) != 0 {
				t.Fatalf("relay node should not mount or serve local static content, mounts=%#v config:\n%s", relayMounts, relayConfig)
			}
		})
	}
}

func TestRenderNginxConfigUsesVerifiedHTTPSBetweenRelayAndUpstream(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{
		"srv-origin": {ID: "srv-origin", Host: "2001:db8::10"},
		"srv-relay":  {ID: "srv-relay", Host: "10.0.0.20"},
	}}}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-origin", "srv-relay"},
		StaticSites:       []StaticSite{{Domain: "secure-relay.example.test", Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://example.test", RedirectCode: 302, DeploymentServers: []string{"srv-origin"}}},
		DomainPolicies:    []DomainPolicy{{Domain: "secure-relay.example.test", EntryServerIDs: []string{"srv-origin"}, UpstreamMode: true, Strategy: DomainStrategyRoundRobin}},
	}
	certificates := []proxycert.Certificate{{ID: "cert-relay", Domains: []string{"secure-relay.example.test"}, CertificatePEM: "CERT", PrivateKeyPEM: "KEY"}}

	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-relay", cfg, nil, certificates)
	if err != nil {
		t.Fatalf("render HTTPS relay config: %v", err)
	}
	domainConfig := managedFileContent(files, "conf.d/secure-relay.example.test.conf")
	for _, want := range []string{
		"server [2001:db8::10]:443 max_fails=3 fail_timeout=30s;",
		"proxy_pass https://panel_domain_secure-relay.example.test;",
		"proxy_ssl_server_name on;",
		"proxy_ssl_name secure-relay.example.test;",
		"proxy_ssl_trusted_certificate /etc/nginx/panel-certs/cert-relay/certificate.pem;",
		"proxy_ssl_verify on;",
	} {
		if !strings.Contains(domainConfig, want) {
			t.Fatalf("HTTPS relay config missing %q:\n%s", want, domainConfig)
		}
	}
	if got := strings.Count(domainConfig, "proxy_pass https://panel_domain_secure-relay.example.test;"); got != 2 {
		t.Fatalf("HTTPS relay proxy_pass count = %d, want both HTTP and HTTPS listeners to use verified HTTPS upstream:\n%s", got, domainConfig)
	}
	if strings.Contains(domainConfig, "proxy_pass http://panel_domain_secure-relay.example.test;") {
		t.Fatalf("certificate-backed relay must not use HTTP between gateway nodes:\n%s", domainConfig)
	}
}

func TestProxySpecUsesBridgeNetworkWhenApplicationRouteTargetsContainer(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{ID: ReverseProxyID, Image: defaultProxyImage, DeploymentServers: []string{"srv-edge"}}
	apps := []applications.ApplicationReverseProxyConfig{
		{
			Routes: []applications.ReverseProxyRoute{
				{Domain: "app.example.test", TargetType: applications.ReverseProxyTargetContainer, TargetPort: 80, TargetContainer: "panel-website", Paths: []applications.ReverseProxyPath{{Path: "/"}}},
			},
		},
	}

	spec, err := svc.proxySpec(context.Background(), "srv-edge", cfg, apps, nil)
	if err != nil {
		t.Fatalf("proxy spec: %v", err)
	}
	if spec.NetworkMode != "bridge" {
		t.Fatalf("network mode = %q, want bridge", spec.NetworkMode)
	}
	if !hasPort(spec.Ports, 80, 80) || !hasPort(spec.Ports, 443, 443) {
		t.Fatalf("expected 80/443 port bindings, ports=%#v", spec.Ports)
	}
}

func TestRuntimeSpecUsesManagedProxyIdentity(t *testing.T) {
	svc, closeStore := newFacilityTestServiceWithAgent(t, &facilityTestAgent{})
	defer closeStore()

	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
	}
	if _, _, err := svc.ensureReverseProxyApplication(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	app := applications.Application{ID: proxyApplicationID, Generation: 1, SpecHash: facilityConfigHash(cfg)}
	spec, ok, err := svc.RuntimeSpecForServer(context.Background(), app, readyFacilityServer("srv-edge"))
	if err != nil || !ok {
		t.Fatalf("runtime spec ok=%v err=%v", ok, err)
	}
	if spec.ApplicationID != proxyApplicationID || spec.InstanceID != instanceID("srv-edge") || spec.ContainerName != proxyContainerName {
		t.Fatalf("runtime identity = %#v", spec)
	}
}

func TestProxySpecMountsPerDomainNginxConfigDirectory(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/", RootPath: "/srv/www/root", SourceType: StaticSourceHostPath},
		},
	}

	spec, err := svc.proxySpec(context.Background(), "srv-edge", cfg, nil, nil)
	if err != nil {
		t.Fatalf("proxy spec: %v", err)
	}
	if managedFileContent(spec.Files, "nginx.conf") == "" {
		t.Fatalf("expected main nginx config file, files=%#v", spec.Files)
	}
	if managedFileContent(spec.Files, "conf.d/static.example.test.conf") == "" {
		t.Fatalf("expected per-domain nginx config file, files=%#v", spec.Files)
	}
	if !hasMount(spec.Mounts, "nginx.conf", "/etc/nginx/nginx.conf") {
		t.Fatalf("expected main nginx config mount, mounts=%#v", spec.Mounts)
	}
	if !hasMount(spec.Mounts, "conf.d", "/etc/nginx/conf.d") {
		t.Fatalf("expected nginx conf.d directory mount, mounts=%#v", spec.Mounts)
	}
}

func TestRenderNginxConfigWritesHttpsQuicServerWhenCertificateMatches(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		StaticSites: []StaticSite{
			{Domain: "secure.example.test", Path: "/", RootPath: "/srv/www/root", SourceType: StaticSourceHostPath},
		},
	}
	certificates := []proxycert.Certificate{
		{ID: "cert-1", Domains: []string{"secure.example.test"}, CertificatePEM: "CERT", PrivateKeyPEM: "KEY"},
	}

	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-edge", cfg, nil, certificates)
	if err != nil {
		t.Fatalf("render nginx config: %v", err)
	}
	domainConfig := managedFileContent(files, "conf.d/secure.example.test.conf")
	for _, want := range []string{
		"listen 443 ssl;",
		"listen 443 quic;",
		"ssl_certificate /etc/nginx/panel-certs/cert-1/certificate.pem;",
		"ssl_certificate_key /etc/nginx/panel-certs/cert-1/private-key.pem;",
		"add_header Alt-Svc 'h3=\":443\"; ma=86400' always;",
	} {
		if !strings.Contains(domainConfig, want) {
			t.Fatalf("expected HTTPS domain config to contain %q, got:\n%s", want, domainConfig)
		}
	}
	if managedFileContent(files, "certs/cert-1/certificate.pem") != "CERT" {
		t.Fatalf("expected certificate managed file, files=%#v", files)
	}
}

func TestRenderNginxConfigWritesPanelEntryOnSelectedGatewayNode(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge", "srv-other"},
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-edge", Domain: "panel.example.test"},
	}

	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-edge", cfg, nil, nil)
	if err != nil {
		t.Fatalf("render nginx config: %v", err)
	}
	domainConfig := managedFileContent(files, "conf.d/panel.example.test.conf")
	for _, want := range []string{
		"server_name panel.example.test;",
		"location / {",
		"proxy_pass http://127.0.0.1:8080;",
		"proxy_set_header Host $host;",
		"proxy_cache off;",
	} {
		if !strings.Contains(domainConfig, want) {
			t.Fatalf("expected Panel entry config to contain %q, got:\n%s", want, domainConfig)
		}
	}

	_, _, otherFiles, err := svc.renderNginxConfig(context.Background(), "srv-other", cfg, nil, nil)
	if err != nil {
		t.Fatalf("render nginx config for other server: %v", err)
	}
	if got := managedFileContent(otherFiles, "conf.d/panel.example.test.conf"); got != "" {
		t.Fatalf("did not expect Panel entry on non-host gateway node, got:\n%s", got)
	}
}

func managedFileContent(files []appruntime.ManagedFile, path string) string {
	for _, file := range files {
		if file.Path == path {
			return string(file.Content)
		}
	}
	return ""
}

func hasMount(mounts []appruntime.Mount, source, target string) bool {
	for _, mount := range mounts {
		if mount.Source == source && mount.Target == target {
			return true
		}
	}
	return false
}

func hasPort(ports []appruntime.Port, containerPort, hostPort int) bool {
	for _, port := range ports {
		if port.ContainerPort == containerPort && port.HostPort == hostPort {
			return true
		}
	}
	return false
}

func TestNormalizeInputRejectsDuplicateDomainPath(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		DomainPolicies:    []DomainPolicy{{Domain: "static.example.test", EntryServerIDs: []string{"srv-edge"}}},
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/docs", RootPath: "/srv/www/docs", SourceType: StaticSourceHostPath},
			{Domain: "static.example.test", Path: "/docs", RuleType: StaticRuleRedirect, RedirectURL: "https://static.example.test/new", RedirectCode: 302},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate domain/path validation error")
	}
}

func TestNormalizeInputRequiresExplicitDomainGatewayNodes(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		StaticSites: []StaticSite{{
			Domain:     "static.example.test",
			Path:       "/",
			RuleType:   StaticRuleStatic,
			SourceType: StaticSourceHostPath,
			RootPath:   "/srv/www",
		}},
	})
	if !isPanelValidation(err, "facility_domain_servers_required") {
		t.Fatalf("normalize input error = %v, want facility_domain_servers_required", err)
	}
}

func TestNormalizeStoredDomainPoliciesExpandsLegacyEmptyNodes(t *testing.T) {
	policies := normalizeStoredDomainPolicies(nil, []StaticSite{{Domain: "legacy.example.test", Path: "/"}}, []string{"srv-a", "srv-b"})
	if len(policies) != 1 || strings.Join(policies[0].EntryServerIDs, ",") != "srv-a,srv-b" {
		t.Fatalf("legacy policies = %#v, want both gateway nodes", policies)
	}
}

func TestValidateRouteConflictsReservesUpstreamModeDomainForFacility(t *testing.T) {
	svc := &Service{apps: facilityTestApplications{items: []applications.ApplicationReverseProxyConfig{
		{
			ApplicationID:     "app-1",
			ApplicationName:   "web",
			DeploymentMode:    applications.DeploymentModeSelected,
			DeploymentServers: []string{"srv-edge"},
			Routes: []applications.ReverseProxyRoute{{
				Domain:     "UPSTREAM.EXAMPLE.TEST",
				TargetPort: 8080,
				Paths:      []applications.ReverseProxyPath{{Path: "/other"}},
			}},
		},
	}}}
	cfg := ReverseProxyConfig{
		DeploymentServers: []string{"srv-edge", "srv-relay"},
		StaticSites:       []StaticSite{{Domain: "upstream.example.test", Path: "/", DeploymentServers: []string{"srv-edge"}}},
		DomainPolicies:    []DomainPolicy{{Domain: "upstream.example.test", EntryServerIDs: []string{"srv-edge"}, UpstreamMode: true, Strategy: DomainStrategyRoundRobin}},
	}

	err := svc.validateRouteConflicts(context.Background(), cfg)
	if !isPanelConflict(err, "facility_upstream_domain_application_conflict") {
		t.Fatalf("route conflict error = %v, want facility_upstream_domain_application_conflict", err)
	}
}

func TestNormalizeInputRequiresPanelEntryServerToBeGatewayNode(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-other", Domain: "panel.example.test"},
	})
	if err == nil {
		t.Fatal("expected panel entry server validation error")
	}
}

func TestNormalizeInputRejectsPanelEntryStaticRootConflict(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-edge", Domain: "panel.example.test"},
		DomainPolicies:    []DomainPolicy{{Domain: "panel.example.test", EntryServerIDs: []string{"srv-edge"}}},
		StaticSites: []StaticSite{
			{Domain: "panel.example.test", Path: "/", RootPath: "/srv/www", SourceType: StaticSourceHostPath},
		},
	})
	if err == nil {
		t.Fatal("expected panel entry static route conflict")
	}
}

func TestSaveReverseProxyReturnsSavedConfigWhenReconcileFails(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, errors.New("pull failed"))
	defer closeStore()

	cfg, err := svc.SaveReverseProxy(context.Background(), ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-edge", Domain: "panel.example.test"},
		DomainPolicies:    []DomainPolicy{{Domain: "static.example.test", EntryServerIDs: []string{"srv-edge"}}},
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceHostPath, RootPath: "/srv/www"},
		},
	})
	if err != nil {
		t.Fatalf("save reverse proxy: %v", err)
	}
	if len(cfg.StaticSites) != 1 {
		t.Fatalf("static sites = %#v", cfg.StaticSites)
	}
	if cfg.StaticSites[0].Domain != "static.example.test" || cfg.StaticSites[0].RootPath != "/srv/www" {
		t.Fatalf("saved static site = %#v", cfg.StaticSites[0])
	}
	if !cfg.PanelEntry.Enabled || cfg.PanelEntry.Domain != "panel.example.test" {
		t.Fatalf("saved panel entry = %#v", cfg.PanelEntry)
	}
	if !strings.Contains(cfg.LastError, "pull failed") {
		t.Fatalf("last error = %q", cfg.LastError)
	}
}

func TestSaveReverseProxyClearsPreviousLastError(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	ctx := context.Background()

	if err := svc.saveConfig(ctx, ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		LastError:         "select at least one deployment server",
	}); err != nil {
		t.Fatalf("seed reverse proxy config: %v", err)
	}
	if err := svc.setLastError(ctx, "select at least one deployment server"); err != nil {
		t.Fatalf("seed last error: %v", err)
	}

	cfg, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		DomainPolicies:    []DomainPolicy{{Domain: "static.example.test", EntryServerIDs: []string{"srv-edge"}}},
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceHostPath, RootPath: "/srv/www"},
		},
	})
	if err != nil {
		t.Fatalf("save reverse proxy: %v", err)
	}
	if cfg.LastError != "" {
		t.Fatalf("last error = %q, want cleared", cfg.LastError)
	}
}

func TestSaveReverseProxyForcesReconcile(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	reconciler, ok := svc.reconciler.(*facilityTestReconciler)
	if !ok {
		t.Fatal("expected facility test reconciler")
	}

	if _, err := svc.SaveReverseProxy(context.Background(), ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
	}); err != nil {
		t.Fatalf("save reverse proxy: %v", err)
	}

	payload, ok := reconciler.lastTrigger.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", reconciler.lastTrigger.Payload)
	}
	force, _ := payload["force"].(bool)
	if !force {
		t.Fatalf("expected force reconcile payload, got %#v", payload)
	}
}

func TestUploadStaticAssetUploadedFilePersistsDeployableContent(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()

	asset, err := svc.UploadStaticAsset(context.Background(), StaticAssetUploadInput{
		Name:     "Home page",
		Kind:     StaticSourceUploadedFile,
		FileName: "../index.html",
		Content:  []byte("<h1>Hello</h1>"),
	})
	if err != nil {
		t.Fatalf("upload static file: %v", err)
	}
	if asset.Kind != StaticSourceUploadedFile || asset.Filename != "index.html" || asset.Size != int64(len("<h1>Hello</h1>")) {
		t.Fatalf("asset metadata = %#v", asset)
	}

	files, err := svc.staticAssetFiles(asset.ID)
	if err != nil {
		t.Fatalf("read static asset files: %v", err)
	}
	if len(files) != 1 || files[0].Path != "index.html" || string(files[0].Content) != "<h1>Hello</h1>" {
		t.Fatalf("deployable files = %#v", files)
	}
	if _, err := os.Stat(filepath.Join(svc.staticAssetContentDir(asset.ID), "index.html")); err != nil {
		t.Fatalf("uploaded file was not saved under content dir: %v", err)
	}

	assets, err := svc.ListStaticAssets(context.Background())
	if err != nil {
		t.Fatalf("list static assets: %v", err)
	}
	if len(assets) != 1 || assets[0].ID != asset.ID {
		t.Fatalf("listed assets = %#v, want uploaded asset", assets)
	}
}

func TestUploadStaticAssetBundleRejectsTraversalAndEmptyArchives(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  []byte
	}{
		{name: "zip traversal", filename: "site.zip", content: zipArchive(t, map[string]string{"../escape.txt": "nope"})},
		{name: "tar traversal", filename: "site.tar", content: tarArchive(t, map[string]string{"../escape.txt": "nope"})},
		{name: "tar gzip traversal", filename: "site.tar.gz", content: gzipBytes(t, tarArchive(t, map[string]string{"../escape.txt": "nope"}))},
		{name: "empty zip", filename: "empty.zip", content: zipArchive(t, nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, closeStore := newFacilityTestService(t, nil)
			defer closeStore()

			_, err := svc.UploadStaticAsset(context.Background(), StaticAssetUploadInput{
				Name:     tt.name,
				Kind:     StaticSourceUploadedBundle,
				FileName: tt.filename,
				Content:  tt.content,
			})
			if err == nil {
				t.Fatal("expected bundle upload to reject unsafe or empty archive")
			}
			assets, listErr := svc.ListStaticAssets(context.Background())
			if listErr != nil {
				t.Fatalf("list static assets: %v", listErr)
			}
			if len(assets) != 0 {
				t.Fatalf("rejected upload left persisted assets: %#v", assets)
			}
		})
	}
}

func TestDeleteStaticAssetReturnsConflictWhenReferencedByConfig(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	ctx := context.Background()

	asset, err := svc.UploadStaticAsset(ctx, StaticAssetUploadInput{
		Name:     "Referenced bundle",
		Kind:     StaticSourceUploadedBundle,
		FileName: "site.zip",
		Content:  zipArchive(t, map[string]string{"index.html": "ok"}),
	})
	if err != nil {
		t.Fatalf("upload static bundle: %v", err)
	}
	if err := svc.saveConfig(ctx, ReverseProxyConfig{
		ID:                ReverseProxyID,
		Image:             defaultProxyImage,
		DeploymentServers: []string{"srv-edge"},
		StaticSites: []StaticSite{{
			Domain:     "static.example.test",
			Path:       "/",
			RuleType:   StaticRuleStatic,
			SourceType: StaticSourceUploadedBundle,
			AssetID:    asset.ID,
		}},
	}); err != nil {
		t.Fatalf("save reverse proxy config: %v", err)
	}

	err = svc.DeleteStaticAsset(ctx, asset.ID)
	if !isPanelConflict(err, "facility_static_asset_in_use") {
		t.Fatalf("delete referenced asset error = %v, want conflict", err)
	}
	if _, err := svc.getStaticAsset(ctx, asset.ID); err != nil {
		t.Fatalf("referenced asset should remain after failed delete: %v", err)
	}
}

func TestFacilitySaveSessionCommitsStagedAssetsAndConfiguration(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	ctx := context.Background()

	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{})
	if err != nil {
		t.Fatalf("begin save session: %v", err)
	}
	asset, err := svc.UploadSaveSessionAsset(ctx, session.ID, StaticAssetUploadInput{
		Name:     "Home page",
		Kind:     StaticSourceUploadedFile,
		FileName: "index.html",
		Content:  []byte("<h1>saved</h1>"),
	})
	if err != nil {
		t.Fatalf("upload save session asset: %v", err)
	}
	result, err := svc.CommitSaveSession(ctx, session.ID, CommitSaveSessionInput{Save: ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		DomainPolicies:    []DomainPolicy{{Domain: "static.example.test", EntryServerIDs: []string{"srv-edge"}}},
		StaticSites: []StaticSite{{
			Domain:     "static.example.test",
			Path:       "/",
			RuleType:   StaticRuleStatic,
			SourceType: StaticSourceUploadedFile,
			AssetID:    asset.ID,
		}},
	}})
	if err != nil {
		t.Fatalf("commit save session: %v", err)
	}
	if !result.ApplyRequested || len(result.Config.StaticSites) != 1 || result.Config.StaticSites[0].AssetID != asset.ID {
		t.Fatalf("commit result = %#v", result)
	}
	content, err := os.ReadFile(filepath.Join(svc.staticAssetContentDir(asset.ID), "index.html"))
	if err != nil || string(content) != "<h1>saved</h1>" {
		t.Fatalf("committed asset content = %q err=%v", content, err)
	}
	if _, err := svc.getSaveSession(session.ID); err == nil {
		t.Fatal("committed save session should be discarded")
	}
}

func TestFacilitySaveSessionDiscardLeavesConfigurationUnchanged(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	ctx := context.Background()

	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{})
	if err != nil {
		t.Fatalf("begin save session: %v", err)
	}
	if _, err := svc.UploadSaveSessionAsset(ctx, session.ID, StaticAssetUploadInput{Name: "Draft", Kind: StaticSourceUploadedFile, FileName: "draft.txt", Content: []byte("draft")}); err != nil {
		t.Fatalf("upload draft asset: %v", err)
	}
	internalSession, err := svc.getSaveSession(session.ID)
	if err != nil {
		t.Fatalf("get save session: %v", err)
	}
	sessionDir := internalSession.Dir
	svc.DiscardSaveSession(session.ID)
	if _, err := os.Stat(sessionDir); err == nil {
		t.Fatal("discarded save session directory should not remain")
	}
	assets, err := svc.ListStaticAssets(ctx)
	if err != nil || len(assets) != 0 {
		t.Fatalf("persistent assets after discard = %#v err=%v", assets, err)
	}
	cfg, err := svc.GetReverseProxy(ctx)
	if err != nil || len(cfg.StaticSites) != 0 {
		t.Fatalf("configuration after discard = %#v err=%v", cfg, err)
	}
}

func TestFacilitySaveSessionRejectsStaleConfigurationVersion(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, nil)
	defer closeStore()
	ctx := context.Background()

	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{})
	if err != nil {
		t.Fatalf("begin save session: %v", err)
	}
	if err := svc.saveConfig(ctx, ReverseProxyConfig{ID: ReverseProxyID, Image: defaultProxyImage, DeploymentServers: []string{"srv-edge"}}); err != nil {
		t.Fatalf("change reverse proxy config: %v", err)
	}
	_, err = svc.CommitSaveSession(ctx, session.ID, CommitSaveSessionInput{Save: ReverseProxySaveInput{DeploymentServers: []string{"srv-edge"}, Image: defaultProxyImage}})
	if !isPanelConflict(err, "facility_reverse_proxy_config_changed") {
		t.Fatalf("stale commit error = %v, want facility_reverse_proxy_config_changed", err)
	}
}

func newFacilityTestService(t *testing.T, agentErr error) (*Service, func()) {
	return newFacilityTestServiceWithAgent(t, &facilityTestAgent{err: agentErr})
}

func newFacilityTestServiceWithAgent(t *testing.T, agent *facilityTestAgent) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"cred-1", "credential", "password", "root", now, now); err != nil {
		t.Fatal(err)
	}
	traits := map[string]string{
		agentcontract.TraitURL:    "https://srv-edge.agent",
		agentcontract.TraitStatus: agentcontract.StatusCompatible,
	}
	rawTraits, _ := json.Marshal(traits)
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"srv-edge", "Edge", "127.0.0.1", 22, "root", "cred-1", agentcontract.DefaultDockerHost, string(rawTraits), "{}", now, now); err != nil {
		t.Fatal(err)
	}
	provider := facilityTestServers{items: map[string]server.Server{
		"srv-edge": {
			ID:          "srv-edge",
			Name:        "Edge",
			Host:        "127.0.0.1",
			Port:        22,
			SSHUsername: "root",
			DockerHost:  agentcontract.DefaultDockerHost,
			Traits:      traits,
		},
	}}
	reconciler := &facilityTestReconciler{err: agent.err}
	svc := NewService(store.AppDB(), agent, provider, nil, WithDataRoot(cfg.DataRoot), WithApplicationReconcileTrigger(reconciler))
	reconciler.svc = svc
	return svc, func() { _ = store.Close() }
}

type facilityTestReconciler struct {
	svc         *Service
	err         error
	lastTrigger tasks.PeriodicTrigger
}

func (r *facilityTestReconciler) TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	r.lastTrigger = trigger
	if r.err != nil {
		return tasks.Task{}, true, r.err
	}
	return tasks.Task{}, true, nil
}

type facilityTestServers struct {
	items map[string]server.Server
}

type facilityTestApplications struct {
	items []applications.ApplicationReverseProxyConfig
}

func (p facilityTestApplications) ApplicationReverseProxyConfigs(context.Context) ([]applications.ApplicationReverseProxyConfig, error) {
	return p.items, nil
}

func (p facilityTestServers) List(context.Context) ([]server.Server, error) {
	out := make([]server.Server, 0, len(p.items))
	for _, item := range p.items {
		out = append(out, item)
	}
	return out, nil
}

func (p facilityTestServers) Get(_ context.Context, id string) (server.Server, error) {
	item, ok := p.items[id]
	if !ok {
		return server.Server{}, errors.New("server not found")
	}
	return item, nil
}

func readyFacilityServer(id string) server.Server {
	return server.Server{
		ID:          id,
		Name:        id,
		Host:        "127.0.0.1",
		Port:        22,
		SSHUsername: "root",
		DockerHost:  agentcontract.DefaultDockerHost,
		Traits: map[string]string{
			agentcontract.TraitURL:    "https://" + id + ".agent",
			agentcontract.TraitStatus: agentcontract.StatusCompatible,
		},
	}
}

type facilityTestAgent struct {
	err   error
	stops []agentcontract.RuntimeStopRequest
}

func (a *facilityTestAgent) RuntimeWriteFiles(context.Context, string, agentcontract.RuntimeWriteFilesRequest) error {
	return a.err
}

func (a *facilityTestAgent) RuntimeCreateContainer(context.Context, string, agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	return agentcontract.RuntimeCreateContainerResponse{}, a.err
}

func (a *facilityTestAgent) RuntimeStop(_ context.Context, _ string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	a.stops = append(a.stops, req)
	return agentcontract.RuntimeInstanceResponse{}, nil
}

func (a *facilityTestAgent) DockerImagePull(context.Context, string, string) error {
	return a.err
}

func (a *facilityTestAgent) DockerContainerAction(context.Context, string, string, string) error {
	return a.err
}

func zipArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip archive: %v", err)
	}
	return buf.Bytes()
}

func tarArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar entry: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar archive: %v", err)
	}
	return buf.Bytes()
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(content); err != nil {
		t.Fatalf("write gzip content: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip content: %v", err)
	}
	return buf.Bytes()
}

func isPanelConflict(err error, code string) bool {
	var panel *panelerr.Error
	return errors.As(err, &panel) && panel.HTTPStatus == 409 && panel.Code == code
}

func isPanelValidation(err error, code string) bool {
	var panel *panelerr.Error
	return errors.As(err, &panel) && panel.HTTPStatus == 422 && panel.Code == code
}
