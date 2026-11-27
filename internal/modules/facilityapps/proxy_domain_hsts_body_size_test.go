package facilityapps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
	panelerr "panel/internal/platform/errors"
)

// 域名级 HSTS/请求体上限覆盖的归一化：nil/空白=继承，合法值小写归一化，
// 非法 body size 复用设施稳定错误码。
func TestNormalizeInputPerDomainHSTSAndBodySize(t *testing.T) {
	base := func(hsts *bool, size *string) ReverseProxySaveInput {
		return ReverseProxySaveInput{
			DeploymentServers: []string{"srv-a"},
			Domains: []FacilityRouteDomain{{
				Domain:            "example.test",
				OriginServerIDs:   []string{"srv-a"},
				HSTSEnabled:       hsts,
				ClientMaxBodySize: size,
				Paths:             []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
			}},
		}
	}

	// nil/空白都表示继承，归一化为 nil（HSTS 与 body size 各自独立）。
	cfg, err := normalizeInput(base(nil, stringPtr("   ")))
	if err != nil {
		t.Fatalf("normalize inherit overrides: %v", err)
	}
	if cfg.Domains[0].HSTSEnabled != nil || cfg.Domains[0].ClientMaxBodySize != nil {
		t.Fatalf("inherit overrides = %#v/%#v, want nil", cfg.Domains[0].HSTSEnabled, cfg.Domains[0].ClientMaxBodySize)
	}

	// 显式 on/off 与合法 size 大小写/空白归一化。
	for _, tc := range []struct {
		hsts *bool
		size *string
		want bool
	}{
		{boolPtr(true), stringPtr(" 128M "), true},
		{boolPtr(false), stringPtr("1G"), false},
	} {
		cfg, err := normalizeInput(base(tc.hsts, tc.size))
		if err != nil {
			t.Fatalf("normalize overrides %#v/%#v: %v", tc.hsts, tc.size, err)
		}
		if cfg.Domains[0].HSTSEnabled == nil || *cfg.Domains[0].HSTSEnabled != tc.want {
			t.Fatalf("hsts override = %#v, want %v", cfg.Domains[0].HSTSEnabled, tc.want)
		}
		wantSize := strings.ToLower(strings.TrimSpace(*tc.size))
		if cfg.Domains[0].ClientMaxBodySize == nil || *cfg.Domains[0].ClientMaxBodySize != wantSize {
			t.Fatalf("body size override = %#v, want %q", cfg.Domains[0].ClientMaxBodySize, wantSize)
		}
	}

	// 非法 size 返回稳定错误码并带域名/值上下文。
	_, err = normalizeInput(base(nil, stringPtr("2g")))
	assertFacilityPanelError(t, err, "facility_client_max_body_size_invalid")
	var panelFailure *panelerr.Error
	if !errors.As(err, &panelFailure) || panelFailure.Details["domain"] != "example.test" || panelFailure.Details["value"] != "2g" {
		t.Fatalf("invalid body size error = %v, want domain/value details", err)
	}
}

