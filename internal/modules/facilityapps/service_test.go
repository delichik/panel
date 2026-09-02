package facilityapps

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
)

// TestNormalizeInputRejectsHostPathSource 回归测试：host_path 静态站点来源已
// 整体移除（前端不再提供、后端不再渲染），保存时必须被拒绝并返回
// facility_static_site_source_invalid，而不是继续生成宿主机目录 bind mount。
func TestNormalizeInputRejectsHostPathSource(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a"},
		Domains: []FacilityRouteDomain{{
			Domain:          "static.example.test",
			OriginServerIDs: []string{"srv-a"},
			Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleStatic, SourceType: "host_path"}},
		}},
	})
	if err == nil {
		t.Fatal("expected host_path source to be rejected")
	}
	var pe *panelerr.Error
	if !errors.As(err, &pe) || pe.Code != "facility_static_site_source_invalid" {
		t.Fatalf("error = %v, want facility_static_site_source_invalid", err)
	}
}

func TestNormalizeInputUsesDomainOriginsAndAnyAccess(t *testing.T) {
	cfg, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a", "srv-b"},
		Domains: []FacilityRouteDomain{{
			Domain:          "EXAMPLE.TEST",
			OriginServerIDs: []string{"srv-a"},
			AnyAccess:       applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyPrimaryBackup, PrimaryOriginServerID: "srv-a"},
			Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test", RedirectCode: 302}},
		}},
	})
	if err != nil {
		t.Fatalf("normalize input: %v", err)
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0].Domain != "example.test" {
		t.Fatalf("domains = %#v", cfg.Domains)
	}
	if !cfg.Domains[0].AnyAccess.Enabled || cfg.Domains[0].AnyAccess.PrimaryOriginServerID != "srv-a" {
		t.Fatalf("any access = %#v", cfg.Domains[0].AnyAccess)
	}
}

func TestBestCertificateWildcardMatchesOnlyOneLabel(t *testing.T) {
	certs := []proxycert.Certificate{{
		ID:      "wildcard",
		Domains: []string{"*.test.com"},
	}}
	if cert := bestCertificate("a.test.com", certs); cert == nil || cert.ID != "wildcard" {
		t.Fatalf("expected wildcard certificate for one-label subdomain, got %#v", cert)
	}
	if cert := bestCertificate("test.com", certs); cert != nil {
		t.Fatalf("expected root domain not to match wildcard certificate, got %#v", cert)
	}
	if cert := bestCertificate("a.b.test.com", certs); cert != nil {
		t.Fatalf("expected multi-label subdomain not to match wildcard certificate, got %#v", cert)
	}
}

func TestNormalizeInputRejectsInvalidOriginsAndDuplicatePaths(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-b"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
	}}})
	if err == nil {
		t.Fatal("expected invalid origin error")
	}
	_, err = normalizeInput(ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://one.example.test"}, {Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://two.example.test"}},
	}}})
	if err == nil {
		t.Fatal("expected duplicate path error")
	}
}

