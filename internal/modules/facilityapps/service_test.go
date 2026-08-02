package facilityapps

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
)

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
	route := applications.ReverseProxyRoute{Domain: "app.example.test", TargetType: applications.ReverseProxyTargetLocal, TargetPort: 8080, OriginServerIDs: []string{"srv-origin"}, AnyAccess: applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyIPHash}, Paths: []applications.ReverseProxyPath{{Path: "/"}}}
	app := applications.ApplicationReverseProxyConfig{ApplicationID: "app-1", ApplicationName: "app", Routes: []applications.ReverseProxyRoute{route}}
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-origin", "srv-relay"}}
	_, _, originFiles, err := svc.renderNginxConfig(context.Background(), "srv-origin", cfg, []applications.ApplicationReverseProxyConfig{app}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if origin := managedConfigText(originFiles); !strings.Contains(origin, "proxy_pass http://127.0.0.1:8080") || !strings.Contains(origin, "proxy_cache off;") {
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

func TestProxySpecUsesFixedSupportedImage(t *testing.T) {
	svc := &Service{}
	spec, err := svc.proxySpec(context.Background(), "srv-a", ReverseProxyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Image != supportedProxyImage {
		t.Fatalf("image = %q", spec.Image)
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

func TestPanelEntryUsesHostGatewayInBridgeMode(t *testing.T) {
	svc := &Service{}
	cfg := ReverseProxyConfig{PanelEntry: PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"}}
	apps := []applications.ApplicationReverseProxyConfig{{Routes: []applications.ReverseProxyRoute{{
		Domain: "app.example.test", TargetType: applications.ReverseProxyTargetContainer, TargetContainer: "panel-app", TargetPort: 8080,
		OriginServerIDs: []string{"srv-a"}, Paths: []applications.ReverseProxyPath{{Path: "/"}},
	}}}}
	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", cfg, apps, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text := managedConfigText(files); !strings.Contains(text, "proxy_pass http://host.docker.internal:8080") {
		t.Fatalf("Panel bridge upstream missing:\n%s", text)
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

type facilityPanelHostFake struct {
	hostServerID string
	registered   []string
}

func (f *facilityPanelHostFake) HostServerID(context.Context) (string, error) {
	return f.hostServerID, nil
}

func (f *facilityPanelHostFake) RegisterHostServer(_ context.Context, serverID string) error {
	f.registered = append(f.registered, serverID)
	f.hostServerID = serverID
	return nil
}

func TestSaveReverseProxyRegistersUnregisteredPanelHost(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	host := &facilityPanelHostFake{}
	svc.panelHost = host
	ctx := context.Background()
	cfg, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a"},
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.registered, []string{"srv-a"}) {
		t.Fatalf("registered=%v want [srv-a]", host.registered)
	}
	if cfg.PanelHostServerID != "srv-a" {
		t.Fatalf("panelHostServerId=%q want srv-a", cfg.PanelHostServerID)
	}
}

func TestSaveReverseProxyRequiresRegisteredPanelHostWhenSet(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.panelHost = &facilityPanelHostFake{hostServerID: "srv-host"}
	ctx := context.Background()
	_, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a"},
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	})
	assertFacilityPanelError(t, err, "facility_panel_entry_host_required")
}
