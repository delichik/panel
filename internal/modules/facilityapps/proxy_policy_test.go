package facilityapps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/proxycert"
	"panel/internal/modules/servers"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

func TestParseCustomDirectivesOverridesAndValidates(t *testing.T) {
	directives, err := parseCustomDirectives(directiveScopeHTTP, []string{
		"client_max_body_size 10m;",
		"  add_header X-One 1;  ",
		"ADD_HEADER X-Two 2",
	})
	if err == nil {
		t.Fatalf("uppercase directive name must be rejected: %#v", directives)
	}

	directives, err = parseCustomDirectives(directiveScopeHTTP, []string{
		"client_max_body_size 10m;",
		"client_max_body_size 20m",
		"add_header X-One 1;",
		"add_header X-Two 2;",
	})
	if err != nil {
		t.Fatalf("parse directives: %v", err)
	}
	if len(directives) != 3 {
		t.Fatalf("directives = %#v, want last duplicate dropped plus two add_header entries", directives)
	}
	if directives[0].Name != "client_max_body_size" || directives[0].Args != "20m" {
		t.Fatalf("duplicate single-value directive must keep last: %#v", directives[0])
	}
	if directives[1].Name != "add_header" || directives[2].Name != "add_header" {
		t.Fatalf("repeatable directives must keep all entries: %#v", directives)
	}
}

func TestParseCustomDirectivesRejectsForbiddenAndBlocks(t *testing.T) {
	for _, line := range []string{
		"listen 8080;",
		"server_name example.test;",
		"ssl_certificate /tmp/cert.pem;",
		"include /etc/passwd;",
		"location / {",
		"client_max_body_size 10m # comment",
		"proxy_pass http://127.0.0.1;",
	} {
		if _, err := parseCustomDirectives(directiveScopeServer, []string{line}); err == nil {
			t.Fatalf("directive %q must be rejected", line)
		}
	}
	if _, err := parseCustomDirectives(directiveScopeHTTP, []string{"resolver 1.1.1.1;"}); err == nil {
		t.Fatal("http scope must reject resolver override")
	}
	if _, err := parseCustomDirectives(directiveScopeServer, []string{"resolver 1.1.1.1;"}); err != nil {
		t.Fatalf("server scope resolver should be allowed: %v", err)
	}
	var target *panelerr.Error
	if _, err := parseCustomDirectives(directiveScopeServer, []string{"listen 80;"}); !errors.As(err, &target) || target.Code != customDirectivesInvalidCode {
		t.Fatalf("error = %v, want %s", err, customDirectivesInvalidCode)
	}
}

func TestParseCustomDirectivesEnforcesLimits(t *testing.T) {
	lines := make([]string, 0, customDirectivesMaxLines+1)
	for index := 0; index <= customDirectivesMaxLines; index++ {
		lines = append(lines, "add_header X-Test value;")
	}
	if _, err := parseCustomDirectives(directiveScopeServer, lines); err == nil {
		t.Fatal("too many directives must be rejected")
	}
	long := "add_header X-Test " + strings.Repeat("a", customDirectiveMaxLineBytes) + ";"
	if _, err := parseCustomDirectives(directiveScopeServer, []string{long}); err == nil {
		t.Fatal("oversized single directive must be rejected")
	}
}

