package facilityapps

import (
	"context"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
	panelerr "panel/internal/platform/errors"
)

// facilityTestCertificateProvider 让 routeSummaries 走真实证书匹配路径。
type facilityTestCertificateProvider struct {
	certificates []proxycert.Certificate
}

func (p facilityTestCertificateProvider) ReverseProxyCertificates(context.Context) ([]proxycert.Certificate, error) {
	return p.certificates, nil
}

func stringPtr(value string) *string { return &value }

func renderTestDomain(domain string, httpsEnabled bool, httpPortMode *string) FacilityRouteDomain {
	https := httpsEnabled
	return FacilityRouteDomain{
		Domain:          domain,
		OriginServerIDs: []string{"srv-a"},
		HTTPSEnabled:    &https,
		HTTPPortMode:    httpPortMode,
		Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
	}
}

func TestNormalizeInputPerDomainHTTPPortMode(t *testing.T) {
	base := func(override *string) ReverseProxySaveInput {
		return ReverseProxySaveInput{
			DeploymentServers: []string{"srv-a"},
			Domains: []FacilityRouteDomain{{
				Domain:          "example.test",
				OriginServerIDs: []string{"srv-a"},
				HTTPPortMode:    override,
				Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
			}},
		}
	}
	// nil 与空白都表示继承（归一化为 nil）。
	for _, override := range []*string{nil, stringPtr(""), stringPtr("   ")} {
		cfg, err := normalizeInput(base(override))
		if err != nil {
			t.Fatalf("normalize inherit override %#v: %v", override, err)
		}
		if cfg.Domains[0].HTTPPortMode != nil {
			t.Fatalf("inherit override %#v normalized to %q, want nil", override, *cfg.Domains[0].HTTPPortMode)
		}
	}
	// 合法值大小写/空白归一化。
	for _, tc := range []struct {
		override *string
		want     string
	}{
		{stringPtr(" Redirect "), HTTPPortModeRedirect},
		{stringPtr("SERVE"), HTTPPortModeServe},
	} {
		cfg, err := normalizeInput(base(tc.override))
		if err != nil {
			t.Fatalf("normalize override %#v: %v", tc.override, err)
		}
		if cfg.Domains[0].HTTPPortMode == nil || *cfg.Domains[0].HTTPPortMode != tc.want {
			t.Fatalf("override %#v normalized = %#v, want %q", tc.override, cfg.Domains[0].HTTPPortMode, tc.want)
		}
	}
	// 非法值返回稳定错误码并带域名上下文。
	_, err := normalizeInput(base(stringPtr("bogus")))
	assertFacilityPanelError(t, err, "facility_http_port_mode_invalid")
	panelFailure, ok := err.(*panelerr.Error)
	if !ok || panelFailure.Details["value"] != "bogus" || panelFailure.Details["domain"] != "example.test" {
		t.Fatalf("invalid override error = %v, want domain/value details", err)
	}
}

