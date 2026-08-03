package facilityapps

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/dns"
	"panel/internal/modules/servers"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

type fakeDNSProxySyncer struct {
	zones   []dns.Domain
	targets []dns.ProxyRecordTarget
	results []dns.ProxyZoneResult
	err     error
}

func (f *fakeDNSProxySyncer) ListDomains(ctx context.Context) ([]dns.Domain, error) {
	return f.zones, nil
}

func (f *fakeDNSProxySyncer) SyncProxyRecords(ctx context.Context, targets []dns.ProxyRecordTarget) ([]dns.ProxyZoneResult, error) {
	f.targets = targets
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func TestAffectedFacilityDomainsIgnoresPathOnlyChanges(t *testing.T) {
	previous := ReverseProxyConfig{Domains: []FacilityRouteDomain{{
		Domain: "app.example.test", OriginServerIDs: []string{"srv-a"},
		Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://old.example.test"}},
	}}}
	next := ReverseProxyConfig{Domains: []FacilityRouteDomain{{
		Domain: "app.example.test", OriginServerIDs: []string{"srv-a"},
		Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://new.example.test"}},
	}}}
	if affected := affectedFacilityDomains(previous, next); len(affected) != 0 {
		t.Fatalf("path-only change should not affect DNS: %#v", affected)
	}
}

func TestAffectedFacilityDomainsTracksServersAndPanelEntry(t *testing.T) {
	previous := ReverseProxyConfig{
		Domains: []FacilityRouteDomain{{Domain: "app.example.test", OriginServerIDs: []string{"srv-a"}}},
		PanelEntry: PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	}
	next := ReverseProxyConfig{
		Domains: []FacilityRouteDomain{{Domain: "app.example.test", OriginServerIDs: []string{"srv-a", "srv-b"}}},
		PanelEntry: PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	}
	affected := affectedFacilityDomains(previous, next)
	if len(affected) != 1 || affected[0] != "app.example.test" {
		t.Fatalf("affected = %#v", affected)
	}
	next.PanelEntry.Domain = "panel2.example.test"
	affected = affectedFacilityDomains(previous, next)
	if len(affected) != 3 {
		t.Fatalf("affected = %#v", affected)
	}
}

func TestDNSSyncDomainsOnSaveIncludesEveryCurrentDomain(t *testing.T) {
	previous := ReverseProxyConfig{Domains: []FacilityRouteDomain{
		{Domain: "app.example.test", OriginServerIDs: []string{"srv-a"}},
		{Domain: "gone.example.test", OriginServerIDs: []string{"srv-a"}},
	}}
	next := ReverseProxyConfig{
		DeploymentServers: []string{"srv-a", "srv-b"},
		Domains: []FacilityRouteDomain{
			{Domain: "app.example.test", OriginServerIDs: []string{"srv-a"}},
			{Domain: "new.example.test", OriginServerIDs: []string{"srv-b"}},
		},
		PanelEntry: PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	}
	got := dnsSyncDomainsOnSave(previous, next)
	want := []string{"app.example.test", "gone.example.test", "new.example.test", "panel.example.test"}
	if len(got) != len(want) {
		t.Fatalf("domains = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("domains = %#v, want %#v", got, want)
		}
	}
}

func TestDomainDNSServersUsesAnyAccessDeploymentServers(t *testing.T) {
	cfg := ReverseProxyConfig{DeploymentServers: []string{"srv-a", "srv-b"}}
	domain := FacilityRouteDomain{OriginServerIDs: []string{"srv-a"}}
	if got := domainDNSServers(cfg, domain); len(got) != 1 || got[0] != "srv-a" {
		t.Fatalf("origins = %#v", got)
	}
	domain.AnyAccess = applications.AnyAccessConfig{Enabled: true}
	if got := domainDNSServers(cfg, domain); len(got) != 2 {
		t.Fatalf("any access servers = %#v", got)
	}
}

func TestMatchProxyZonePrefersLongestZone(t *testing.T) {
	zones := []string{"test", "example.test", "api.example.test"}
	if got := matchProxyZone(zones, "app.api.example.test"); got != "api.example.test" {
		t.Fatalf("zone = %q", got)
	}
	if got := matchProxyZone(zones, "example.test"); got != "example.test" {
		t.Fatalf("zone = %q", got)
	}
	if got := matchProxyZone(zones, "*.example.test"); got != "example.test" {
		t.Fatalf("wildcard zone = %q", got)
	}
	if got := matchProxyZone(zones, "other.net"); got != "" {
		t.Fatalf("unmanaged zone = %q", got)
	}
}

func TestRunDNSSyncPersistsPerDomainStatus(t *testing.T) {
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
	defer store.Close()
	svc := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{
		"srv-a": {ID: "srv-a", IPv4: "203.0.113.10"},
		"srv-b": {ID: "srv-b", IPv6: "2001:db8::10"},
	}}, nil, WithDataRoot(cfg.DataRoot))
	syncer := &fakeDNSProxySyncer{
		zones:   []dns.Domain{{Name: "example.test"}},
		results: []dns.ProxyZoneResult{{Zone: "example.test"}},
	}
	svc.dns = syncer
	ctx := context.Background()

	config := ReverseProxyConfig{
		DeploymentServers: []string{"srv-a", "srv-b"},
		Domains: []FacilityRouteDomain{{
			Domain: "app.example.test", OriginServerIDs: []string{"srv-a"},
			Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
		}},
	}
	if err := svc.saveConfig(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := svc.runDNSSync(ctx, []string{"app.example.test"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.loadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DNSSync["app.example.test"].State != DNSSyncSynced {
		t.Fatalf("dns sync = %#v", loaded.DNSSync)
	}
	if len(syncer.targets) != 1 || len(syncer.targets[0].Records) != 1 || syncer.targets[0].Records[0].Value != "203.0.113.10" {
		t.Fatalf("targets = %#v", syncer.targets)
	}

	syncer.err = errors.New("provider failed")
	if err := svc.runDNSSync(ctx, []string{"app.example.test"}); err == nil {
		t.Fatal("expected provider failure")
	}
	loaded, _ = svc.loadConfig(ctx)
	if loaded.DNSSync["app.example.test"].State != DNSSyncFailed {
		t.Fatalf("dns sync after failure = %#v", loaded.DNSSync)
	}
}

func TestRunDNSSyncSkipsServersWithoutAddresses(t *testing.T) {
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
	defer store.Close()
	svc := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{
		"srv-a": {ID: "srv-a"},
	}}, nil, WithDataRoot(cfg.DataRoot))
	syncer := &fakeDNSProxySyncer{zones: []dns.Domain{{Name: "example.test"}}}
	svc.dns = syncer
	config := ReverseProxyConfig{
		DeploymentServers: []string{"srv-a"},
		Domains: []FacilityRouteDomain{{
			Domain: "app.example.test", OriginServerIDs: []string{"srv-a"},
			Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
		}},
	}
	if err := svc.saveConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if err := svc.runDNSSync(context.Background(), []string{"app.example.test"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DNSSync["app.example.test"].State != DNSSyncSkipped {
		t.Fatalf("dns sync = %#v", loaded.DNSSync)
	}
}