func TestRenderNginxConfigPerDomainHSTSAndBodySize(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	certificates := []proxycert.Certificate{{
		ID:             "example-cert",
		Domains:        []string{"example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}
	global := FacilityProxySettings{
		HSTS:              HSTSConfig{Enabled: true, MaxAgeSeconds: 600, IncludeSubDomains: true},
		ClientMaxBodySize: "50m",
	}
	render := func(httpsEnabled bool, hsts *bool, size *string, certs []proxycert.Certificate) string {
		t.Helper()
		domain := renderTestDomain("example.test", httpsEnabled, nil)
		domain.HSTSEnabled = hsts
		domain.ClientMaxBodySize = size
		_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", ReverseProxyConfig{
			DeploymentServers: []string{"srv-a"},
			Settings:          global,
			Domains:           []FacilityRouteDomain{domain},
		}, nil, certs)
		if err != nil {
			t.Fatalf("render nginx config: %v", err)
		}
		return managedConfigText(files)
	}
	const hstsHeader = `Strict-Transport-Security "max-age=600; includeSubDomains"`

	// 全局 HSTS 开 + 域名覆盖 off：443 server 不输出 HSTS；body size 覆盖值在
	// 80/443 两个 server 块各输出一次。
	text := render(true, boolPtr(false), stringPtr("128m"), certificates)
	if strings.Contains(text, "Strict-Transport-Security") {
		t.Fatalf("domain HSTS off must suppress header:\n%s", text)
	}
	if got := strings.Count(text, "client_max_body_size 128m;"); got != 2 {
		t.Fatalf("body size occurrences = %d, want 2 (80+443):\n%s", got, text)
	}

	// 全局 HSTS 关 + 域名覆盖 on：仍按全局 maxAge/includeSubDomains 输出。
	text = render(true, boolPtr(true), nil, certificates)
	if !strings.Contains(text, hstsHeader) {
		t.Fatalf("domain HSTS on must emit header:\n%s", text)
	}
	// 未覆盖 body size 时继承全局值。
	if got := strings.Count(text, "client_max_body_size 50m;"); got != 2 {
		t.Fatalf("inherited body size occurrences = %d, want 2:\n%s", got, text)
	}

	// 未覆盖 HSTS 时继承全局开关。
	text = render(true, nil, nil, certificates)
	if !strings.Contains(text, hstsHeader) {
		t.Fatalf("inherited global HSTS must emit header:\n%s", text)
	}

	// 全局设置沿用既有校验（允许 >1024m）；域名继承时原样输出，不得回退默认。
	global.ClientMaxBodySize = "2g"
	text = render(true, nil, nil, certificates)
	if got := strings.Count(text, "client_max_body_size 2g;"); got != 2 {
		t.Fatalf("legacy global body size occurrences = %d, want 2:\n%s", got, text)
	}
	global.ClientMaxBodySize = "50m"

	// 没有证书/HTTPS 关闭：即使域名覆盖 on 也不得输出 HSTS；body size 仍输出。
	text = render(true, boolPtr(true), stringPtr("64m"), nil)
	if strings.Contains(text, "Strict-Transport-Security") {
		t.Fatalf("no certificate must not emit HSTS:\n%s", text)
	}
	if got := strings.Count(text, "client_max_body_size 64m;"); got != 1 {
		t.Fatalf("no-cert body size occurrences = %d, want 1 (80 only):\n%s", got, text)
	}
	text = render(false, boolPtr(true), nil, certificates)
	if strings.Contains(text, "Strict-Transport-Security") {
		t.Fatalf("https disabled must not emit HSTS:\n%s", text)
	}
}

func TestRouteSummariesReportEffectiveHSTSAndBodySize(t *testing.T) {
	certificates := []proxycert.Certificate{{
		ID:      "example-cert",
		Source:  HTTPSDomainCertificate,
		Domains: []string{"a.example.test", "b.example.test", "app.example.test"},
	}}
	svc := &Service{certificates: facilityTestCertificateProvider{certificates: certificates}}
	cfg := ReverseProxyConfig{
		DeploymentServers: []string{"srv-a"},
		Settings: FacilityProxySettings{
			HSTS:              HSTSConfig{Enabled: true, MaxAgeSeconds: 600},
			ClientMaxBodySize: "50m",
		},
		Domains: []FacilityRouteDomain{
			renderTestDomain("a.example.test", true, nil),
			renderTestDomain("b.example.test", true, nil),
		},
	}
	cfg.Domains[0].HSTSEnabled = boolPtr(false)
	cfg.Domains[0].ClientMaxBodySize = stringPtr("128m")
	hstsOn := true
	appRoutes := []applications.ApplicationReverseProxyConfig{{
		ApplicationID: "app-1",
		Routes: []applications.ReverseProxyRoute{{
			Domain:            "app.example.test",
			TargetPort:        8080,
			OriginServerIDs:   []string{"srv-a"},
			Paths:             []applications.ReverseProxyPath{{Path: "/"}},
			HTTPSEnabled:      true,
			HTTP2Enabled:      true,
			HSTSEnabled:       &hstsOn,
			ClientMaxBodySize: "1g",
		}},
	}}
	summaries, err := svc.routeSummaries(context.Background(), cfg, appRoutes)
	if err != nil {
		t.Fatalf("route summaries: %v", err)
	}
	byKey := map[string]RouteSummary{}
	for _, summary := range summaries {
		byKey[summary.Domain+"|"+summary.Source] = summary
	}
	assertSummary := func(domain, source string, wantHSTS bool, wantSize string) {
		t.Helper()
		summary, ok := byKey[domain+"|"+source]
		if !ok {
			t.Fatalf("summary %s/%s missing: %#v", domain, source, summaries)
		}
		if summary.HSTSEnabled != wantHSTS || summary.ClientMaxBodySize != wantSize {
			t.Fatalf("%s/%s summary HSTS/body = %v/%q, want %v/%q", domain, source, summary.HSTSEnabled, summary.ClientMaxBodySize, wantHSTS, wantSize)
		}
	}
	assertSummary("a.example.test", "facility", false, "128m")
	assertSummary("b.example.test", "facility", true, "50m")
	assertSummary("app.example.test", "application", true, "1g")
}

// 应用反代规则与设施域名共用 HSTS/body size 解析：渲染层必须消费应用规则上
// 已解析的覆盖值，而不是只看设施全局设置。
func TestRenderNginxConfigAppliesApplicationRouteHSTSAndBodySize(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	certificates := []proxycert.Certificate{{
		ID:             "app-cert",
		Domains:        []string{"app.example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}
	hstsOn := true
	appRoutes := []applications.ApplicationReverseProxyConfig{{
		ApplicationID: "app-1",
		Routes: []applications.ReverseProxyRoute{{
			Domain:            "app.example.test",
			TargetPort:        8080,
			TargetContainer:   "panel-app-1",
			OriginServerIDs:   []string{"srv-a"},
			Paths:             []applications.ReverseProxyPath{{Path: "/"}},
			HTTPSEnabled:      true,
			HTTP2Enabled:      true,
			HSTSEnabled:       &hstsOn,
			ClientMaxBodySize: "128m",
		}},
	}}
	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", ReverseProxyConfig{
		DeploymentServers: []string{"srv-a"},
		Settings: FacilityProxySettings{
			HSTS:              HSTSConfig{Enabled: false, MaxAgeSeconds: 600},
			ClientMaxBodySize: "50m",
		},
	}, appRoutes, certificates)
	if err != nil {
		t.Fatalf("render nginx config with application route: %v", err)
	}
	text := managedConfigText(files)
	if !strings.Contains(text, "Strict-Transport-Security") {
		t.Fatalf("application route HSTS override missing:\n%s", text)
	}
	if got := strings.Count(text, "client_max_body_size 128m;"); got != 2 {
		t.Fatalf("application route body size occurrences = %d, want 2:\n%s", got, text)
	}
	if strings.Contains(text, "client_max_body_size 50m;") {
		t.Fatalf("global body size must be overridden:\n%s", text)
	}
}

func TestFacilityDomainHSTSBodySizeEditSessionRoundTrip(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")

	draft := configRecordDraftForTest("hsts.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draft.HSTSEnabled = boolPtr(true)
	draft.ClientMaxBodySize = stringPtr("128m")
	result := commitConfigRecordForTest(t, svc, "seed-hsts", draft, nil)
	if result.Record.Domain.HSTSEnabled == nil || !*result.Record.Domain.HSTSEnabled {
		t.Fatalf("commit record hsts = %#v, want true", result.Record.Domain.HSTSEnabled)
	}
	if result.Record.Domain.ClientMaxBodySize == nil || *result.Record.Domain.ClientMaxBodySize != "128m" {
		t.Fatalf("commit record body size = %#v, want 128m", result.Record.Domain.ClientMaxBodySize)
	}

	record := mustGetConfigRecord(t, svc, "hsts.example.test")
	if record.Domain.HSTSEnabled == nil || !*record.Domain.HSTSEnabled {
		t.Fatalf("get config record hsts = %#v, want true", record.Domain.HSTSEnabled)
	}
	if record.Domain.ClientMaxBodySize == nil || *record.Domain.ClientMaxBodySize != "128m" {
		t.Fatalf("get config record body size = %#v, want 128m", record.Domain.ClientMaxBodySize)
	}
	assertHSTSBodySizeStored(t, svc, "hsts.example.test", "on", "128m")

	// 更新为 off + 1g。
	draft.HSTSEnabled = boolPtr(false)
	draft.ClientMaxBodySize = stringPtr("1G")
	result = commitConfigRecordForTest(t, svc, "update-hsts", draft, nil)
	if result.Record.Version != 2 {
		t.Fatalf("updated record version = %d, want 2", result.Record.Version)
	}
	record = mustGetConfigRecord(t, svc, "hsts.example.test")
	if record.Domain.HSTSEnabled == nil || *record.Domain.HSTSEnabled {
		t.Fatalf("updated hsts = %#v, want false", record.Domain.HSTSEnabled)
	}
	if record.Domain.ClientMaxBodySize == nil || *record.Domain.ClientMaxBodySize != "1g" {
		t.Fatalf("updated body size = %#v, want 1g", record.Domain.ClientMaxBodySize)
	}
	assertHSTSBodySizeStored(t, svc, "hsts.example.test", "off", "1g")

	// 更新为继承：落空串、响应回填 nil。
	draft.HSTSEnabled = nil
	draft.ClientMaxBodySize = stringPtr("")
	commitConfigRecordForTest(t, svc, "inherit-hsts", draft, nil)
	record = mustGetConfigRecord(t, svc, "hsts.example.test")
	if record.Domain.HSTSEnabled != nil || record.Domain.ClientMaxBodySize != nil {
		t.Fatalf("inherit overrides = %#v/%#v, want nil", record.Domain.HSTSEnabled, record.Domain.ClientMaxBodySize)
	}
	assertHSTSBodySizeStored(t, svc, "hsts.example.test", "", "")

	// ListConfigRecords 与 GET 反代配置都必须带上覆盖值。
	draft.HSTSEnabled = boolPtr(true)
	draft.ClientMaxBodySize = stringPtr("128m")
	commitConfigRecordForTest(t, svc, "list-hsts", draft, nil)
	records, err := svc.ListConfigRecords(ctx)
	if err != nil {
		t.Fatalf("list config records: %v", err)
	}
	found := false
	for _, item := range records {
		if item.RecordID != "hsts.example.test" {
			continue
		}
		found = true
		if item.Domain.HSTSEnabled == nil || !*item.Domain.HSTSEnabled || item.Domain.ClientMaxBodySize == nil || *item.Domain.ClientMaxBodySize != "128m" {
			t.Fatalf("listed overrides = %#v/%#v", item.Domain.HSTSEnabled, item.Domain.ClientMaxBodySize)
		}
	}
	if !found {
		t.Fatalf("hsts.example.test missing from records: %#v", records)
	}
	config, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatalf("get reverse proxy: %v", err)
	}
	if len(config.Domains) != 1 || config.Domains[0].HSTSEnabled == nil || !*config.Domains[0].HSTSEnabled || config.Domains[0].ClientMaxBodySize == nil || *config.Domains[0].ClientMaxBodySize != "128m" {
		t.Fatalf("reverse proxy domains = %#v, want overrides", config.Domains)
	}
}

func TestSaveReverseProxyPersistsPerDomainHSTSAndBodySize(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()

	save := func(hsts *bool, size *string) ReverseProxyConfig {
		t.Helper()
		domain := renderTestDomain("save-hsts.example.test", true, nil)
		domain.HSTSEnabled = hsts
		domain.ClientMaxBodySize = size
		cfg, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
			DeploymentServers: []string{"srv-a"},
			Domains:           []FacilityRouteDomain{domain},
		})
		if err != nil {
			t.Fatalf("save reverse proxy: %v", err)
		}
		return cfg
	}

	cfg := save(boolPtr(true), stringPtr("128m"))
	if len(cfg.Domains) != 1 || cfg.Domains[0].HSTSEnabled == nil || !*cfg.Domains[0].HSTSEnabled || cfg.Domains[0].ClientMaxBodySize == nil || *cfg.Domains[0].ClientMaxBodySize != "128m" {
		t.Fatalf("saved domains = %#v, want HSTS on and 128m", cfg.Domains)
	}
	assertHSTSBodySizeStored(t, svc, "save-hsts.example.test", "on", "128m")
	// loadFacilityRouteDomains 路径（GET 反代配置）必须回填同一覆盖值。
	domains, err := svc.loadFacilityRouteDomains(ctx)
	if err != nil {
		t.Fatalf("load facility route domains: %v", err)
	}
	if len(domains) != 1 || domains[0].HSTSEnabled == nil || !*domains[0].HSTSEnabled || domains[0].ClientMaxBodySize == nil || *domains[0].ClientMaxBodySize != "128m" {
		t.Fatalf("loaded domains = %#v, want overrides", domains)
	}

	// 保存继承值后列清空、响应回填 nil。
	cfg = save(nil, stringPtr(""))
	if len(cfg.Domains) != 1 || cfg.Domains[0].HSTSEnabled != nil || cfg.Domains[0].ClientMaxBodySize != nil {
		t.Fatalf("inherit saved domains = %#v, want nil overrides", cfg.Domains)
	}
	assertHSTSBodySizeStored(t, svc, "save-hsts.example.test", "", "")
}

