package facilityapps

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	server "panel/internal/modules/servers"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
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

func TestSaveReverseProxyReturnsSavedConfigWhenReconcileFails(t *testing.T) {
	svc, closeStore := newFacilityTestService(t, errors.New("pull failed"))
	defer closeStore()

	cfg, err := svc.SaveReverseProxy(context.Background(), ReverseProxySaveInput{
		DeploymentServers: []string{"srv-edge"},
		Image:             defaultProxyImage,
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
	if !strings.Contains(cfg.LastError, "pull failed") {
		t.Fatalf("last error = %q", cfg.LastError)
	}
	if cfg.Operation == nil || cfg.Operation.Status != applications.LifecycleStatusFailed {
		t.Fatalf("operation = %#v", cfg.Operation)
	}
}

func newFacilityTestService(t *testing.T, agentErr error) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
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
	svc := NewService(store.AppDB(), facilityTestAgent{err: agentErr}, provider, nil)
	return svc, func() { _ = store.Close() }
}

type facilityTestServers struct {
	items map[string]server.Server
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

type facilityTestAgent struct {
	err error
}

func (a facilityTestAgent) RuntimeWriteFiles(context.Context, string, agentcontract.RuntimeWriteFilesRequest) error {
	return a.err
}

func (a facilityTestAgent) RuntimeCreateContainer(context.Context, string, agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	return agentcontract.RuntimeCreateContainerResponse{}, a.err
}

func (a facilityTestAgent) RuntimeStop(context.Context, string, agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	return agentcontract.RuntimeInstanceResponse{}, nil
}

func (a facilityTestAgent) DockerImagePull(context.Context, string, string) error {
	return a.err
}

func (a facilityTestAgent) DockerContainerAction(context.Context, string, string, string) error {
	return a.err
}