func TestNormalizeInputRejectsUnsafeRedirectTargets(t *testing.T) {
	for _, target := range []string{
		"https://target.example.test/path#fragment",
		`https://target.example.test/path"quote`,
		"https://target.example.test/path\\backslash",
		"https://target.example.test/path'quote",
	} {
		_, err := normalizeInput(ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: []FacilityRouteDomain{{
			Domain: "example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: target}},
		}}})
		if err == nil || !strings.Contains(err.Error(), "Redirect target is invalid") {
			t.Fatalf("expected redirect target %q to be rejected, got %v", target, err)
		}
	}
	cfg, err := normalizeInput(ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test/ok?q=1&r=2"}},
	}}})
	if err != nil {
		t.Fatalf("expected clean redirect target to pass: %v", err)
	}
	if got := cfg.Domains[0].Paths[0].RedirectURL; got != "https://target.example.test/ok?q=1&r=2" {
		t.Fatalf("redirect url = %q", got)
	}
}
func TestRenderNginxConfigSeparatesOriginAndAnyAccessRelay(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{
		"srv-origin": {ID: "srv-origin", Host: "10.0.0.11"},
		"srv-relay":  {ID: "srv-relay", Host: "10.0.0.12"},
	}}}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-origin", "srv-relay"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-origin"}, AnyAccess: applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin}, Paths: []FacilityRoutePath{{
			Path: "/api", RuleType: StaticRuleProxyPass, ProxyURL: "http://127.0.0.1:8080", ProxySourceMode: ProxySourcePreserve,
		}},
	}}}
	_, _, originFiles, err := svc.renderNginxConfig(context.Background(), "srv-origin", cfg, nil, nil)
	if err != nil {
		t.Fatalf("render origin: %v", err)
	}
	origin := managedConfigText(originFiles)
	if !strings.Contains(origin, "location /api") || !strings.Contains(origin, "proxy_cache off;") || strings.Contains(origin, "upstream panel_domain_example.test") {
		t.Fatalf("unexpected origin config:\n%s", origin)
	}
	_, _, relayFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay", cfg, nil, nil)
	if err != nil {
		t.Fatalf("render relay: %v", err)
	}
	relay := managedConfigText(relayFiles)
	for _, want := range []string{"upstream panel_domain_example.test", "server 10.0.0.11:80 max_fails=3 fail_timeout=30s;", "location /", "proxy_cache off;"} {
		if !strings.Contains(relay, want) {
			t.Fatalf("relay config missing %q:\n%s", want, relay)
		}
	}
}

func TestRenderApplicationAnyAccessUsesOriginAndRelay(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{
		"srv-origin": {ID: "srv-origin", Host: "10.0.0.21"},
		"srv-relay":  {ID: "srv-relay", Host: "10.0.0.22"},
	}}}
	route := applications.ReverseProxyRoute{Domain: "app.example.test", TargetContainer: "panel-app-1", TargetPort: 8080, OriginServerIDs: []string{"srv-origin"}, AnyAccess: applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyIPHash}, Paths: []applications.ReverseProxyPath{{Path: "/"}}}
	app := applications.ApplicationReverseProxyConfig{ApplicationID: "app-1", ApplicationName: "app", Routes: []applications.ReverseProxyRoute{route}}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-origin", "srv-relay"}}
	_, _, originFiles, err := svc.renderNginxConfig(context.Background(), "srv-origin", cfg, []applications.ApplicationReverseProxyConfig{app}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if origin := managedConfigText(originFiles); !strings.Contains(origin, "set $panel_proxy_upstream panel-app-1;") || !strings.Contains(origin, "proxy_pass http://$panel_proxy_upstream:8080;") || !strings.Contains(origin, "proxy_cache off;") {
		t.Fatalf("origin config:\n%s", origin)
	}
	_, _, relayFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay", cfg, []applications.ApplicationReverseProxyConfig{app}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay := managedConfigText(relayFiles); !strings.Contains(relay, "ip_hash;") || !strings.Contains(relay, "server 10.0.0.21:80") {
		t.Fatalf("relay config:\n%s", relay)
	}
}

func TestRenderNginxConfigHonorsSpecifiedRelayServers(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{
		"srv-origin":  {ID: "srv-origin", Host: "10.0.0.31"},
		"srv-relay-1": {ID: "srv-relay-1", Host: "10.0.0.32"},
		"srv-relay-2": {ID: "srv-relay-2", Host: "10.0.0.33"},
	}}}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-origin", "srv-relay-1", "srv-relay-2"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-origin"}, AnyAccess: applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin, RelayServerIDs: []string{"srv-relay-1"}}, Paths: []FacilityRoutePath{{
			Path: "/api", RuleType: StaticRuleProxyPass, ProxyURL: "http://127.0.0.1:8080", ProxySourceMode: ProxySourcePreserve,
		}},
	}}}
	_, _, selectedFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay-1", cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay := managedConfigText(selectedFiles); !strings.Contains(relay, "upstream panel_domain_example.test") || !strings.Contains(relay, "server 10.0.0.31:80") {
		t.Fatalf("selected relay config:\n%s", relay)
	}
	_, _, excludedFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay-2", cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay := managedConfigText(excludedFiles); strings.Contains(relay, "panel_domain_example") {
		t.Fatalf("unselected relay server must not render the domain:\n%s", relay)
	}
}