func TestListApplicationRouteRecordsMapsHSTSAndBodySize(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	hstsOff := false
	svc.apps = facilityTestApplicationRoutesProvider{configs: []applications.ApplicationReverseProxyConfig{{
		ApplicationID:   "app-1",
		ApplicationName: "app",
		Routes: []applications.ReverseProxyRoute{
			{Domain: "override.example.test", TargetPort: 8080, OriginServerIDs: []string{"srv-a"}, HTTPSEnabled: true, HTTP2Enabled: true, HSTSEnabled: &hstsOff, ClientMaxBodySize: "128m"},
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
	override, ok := byID["override.example.test"]
	if !ok || override.HSTSEnabled == nil || *override.HSTSEnabled || override.ClientMaxBodySize == nil || *override.ClientMaxBodySize != "128m" {
		t.Fatalf("override application route = %#v", override)
	}
	inherit, ok := byID["inherit.example.test"]
	if !ok || inherit.HSTSEnabled != nil || inherit.ClientMaxBodySize != nil {
		t.Fatalf("inherit application route overrides = %#v/%#v, want nil", inherit.HSTSEnabled, inherit.ClientMaxBodySize)
	}
}

func assertHSTSBodySizeStored(t *testing.T, svc *Service, domain, hsts, bodySize string) {
	t.Helper()
	var gotHSTS, gotBodySize string
	if err := svc.db.QueryRowContext(context.Background(), `SELECT hsts_enabled,client_max_body_size FROM reverse_proxy_routes WHERE domain=?`, domain).Scan(&gotHSTS, &gotBodySize); err != nil {
		t.Fatalf("read HSTS/body size columns: %v", err)
	}
	if gotHSTS != hsts || gotBodySize != bodySize {
		t.Fatalf("HSTS/body size columns = %q/%q, want %q/%q", gotHSTS, gotBodySize, hsts, bodySize)
	}
}