func TestRenderNginxConfigPerDomainHTTPPortMode(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	certificates := []proxycert.Certificate{{
		ID:             "example-cert",
		Domains:        []string{"example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}
	const redirectLocation = "return 301 https://$host$request_uri;"
	cases := []struct {
		name         string
		global       string
		override     *string
		httpsEnabled bool
		certificates []proxycert.Certificate
		wantRedirect bool
	}{
		{name: "domain redirect overrides global serve", global: HTTPPortModeServe, override: stringPtr(HTTPPortModeRedirect), httpsEnabled: true, certificates: certificates, wantRedirect: true},
		{name: "domain serve overrides global redirect", global: HTTPPortModeRedirect, override: stringPtr(HTTPPortModeServe), httpsEnabled: true, certificates: certificates, wantRedirect: false},
		{name: "nil override inherits global redirect", global: HTTPPortModeRedirect, override: nil, httpsEnabled: true, certificates: certificates, wantRedirect: true},
		{name: "empty override inherits global redirect", global: HTTPPortModeRedirect, override: stringPtr(""), httpsEnabled: true, certificates: certificates, wantRedirect: true},
		{name: "redirect falls back without certificate", global: HTTPPortModeRedirect, override: stringPtr(HTTPPortModeRedirect), httpsEnabled: true, certificates: nil, wantRedirect: false},
		{name: "redirect falls back when https disabled", global: HTTPPortModeRedirect, override: stringPtr(HTTPPortModeRedirect), httpsEnabled: false, certificates: certificates, wantRedirect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ReverseProxyConfig{
				DeploymentServers: []string{"srv-a"},
				Settings:          FacilityProxySettings{HTTPPortMode: tc.global},
				Domains:           []FacilityRouteDomain{renderTestDomain("example.test", tc.httpsEnabled, tc.override)},
			}
			_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", cfg, nil, tc.certificates)
			if err != nil {
				t.Fatal(err)
			}
			text := managedConfigText(files)
			if strings.Contains(text, redirectLocation) != tc.wantRedirect {
				t.Fatalf("redirect presence = %v, want %v:\n%s", strings.Contains(text, redirectLocation), tc.wantRedirect, text)
			}
			// redirect 生效时 80 server 直接返回 301，不渲染 upstream 错误页。
			if tc.wantRedirect {
				port80Server := strings.SplitN(text, "listen 80;", 2)
				if len(port80Server) == 2 && strings.Contains(strings.SplitN(port80Server[1], "server {", 2)[0], "@panel_upstream_unavailable") {
					t.Fatalf("redirecting port 80 server must not render upstream error page:\n%s", text)
				}
			}
		})
	}
}

func TestRouteSummariesReportEffectiveHTTPPortMode(t *testing.T) {
	certificates := []proxycert.Certificate{{
		ID:      "example-cert",
		Source:  HTTPSDomainCertificate,
		Domains: []string{"a.example.test", "b.example.test", "c.example.test", "app.example.test"},
	}}
	svc := &Service{certificates: facilityTestCertificateProvider{certificates: certificates}}
	cfg := ReverseProxyConfig{
		DeploymentServers: []string{"srv-a"},
		Settings:          FacilityProxySettings{HTTPPortMode: HTTPPortModeRedirect},
		Domains: []FacilityRouteDomain{
			renderTestDomain("a.example.test", true, stringPtr(HTTPPortModeServe)),
			renderTestDomain("b.example.test", true, nil),
			renderTestDomain("c.example.test", true, stringPtr(HTTPPortModeRedirect)),
			renderTestDomain("d.example.test", true, stringPtr(HTTPPortModeRedirect)),
		},
	}
	appRoutes := []applications.ApplicationReverseProxyConfig{{
		ApplicationID: "app-1",
		Routes: []applications.ReverseProxyRoute{{
			Domain:          "app.example.test",
			TargetPort:      8080,
			OriginServerIDs: []string{"srv-a"},
			Paths:           []applications.ReverseProxyPath{{Path: "/"}},
			HTTPSEnabled:    true,
			HTTP2Enabled:    true,
			HTTPPortMode:    HTTPPortModeServe,
		}},
	}}
	summaries, err := svc.routeSummaries(context.Background(), cfg, appRoutes)
	if err != nil {
		t.Fatalf("route summaries: %v", err)
	}
	byDomain := map[string]RouteSummary{}
	for _, summary := range summaries {
		if summary.Domain == "d.example.test" {
			// 无证书域名由 bestCertificate 回退，不影响其他断言。
			continue
		}
		byDomain[summary.Domain+"|"+summary.Source] = summary
	}
	cases := map[string]struct {
		domain string
		source string
		want   string
	}{
		"domain serve overrides global redirect": {domain: "a.example.test", source: "facility", want: HTTPPortModeServe},
		"domain inherits global redirect":        {domain: "b.example.test", source: "facility", want: HTTPPortModeRedirect},
		"domain redirect stays redirect":         {domain: "c.example.test", source: "facility", want: HTTPPortModeRedirect},
		"application route serve":                {domain: "app.example.test", source: "application", want: HTTPPortModeServe},
	}
	for name, tc := range cases {
		summary, ok := byDomain[tc.domain+"|"+tc.source]
		if !ok {
			t.Fatalf("%s: summary for %s missing: %#v", name, tc.domain, summaries)
		}
		if summary.HTTPPortMode != tc.want {
			t.Fatalf("%s: httpPortMode = %q, want %q", name, summary.HTTPPortMode, tc.want)
		}
	}
	// 无证书域名：即使配置 redirect，摘要也必须投影 serve（与渲染一致）。
	for _, summary := range summaries {
		if summary.Domain != "d.example.test" {
			continue
		}
		if summary.EffectiveHTTPS || summary.HTTPPortMode != HTTPPortModeServe {
			t.Fatalf("no-certificate redirect summary = %#v, want effective https false and serve", summary)
		}
	}
}

func TestFacilityDomainHTTPPortModeEditSessionRoundTrip(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")

	draft := configRecordDraftForTest("mode.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draft.HTTPPortMode = stringPtr(HTTPPortModeRedirect)
	result := commitConfigRecordForTest(t, svc, "seed-mode", draft, nil)
	if result.Record.Domain.HTTPPortMode == nil || *result.Record.Domain.HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("commit record httpPortMode = %#v, want redirect", result.Record.Domain.HTTPPortMode)
	}

	// 记录读取与数据库列必须一致，继承规则的记录回填 nil。
	record := mustGetConfigRecord(t, svc, "mode.example.test")
	if record.Domain.HTTPPortMode == nil || *record.Domain.HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("get config record httpPortMode = %#v, want redirect", record.Domain.HTTPPortMode)
	}
	var stored string
	if err := svc.db.QueryRowContext(ctx, `SELECT http_port_mode FROM reverse_proxy_routes WHERE domain=?`, "mode.example.test").Scan(&stored); err != nil {
		t.Fatalf("read http_port_mode: %v", err)
	}
	if stored != HTTPPortModeRedirect {
		t.Fatalf("stored http_port_mode = %q, want redirect", stored)
	}

	// 更新为 serve 覆盖。
	draft.HTTPPortMode = stringPtr(HTTPPortModeServe)
	result = commitConfigRecordForTest(t, svc, "update-mode", draft, nil)
	if result.Record.Version != 2 {
		t.Fatalf("updated record version = %d, want 2", result.Record.Version)
	}
	record = mustGetConfigRecord(t, svc, "mode.example.test")
	if record.Domain.HTTPPortMode == nil || *record.Domain.HTTPPortMode != HTTPPortModeServe {
		t.Fatalf("updated httpPortMode = %#v, want serve", record.Domain.HTTPPortMode)
	}

	// 更新为继承（空串）后落库为空串、响应回填 nil。
	draft.HTTPPortMode = stringPtr("")
	result = commitConfigRecordForTest(t, svc, "inherit-mode", draft, nil)
	record = mustGetConfigRecord(t, svc, "mode.example.test")
	if record.Domain.HTTPPortMode != nil {
		t.Fatalf("inherit httpPortMode = %q, want nil", *record.Domain.HTTPPortMode)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT http_port_mode FROM reverse_proxy_routes WHERE domain=?`, "mode.example.test").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("stored inherit http_port_mode = %q, want empty", stored)
	}

	// ListConfigRecords 与 GET 反代配置都必须带上覆盖值。
	draft.HTTPPortMode = stringPtr(HTTPPortModeRedirect)
	commitConfigRecordForTest(t, svc, "list-mode", draft, nil)
	records, err := svc.ListConfigRecords(ctx)
	if err != nil {
		t.Fatalf("list config records: %v", err)
	}
	found := false
	for _, item := range records {
		if item.RecordID != "mode.example.test" {
			continue
		}
		found = true
		if item.Domain.HTTPPortMode == nil || *item.Domain.HTTPPortMode != HTTPPortModeRedirect {
			t.Fatalf("listed httpPortMode = %#v, want redirect", item.Domain.HTTPPortMode)
		}
	}
	if !found {
		t.Fatalf("mode.example.test missing from records: %#v", records)
	}
	config, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatalf("get reverse proxy: %v", err)
	}
	if len(config.Domains) != 1 || config.Domains[0].HTTPPortMode == nil || *config.Domains[0].HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("reverse proxy domains = %#v, want redirect override", config.Domains)
	}
}

func TestSaveReverseProxyPersistsPerDomainHTTPPortMode(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()

	save := func(override *string) ReverseProxyConfig {
		t.Helper()
		cfg, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
			DeploymentServers: []string{"srv-a"},
			Domains:           []FacilityRouteDomain{renderTestDomain("save.example.test", true, override)},
		})
		if err != nil {
			t.Fatalf("save reverse proxy: %v", err)
		}
		return cfg
	}

	cfg := save(stringPtr(HTTPPortModeRedirect))
	if len(cfg.Domains) != 1 || cfg.Domains[0].HTTPPortMode == nil || *cfg.Domains[0].HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("saved domains = %#v, want redirect override", cfg.Domains)
	}
	var stored string
	if err := svc.db.QueryRowContext(ctx, `SELECT http_port_mode FROM reverse_proxy_routes WHERE domain=?`, "save.example.test").Scan(&stored); err != nil {
		t.Fatalf("read saved http_port_mode: %v", err)
	}
	if stored != HTTPPortModeRedirect {
		t.Fatalf("saved http_port_mode = %q, want redirect", stored)
	}
	// loadFacilityRouteDomains 路径（GET 反代配置）必须回填同一覆盖值。
	domains, err := svc.loadFacilityRouteDomains(ctx)
	if err != nil {
		t.Fatalf("load facility route domains: %v", err)
	}
	if len(domains) != 1 || domains[0].HTTPPortMode == nil || *domains[0].HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("loaded domains = %#v, want redirect override", domains)
	}

	// 保存继承值后列清空、响应回填 nil。
	cfg = save(stringPtr(""))
	if len(cfg.Domains) != 1 || cfg.Domains[0].HTTPPortMode != nil {
		t.Fatalf("inherit saved domains = %#v, want nil override", cfg.Domains)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT http_port_mode FROM reverse_proxy_routes WHERE domain=?`, "save.example.test").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("inherit saved http_port_mode = %q, want empty", stored)
	}
}