func TestMergeNginxDirectivesSameNameOverride(t *testing.T) {
	base := []nginxDirective{
		{Name: "listen", Args: "443 ssl"},
		{Name: "http2", Args: "on"},
		{Name: "error_page", Args: "502 504 @panel_upstream_unavailable"},
	}
	custom := []nginxDirective{
		{Name: "http2", Args: "off"},
		{Name: "add_header", Args: "X-One 1"},
		{Name: "add_header", Args: "X-Two 2"},
	}
	merged := mergeNginxDirectives(base, custom)
	text := ""
	for _, directive := range merged {
		text += directive.line() + "\n"
	}
	for _, want := range []string{"listen 443 ssl;", "http2 off;", "add_header X-One 1;", "add_header X-Two 2;", "error_page 502 504 @panel_upstream_unavailable;"} {
		if !strings.Contains(text, want) {
			t.Fatalf("merged directives missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "http2 on;") {
		t.Fatalf("panel-generated http2 must be overridden:\n%s", text)
	}
}

func TestRenderNginxConfigPerDomainTransportSwitches(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	certificates := []proxycert.Certificate{{
		ID:             "example-cert",
		Domains:        []string{"example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}
	facilityDomain := func(https, http2, http3 *bool) ReverseProxyConfig {
		return ReverseProxyConfig{Domains: []FacilityRouteDomain{{
			Domain:          "example.test",
			OriginServerIDs: []string{"srv-a"},
			HTTPSEnabled:    https,
			HTTP2Enabled:    http2,
			HTTP3Enabled:    http3,
			Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
		}}}
	}
	allOn := facilityDomain(boolPtr(true), boolPtr(true), boolPtr(true))
	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", allOn, nil, certificates)
	if err != nil {
		t.Fatal(err)
	}
	text := managedConfigText(files)
	for _, want := range []string{"listen 443 ssl;", "listen 443 quic;", "http2 on;", "http3 on;", "quic_retry on;", `Alt-Svc 'h3=":443"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("all-on config missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ssl_early_data") {
		t.Fatalf("ssl_early_data must not be rendered:\n%s", text)
	}

	// HTTPS 关闭：不生成 443 server，也不下发该域名证书文件。
	httpsOff := facilityDomain(boolPtr(false), boolPtr(true), boolPtr(true))
	_, _, files, err = svc.renderNginxConfig(context.Background(), "srv-a", httpsOff, nil, certificates)
	if err != nil {
		t.Fatal(err)
	}
	text = managedConfigText(files)
	if strings.Contains(text, "listen 443") || strings.Contains(text, "ssl_certificate") {
		t.Fatalf("https disabled must not render a 443 server:\n%s", text)
	}
	if managedFilesContainPrefix(files, "certs/") {
		t.Fatalf("https disabled must not ship certificate files: %#v", files)
	}

	// H2/H3 均关闭：显式 http2 off，且完全不输出 quic/Alt-Svc。
	h2h3Off := facilityDomain(boolPtr(true), boolPtr(false), boolPtr(false))
	_, _, files, err = svc.renderNginxConfig(context.Background(), "srv-a", h2h3Off, nil, certificates)
	if err != nil {
		t.Fatal(err)
	}
	text = managedConfigText(files)
	if !strings.Contains(text, "http2 off;") {
		t.Fatalf("http2 off must be explicit:\n%s", text)
	}
	for _, forbidden := range []string{"listen 443 quic;", "http3 on;", "quic_retry on;", "Alt-Svc"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("h3 disabled config must not contain %q:\n%s", forbidden, text)
		}
	}
}

func TestRenderNginxConfigHTTPPortRedirectOnlyForEffectiveHTTPS(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	certificates := []proxycert.Certificate{{
		ID:             "example-cert",
		Domains:        []string{"example.test"},
		CertificatePEM: "certificate",
		PrivateKeyPEM:  "private-key",
	}}
	cfg := ReverseProxyConfig{
		Settings: FacilityProxySettings{HTTPPortMode: HTTPPortModeRedirect},
		Domains: []FacilityRouteDomain{{
			Domain:          "example.test",
			OriginServerIDs: []string{"srv-a"},
			Paths:           []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
		}},
	}
	_, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", cfg, nil, certificates)
	if err != nil {
		t.Fatal(err)
	}
	text := managedConfigText(files)
	if !strings.Contains(text, "return 301 https://$host$request_uri;") {
		t.Fatalf("redirect mode must redirect port 80 for effective https domains:\n%s", text)
	}
	if strings.Contains(text, "http2 on;") && strings.Count(text, "http2") > 1 {
		t.Fatalf("port 80 server must not enable http2:\n%s", text)
	}

	// 无证书时即使 redirect 策略开启，80 端口仍直接服务内容。
	_, _, files, err = svc.renderNginxConfig(context.Background(), "srv-a", cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text := managedConfigText(files); strings.Contains(text, "https://$host$request_uri") {
		t.Fatalf("redirect must not apply without a certificate:\n%s", text)
	}
}

func TestRenderNginxConfigCustomDirectiveOverridesPanelDirective(t *testing.T) {
	svc := &Service{servers: facilityTestServers{items: map[string]server.Server{}}}
	cfg := ReverseProxyConfig{
		Settings: FacilityProxySettings{
			CustomHTTPDirectives: []string{"client_max_body_size 5m;", "gzip on;"},
		},
		Domains: []FacilityRouteDomain{{
			Domain:                 "example.test",
			OriginServerIDs:        []string{"srv-a"},
			CustomServerDirectives: []string{"client_max_body_size 1m;"},
			Paths:                  []FacilityRoutePath{{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://target.example.test"}},
		}},
	}
	mainConfig, _, files, err := svc.renderNginxConfig(context.Background(), "srv-a", cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainConfig, "client_max_body_size 5m;") || strings.Contains(mainConfig, "client_max_body_size 50m;") {
		t.Fatalf("http custom directive must override panel default:\n%s", mainConfig)
	}
	if !strings.Contains(mainConfig, "gzip on;") {
		t.Fatalf("extra http custom directive must be appended:\n%s", mainConfig)
	}
	text := managedConfigText(files)
	if !strings.Contains(text, "client_max_body_size 1m;") || strings.Contains(text, "client_max_body_size 50m;") {
		t.Fatalf("server custom directive must override panel default:\n%s", text)
	}
}

func TestRouteSummaryReportsTransportSwitches(t *testing.T) {
	certificates := []proxycert.Certificate{{
		ID:      "example-cert",
		Source:  HTTPSDomainCertificate,
		Domains: []string{"example.test"},
	}}
	transport := resolveProxyTransport(boolPtr(false), boolPtr(true), boolPtr(true))
	summary := routeSummary("example.test", "/", "facility", []string{"srv-a"}, certificates, transport, HTTPPortModeRedirect, false, defaultClientMaxBodySize)
	if summary.HTTPSEnabled || !summary.HTTP2Enabled || !summary.HTTP3Enabled || summary.EffectiveHTTPS {
		t.Fatalf("summary = %#v, want switches off/on/on and effective=false", summary)
	}
	if summary.HTTPPortMode != HTTPPortModeServe {
		t.Fatalf("summary httpPortMode = %q, want serve fallback without effective https", summary.HTTPPortMode)
	}
	if summary.HTTPSStatus != HTTPSDisabled || summary.CertificateID != "example-cert" {
		t.Fatalf("summary = %#v, want disabled status with certificate retained", summary)
	}
	summary = routeSummary("example.test", "/", "facility", []string{"srv-a"}, certificates, defaultProxyTransport(), HTTPPortModeRedirect, false, defaultClientMaxBodySize)
	if !summary.EffectiveHTTPS || summary.HTTPSStatus != HTTPSDomainCertificate {
		t.Fatalf("summary = %#v, want effective https", summary)
	}
	if summary.HTTPPortMode != HTTPPortModeRedirect {
		t.Fatalf("summary httpPortMode = %q, want redirect with effective https", summary.HTTPPortMode)
	}
}

func TestFacilityProxySettingsDefaultsAndValidation(t *testing.T) {
	defaults := parseFacilityProxySettings("{}")
	if defaults.HTTPPortMode != HTTPPortModeServe || !defaults.ImageAutoUpdateEnabled || defaults.HSTS.Enabled {
		t.Fatalf("defaults = %#v", defaults)
	}
	partial := parseFacilityProxySettings(`{"httpPortMode":"redirect"}`)
	if partial.HTTPPortMode != HTTPPortModeRedirect || !partial.ImageAutoUpdateEnabled {
		t.Fatalf("partial settings must keep default auto update: %#v", partial)
	}
	disabled := parseFacilityProxySettings(`{"imageAutoUpdateEnabled":false}`)
	if disabled.ImageAutoUpdateEnabled {
		t.Fatalf("explicit false must be preserved: %#v", disabled)
	}
	if _, err := normalizeFacilityProxySettings(FacilityProxySettings{HTTPPortMode: "bogus"}); err == nil {
		t.Fatal("invalid http port mode must be rejected")
	}
	if _, err := normalizeFacilityProxySettings(FacilityProxySettings{
		HTTPPortMode:           HTTPPortModeServe,
		ImageAutoUpdateEnabled: true,
		CustomHTTPDirectives:   []string{"listen 80;"},
	}); err == nil {
		t.Fatal("invalid custom http directive must be rejected on save")
	}
}

func TestSaveProxySettingsRoundTripAndCAS(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()

	config, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{
		HTTPPortMode:           HTTPPortModeRedirect,
		ImageAutoUpdateEnabled: boolPtr(false),
		CustomHTTPDirectives:   []string{"client_max_body_size 8m;"},
		HSTS:                   &HSTSConfig{Enabled: true, MaxAgeSeconds: 600, IncludeSubDomains: true},
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if config.Settings.HTTPPortMode != HTTPPortModeRedirect || config.Settings.ImageAutoUpdateEnabled {
		t.Fatalf("settings = %#v", config.Settings)
	}
	if len(config.Settings.CustomHTTPDirectives) != 1 || config.Settings.CustomHTTPDirectives[0] != "client_max_body_size 8m;" {
		t.Fatalf("custom directives = %#v", config.Settings.CustomHTTPDirectives)
	}
	if !config.Settings.HSTS.Enabled || config.Settings.HSTS.MaxAgeSeconds != 600 || !config.Settings.HSTS.IncludeSubDomains {
		t.Fatalf("hsts = %#v", config.Settings.HSTS)
	}
	if _, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: 99, HTTPPortMode: HTTPPortModeServe}); err == nil {
		t.Fatal("stale version must conflict")
	}
	updated, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: config.Version, HTTPPortMode: HTTPPortModeServe})
	if err != nil {
		t.Fatalf("second save settings: %v", err)
	}
	if updated.Settings.HTTPPortMode != HTTPPortModeServe {
		t.Fatalf("mode = %q, want serve", updated.Settings.HTTPPortMode)
	}
	// 未提交的 imageAutoUpdateEnabled 保留现值 false。
	if updated.Settings.ImageAutoUpdateEnabled {
		t.Fatalf("partial save must preserve object auto update switch: %#v", updated.Settings)
	}
}

func TestImageDigestHelpers(t *testing.T) {
	if got := digestFromReference("nginx@sha256:abcd"); got != "sha256:abcd" {
		t.Fatalf("digest = %q", got)
	}
	if got := digestFromReference("nginx:1.28-alpine"); got != "" {
		t.Fatalf("tag-only reference digest = %q, want empty", got)
	}
	images := []agentcontract.DockerImage{{
		ID:          "sha256:image-id",
		RepoTags:    []string{supportedProxyImage},
		RepoDigests: []string{"nginx@sha256:new"},
	}}
	if got := localTagImageDigest(images, supportedProxyImage); got != "sha256:new" {
		t.Fatalf("local tag digest = %q", got)
	}
	container := &agentcontract.DockerContainer{
		ID:      "container-1",
		Image:   supportedProxyImage,
		ImageID: "sha256:image-id",
	}
	if got := imageDigestForContainer(container, images); got != "sha256:new" {
		t.Fatalf("container digest = %q", got)
	}
	if !sameDigest("SHA256:NEW", "sha256:new") {
		t.Fatal("digest comparison must be case-insensitive")
	}
	if !sameImageID("sha256:abc", "abc") {
		t.Fatal("image id comparison must ignore sha256 prefix")
	}
}

func TestProxyImageStatePersistenceAndProjection(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()
	now := time.Now().UTC()
	if err := orm.Insert(ctx, svc.db, &models.Credential{ID: "cred-1", Name: "cred", Type: "password", Username: "root", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := orm.Insert(ctx, svc.db, &models.Server{ID: "srv-a", Name: "srv-a", Host: "127.0.0.1", Port: 22, CredentialID: "cred-1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if _, err := svc.SaveProxyPlane(ctx, ProxyPlaneSaveInput{DeploymentServers: []string{"srv-a"}}); err != nil {
		t.Fatalf("save proxy plane: %v", err)
	}
	state, err := svc.loadProxyImageState(ctx, "srv-a")
	if err != nil {
		t.Fatalf("load default image state: %v", err)
	}
	if state.Reference != supportedProxyImage || state.Status != ProxyImageStatusUnknown {
		t.Fatalf("default state = %#v", state)
	}
	state.TargetDigest = "sha256:target"
	state.ObservedDigest = "sha256:observed"
	state.Status = ProxyImageStatusUpdateAvailable
	if err := svc.saveProxyImageState(ctx, state); err != nil {
		t.Fatalf("save image state: %v", err)
	}
	status, err := svc.proxyImageStatus(ctx, []string{"srv-a"})
	if err != nil {
		t.Fatalf("image status: %v", err)
	}
	if !status.UpdateAvailable || status.Status != ProxyImageStatusUpdateAvailable || len(status.Servers) != 1 {
		t.Fatalf("status = %#v, want update_available for one gateway", status)
	}
	status.LatestDigest = "sha256:latest"
	if err := svc.updateProxyImageProjection(ctx, status); err != nil {
		t.Fatalf("update projection: %v", err)
	}
	var updateAvailable int
	if err := svc.db.QueryRowContext(ctx, `SELECT image_update_available FROM applications WHERE id=?`, proxyApplicationID).Scan(&updateAvailable); err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if updateAvailable != 1 {
		t.Fatalf("image_update_available = %d, want 1", updateAvailable)
	}
}

func TestProxyImageStatusAggregatesUpdateAvailable(t *testing.T) {
	status := aggregateProxyImageStatus([]string{ProxyImageStatusUpToDate, ProxyImageStatusUpToDate}, false, false)
	if status != ProxyImageStatusUpToDate {
		t.Fatalf("status = %q, want up_to_date", status)
	}
	status = aggregateProxyImageStatus([]string{ProxyImageStatusUpToDate, ProxyImageStatusUpdateAvailable}, true, false)
	if status != ProxyImageStatusUpdateAvailable {
		t.Fatalf("status = %q, want update_available", status)
	}
	status = aggregateProxyImageStatus([]string{ProxyImageStatusFailed, ProxyImageStatusUpdating}, false, false)
	if status != ProxyImageStatusFailed {
		t.Fatalf("status = %q, want failed", status)
	}
}

func TestApplicationRouteTransportUsesResolvedSwitches(t *testing.T) {
	transport := applicationRouteTransport(applications.ReverseProxyRoute{HTTPSEnabled: true, HTTP2Enabled: false, HTTP3Enabled: true})
	if !transport.HTTPS || transport.HTTP2 || !transport.HTTP3 {
		t.Fatalf("transport = %#v", transport)
	}
	defaults := resolveProxyTransport(nil, nil, nil)
	if !defaults.HTTPS || !defaults.HTTP2 || defaults.HTTP3 {
		t.Fatalf("default transport = %#v", defaults)
	}
}

func boolPtr(value bool) *bool { return &value }