func TestRenderApplicationAnyAccessHonorsSpecifiedRelayServers(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{
		"srv-origin":  {ID: "srv-origin", Host: "10.0.0.41"},
		"srv-relay-1": {ID: "srv-relay-1", Host: "10.0.0.42"},
		"srv-relay-2": {ID: "srv-relay-2", Host: "10.0.0.43"},
	}}}
	route := applications.ReverseProxyRoute{
		Domain: "app.example.test", TargetContainer: "panel-app-1", TargetPort: 8080,
		OriginServerIDs: []string{"srv-origin"},
		AnyAccess:       applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin, RelayServerIDs: []string{"srv-relay-1"}},
		Paths:           []applications.ReverseProxyPath{{Path: "/"}},
	}
	app := applications.ApplicationReverseProxyConfig{ApplicationID: "app-1", ApplicationName: "app", Routes: []applications.ReverseProxyRoute{route}}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-origin", "srv-relay-1", "srv-relay-2"}}
	_, _, selectedFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay-1", cfg, []applications.ApplicationReverseProxyConfig{app}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay := managedConfigText(selectedFiles); !strings.Contains(relay, "upstream panel_domain_app.example.test") || !strings.Contains(relay, "server 10.0.0.41:80") {
		t.Fatalf("selected relay config:\n%s", relay)
	}
	_, _, excludedFiles, err := svc.renderNginxConfig(context.Background(), "srv-relay-2", cfg, []applications.ApplicationReverseProxyConfig{app}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if relay := managedConfigText(excludedFiles); strings.Contains(relay, "panel_domain_app.example.test") {
		t.Fatalf("unselected relay server must not render the route:\n%s", relay)
	}
}

func TestNormalizeInputRejectsInvalidAnyAccessRelay(t *testing.T) {
	base := ReverseProxySaveInput{DeploymentServers: []string{"srv-a", "srv-b"}, Domains: []FacilityRouteDomain{{
		Domain: "example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
	}}}
	base.Domains[0].AnyAccess = applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin, RelayServerIDs: []string{"srv-outside"}}
	if _, err := normalizeInput(base); err == nil || !strings.Contains(err.Error(), "AnyAccess relay server must be a global gateway node") {
		t.Fatalf("expected non-gateway relay to be rejected, got %v", err)
	}
	base.Domains[0].AnyAccess = applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin, RelayServerIDs: []string{"srv-a"}}
	if _, err := normalizeInput(base); err == nil || !strings.Contains(err.Error(), "AnyAccess relay server cannot be an origin server") {
		t.Fatalf("expected origin relay to be rejected, got %v", err)
	}
}

func TestProxySpecUsesFixedSupportedImage(t *testing.T) {
	svc := &Service{}
	spec, err := svc.proxySpec(context.Background(), "srv-a", ReverseProxyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Image != supportedProxyImage {
		t.Fatalf("image = %q", spec.Image)
	}
	wantPorts := []appruntime.Port{
		{Label: "http", ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
		{Label: "https", ContainerPort: 443, HostPort: 443, Protocol: "tcp"},
	}
	if !reflect.DeepEqual(spec.Ports, wantPorts) {
		t.Fatalf("ports = %#v, want %#v", spec.Ports, wantPorts)
	}
}

func TestRuntimeSpecForServerUsesApplicationSpecHash(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{
		"srv-a": {ID: "srv-a"},
	}}, nil, WithDataRoot(cfg.DataRoot))
	ctx := context.Background()
	app := applications.Application{
		ID:                proxyApplicationID,
		Kind:              applications.ApplicationKindFacility,
		Generation:        7,
		SpecHash:          "application-spec-hash",
		DeploymentMode:    applications.DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	}
	spec, ok, err := svc.RuntimeSpecForServer(ctx, app, server.Server{ID: "srv-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected facility runtime spec")
	}
	if spec.Generation != 7 {
		t.Fatalf("generation = %d, want 7", spec.Generation)
	}
	if spec.SpecHash != "application-spec-hash" {
		t.Fatalf("spec hash = %q, want application-spec-hash so verify/drift checks match the application-level desired hash", spec.SpecHash)
	}
}