func TestListApplicationRouteRecordsMapsHTTPPortMode(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.apps = facilityTestApplicationRoutesProvider{configs: []applications.ApplicationReverseProxyConfig{{
		ApplicationID:   "app-1",
		ApplicationName: "app",
		Routes: []applications.ReverseProxyRoute{
			{Domain: "redirect.example.test", TargetPort: 8080, OriginServerIDs: []string{"srv-a"}, HTTPSEnabled: true, HTTP2Enabled: true, HTTPPortMode: HTTPPortModeRedirect},
			{Domain: "inherit.example.test", TargetPort: 8080, OriginServerIDs: []string{"srv-a"}, HTTPSEnabled: true, HTTP2Enabled: true},
		},
	}}}
	records, err := svc.listApplicationRouteRecords(context.Background())
	if err != nil {
		t.Fatalf("list application route records: %v", err)
	}
	byID := map[string]FacilityRouteDomain{}
	for _, item := range records {
		byID[item.RecordID] = item.Domain
	}
	if domain := byID["redirect.example.test"]; domain.HTTPPortMode == nil || *domain.HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("redirect application route httpPortMode = %#v, want redirect", domain.HTTPPortMode)
	}
	if domain := byID["inherit.example.test"]; domain.HTTPPortMode != nil {
		t.Fatalf("inherit application route httpPortMode = %q, want nil", *domain.HTTPPortMode)
	}
}
