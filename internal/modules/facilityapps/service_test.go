package facilityapps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
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
