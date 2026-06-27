package facilityapps

import (
	"context"
	"strings"
	"testing"

	"panel/internal/modules/applications"
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
				{Domain: "static.example.test", TargetPort: 8080, Paths: []applications.ReverseProxyPath{{Path: "/api"}}},
			},
		},
	}

	nginx, mounts, _, err := svc.renderNginxConfig(context.Background(), "srv-edge", cfg, apps, nil)
	if err != nil {
		t.Fatalf("render nginx config: %v", err)
	}
	if strings.Count(nginx, "server_name static.example.test;") != 1 {
		t.Fatalf("expected one http server for domain, got config:\n%s", nginx)
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
		"location /api {",
	} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("expected nginx config to contain %q, got:\n%s", want, nginx)
		}
	}
	if len(mounts) != 2 {
		t.Fatalf("expected two static mounts, got %d", len(mounts))
	}
}

func TestNormalizeInputRejectsDuplicateDomainPath(t *testing.T) {
	_, err := normalizeInput(ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
		StaticSites: []StaticSite{
			{Domain: "static.example.test", Path: "/docs", RootPath: "/srv/www/docs", SourceType: StaticSourceHostPath},
			{Domain: "static.example.test", Path: "/docs", RuleType: StaticRuleRedirect, RedirectURL: "https://static.example.test/new", RedirectCode: 302},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate domain/path validation error")
	}
}