func TestProxySpecMountsNginxConfigurationDirectory(t *testing.T) {
	svc := &Service{}
	spec, err := svc.proxySpec(context.Background(), "srv-a", ReverseProxyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundNginxMount := false
	for _, mount := range spec.Mounts {
		if mount.Target == "/etc/nginx" {
			t.Fatalf("nginx image configuration must not be shadowed: %#v", spec.Mounts)
		}
		if mount.Source == proxyConfigRoot && mount.Target == proxyContainerRoot && mount.ReadOnly {
			foundNginxMount = true
		}
		if mount.Source == "certs" || mount.Target == proxyTLSMountRoot {
			t.Fatalf("certificate mount must not be present without TLS certificates: %#v", spec.Mounts)
		}
	}
	if !foundNginxMount {
		t.Fatalf("nginx directory mount missing: %#v", spec.Mounts)
	}
	wantCommand := []string{"nginx", "-c", "/etc/panel-nginx/nginx.conf", "-g", "daemon off;"}
	if !reflect.DeepEqual(spec.Command, wantCommand) {
		t.Fatalf("command = %#v, want %#v", spec.Command, wantCommand)
	}
	mainConfig := string(spec.Files[0].Content)
	for _, want := range []string{"include /etc/nginx/mime.types;", "include /etc/panel-nginx/conf.d/*.conf;"} {
		if !strings.Contains(mainConfig, want) {
			t.Fatalf("main nginx config missing %q:\n%s", want, mainConfig)
		}
	}
}

func TestProxySpecMountsTLSCertificatesOutsideNginxDirectory(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{Domains: []FacilityRouteDomain{{
		Domain:          "example.test",
		OriginServerIDs: []string{"srv-a"},
		Paths: []FacilityRoutePath{{
			Path:        "/",
			RuleType:    StaticRuleRedirect,
			RedirectURL: "https://target.example.test",
		}},
	}}}
	certificates := []proxycert.Certificate{{
		ID:             "example-cert",
		Domains:        []string{"example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}

	spec, err := svc.proxySpec(context.Background(), "srv-a", cfg, nil, certificates)
	if err != nil {
		t.Fatal(err)
	}

	wantMounts := map[string]string{
		proxyConfigRoot: proxyContainerRoot,
		"certs":         proxyTLSMountRoot,
	}
	for source, target := range wantMounts {
		found := false
		for _, mount := range spec.Mounts {
			if mount.Source == source && mount.Target == target && mount.ReadOnly {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("read-only mount %q -> %q missing: %#v", source, target, spec.Mounts)
		}
	}
	for _, mount := range spec.Mounts {
		if mount.Target == "/etc/nginx" || strings.HasPrefix(mount.Target, "/etc/nginx/") {
			t.Fatalf("nginx image configuration must not be shadowed or nested: %#v", spec.Mounts)
		}
	}

	config := managedConfigText(spec.Files)
	for _, want := range []string{
		"ssl_certificate /etc/panel-certs/example-cert/certificate.pem;",
		"ssl_certificate_key /etc/panel-certs/example-cert/private-key.pem;",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(config, "/etc/nginx/panel-certs") {
		t.Fatalf("nginx config still references nested certificate path:\n%s", config)
	}

	wantModes := map[string]string{
		"certs/example-cert/certificate.pem": "0644",
		"certs/example-cert/private-key.pem": "0600",
	}
	for path, mode := range wantModes {
		found := false
		for _, file := range spec.Files {
			if file.Path == path && file.Mode == mode {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("managed certificate file %q with mode %q missing: %#v", path, mode, spec.Files)
		}
	}
}

func TestContainerProxyTargetDefersUpstreamResolution(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-a"}}
	apps := []applications.ApplicationReverseProxyConfig{{Routes: []applications.ReverseProxyRoute{{
		Domain: "app.example.test", TargetContainer: "panel-cpa-private", TargetPort: 8080,
		OriginServerIDs: []string{"srv-a"}, Paths: []applications.ReverseProxyPath{{Path: "/"}},
	}}}}
	mainConfig, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", cfg, apps, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainConfig, "resolver 127.0.0.11 valid=10s ipv6=off;") {
		t.Fatalf("nginx main config missing Docker DNS resolver:\n%s", mainConfig)
	}
	if strings.Contains(mainConfig, "panel_error_language") {
		t.Fatalf("nginx main config must not map error page language after merging pages:\n%s", mainConfig)
	}
	text := managedConfigText(files)
	for _, want := range []string{
		"set $panel_proxy_upstream panel-cpa-private;",
		"proxy_pass http://$panel_proxy_upstream:8080;",
		"error_page 502 504 @panel_upstream_unavailable;",
		"location @panel_upstream_unavailable {",
		"root /etc/panel-nginx/errors;",
		"try_files /upstream-unavailable.html =404;",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "proxy_pass http://panel-cpa-private:8080;") {
		t.Fatalf("nginx config must not resolve container upstream at parse time:\n%s", text)
	}
	page := ""
	for _, file := range files {
		if file.Path == proxyErrorPagePath {
			page = string(file.Content)
		}
	}
	if page == "" {
		t.Fatalf("nginx upstream unavailable pages missing from managed files: %#v", files)
	}
	if page != string(proxyUpstreamUnavailablePageContent()) {
		t.Fatalf("upstream unavailable page missing expected copy:\n%s", page)
	}
	if !strings.Contains(page, "Service temporarily unavailable") || !strings.Contains(page, "\u8bf7\u7a0d\u540e\u91cd\u8bd5") {
		t.Fatalf("upstream unavailable page must contain both languages:\n%s", page)
	}
	for _, want := range []string{
		"Seamark",
		`<symbol id="seamark-icon"`,
		`class="brand-bottom"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("upstream unavailable page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "bg-mark") || strings.Contains(page, "seamark-mark") || strings.Contains(page, "watermark") {
		t.Fatalf("upstream unavailable page must not include a watermark:\n%s", page)
	}
	if strings.Contains(page, "brand-tagline") || strings.Contains(page, "Always online") || strings.Contains(page, "\u59cb\u7ec8\u5728\u822a") {
		t.Fatalf("upstream unavailable page must not show a slogan:\n%s", page)
	}
	if strings.Contains(page, "502") || strings.Contains(page, "nginx") {
		t.Fatalf("upstream unavailable page must not expose technical details:\n%s", page)
	}
}

func TestReverseProxyFacilityPlansReload(t *testing.T) {
	svc := &Service{}
	plan := svc.PlanRuntimeUpdate(context.Background(), applications.Application{ID: proxyApplicationID}, server.Server{}, appruntime.Spec{}, appruntime.Spec{})
	if plan.Mode != appruntime.UpdateModeReload || plan.Strategy == nil || len(plan.Strategy.ValidateCommand) == 0 || len(plan.Strategy.ReloadCommand) == 0 {
		t.Fatalf("reload plan = %#v", plan)
	}
	wantValidate := []string{"nginx", "-t", "-c", "/etc/panel-nginx/nginx.conf"}
	wantReload := []string{"nginx", "-s", "reload", "-c", "/etc/panel-nginx/nginx.conf"}
	if !reflect.DeepEqual(plan.Strategy.ValidateCommand, wantValidate) {
		t.Fatalf("validate command = %#v, want %#v", plan.Strategy.ValidateCommand, wantValidate)
	}
	if !reflect.DeepEqual(plan.Strategy.ReloadCommand, wantReload) {
		t.Fatalf("reload command = %#v, want %#v", plan.Strategy.ReloadCommand, wantReload)
	}
}

func managedConfigText(files []appruntime.ManagedFile) string {
	var out strings.Builder
	for _, file := range files {
		if strings.HasPrefix(file.Path, proxyConfigDir+"/") {
			out.Write(file.Content)
		}
	}
	return out.String()
}

type facilityTestServers struct{ items map[string]server.Server }

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

func TestGetReverseProxyExposesReconcileStopped(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()

	cfg, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReconcileStopped {
		t.Fatal("expected reconcileStopped=false without a stopped marker")
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id, name, spec_yaml, job_id, created_at, updated_at, reconcile_stopped)
		VALUES(?, 'facility-reverse-proxy', 'kind: facility/reverse-proxy', 'facility-reverse-proxy', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, proxyApplicationID); err != nil {
		t.Fatal(err)
	}
	cfg, err = svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ReconcileStopped {
		t.Fatal("expected reconcileStopped=true when applications.reconcile_stopped=1")
	}
}

type facilityTestApplicationRoutesProvider struct {
	configs []applications.ApplicationReverseProxyConfig
}

func (p facilityTestApplicationRoutesProvider) ApplicationReverseProxyConfigs(context.Context) ([]applications.ApplicationReverseProxyConfig, error) {
	return p.configs, nil
}

func TestFacilityConfigHashIncludesApplicationRoutes(t *testing.T) {
	base := ReverseProxyConfig{DeploymentServers: []string{"srv-a"}}
	withRoutes := base
	withRoutes.ApplicationRoutes = []applications.ApplicationReverseProxyConfig{{
		ApplicationID: "app-1",
		Routes: []applications.ReverseProxyRoute{{
			Domain:          "app.example.test",
			TargetPort:      8080,
			OriginServerIDs: []string{"srv-a"},
		}},
	}}
	baseHash := facilityConfigHash(base)
	if facilityConfigHash(withRoutes) == baseHash {
		t.Fatal("facility config hash must include application routes")
	}
	if facilityConfigHash(withRoutes) != facilityConfigHash(withRoutes) {
		t.Fatal("facility config hash must be stable")
	}
}

func TestEnsureReverseProxyApplicationGenerationBumpsOnApplicationRouteChange(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	provider := &facilityTestApplicationRoutesProvider{}
	svc := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{"srv-a": {ID: "srv-a"}}}, provider, WithDataRoot(cfg.DataRoot))
	ctx := context.Background()
	base := ReverseProxyConfig{DeploymentServers: []string{"srv-a"}}
	gen1, hash1, err := svc.ensureReverseProxyApplication(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if gen1 != 1 {
		t.Fatalf("generation = %d, want 1", gen1)
	}
	gen2, hash2, err := svc.ensureReverseProxyApplication(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if gen2 != gen1 || hash2 != hash1 {
		t.Fatalf("unchanged routes must keep generation/hash: generation %d -> %d, hash %q -> %q", gen1, gen2, hash1, hash2)
	}
	provider.configs = []applications.ApplicationReverseProxyConfig{{
		ApplicationID: "app-1",
		Routes: []applications.ReverseProxyRoute{{
			Domain:          "app.example.test",
			TargetPort:      8080,
			OriginServerIDs: []string{"srv-a"},
		}},
	}}
	gen3, hash3, err := svc.ensureReverseProxyApplication(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if gen3 != gen2+1 {
		t.Fatalf("generation = %d, want %d after application route change", gen3, gen2+1)
	}
	if hash3 == hash2 {
		t.Fatal("hash must change when application routes change")
	}
}
