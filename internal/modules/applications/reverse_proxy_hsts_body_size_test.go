package applications

import (
	"context"
	"errors"
	"testing"

	panelerr "panel/internal/platform/errors"
)

// 域名级请求体上限复用与设施一致的 nginx size 语法与 1k..1024m 范围。
func TestNormalizeClientMaxBodySize(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "empty inherits", input: "   ", want: "", wantOK: true},
		{name: "lowercase", input: "50m", want: "50m", wantOK: true},
		{name: "uppercase unit", input: "512K", want: "512k", wantOK: true},
		{name: "bytes within range", input: "2048", want: "2048", wantOK: true},
		{name: "min", input: "1k", want: "1k", wantOK: true},
		{name: "max m", input: "1024m", want: "1024m", wantOK: true},
		{name: "max g", input: "1g", want: "1g", wantOK: true},
		{name: "zero", input: "0", wantOK: false},
		{name: "zero m", input: "0m", wantOK: false},
		{name: "below min", input: "1023", wantOK: false},
		{name: "above max", input: "1025m", wantOK: false},
		{name: "above max g", input: "2g", wantOK: false},
		{name: "bad unit", input: "50mb", wantOK: false},
		{name: "decimal", input: "1.5m", wantOK: false},
		{name: "negative", input: "-1m", wantOK: false},
		{name: "overflow", input: "999999999999999999999999", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeClientMaxBodySize(tc.input)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("NormalizeClientMaxBodySize(%q) = %q,%v want %q,%v", tc.input, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// HSTS 覆盖与请求体上限必须穿过解析/导出链路；nil/空保持继承语义。
func TestReverseProxyRuleCarriesHSTSandBodySize(t *testing.T) {
	hstsOff := false
	size := " 128M "
	rule := resolveReverseProxyRule(ReverseProxyRule{
		Domain:            "app.example.test",
		TargetPort:        8080,
		HSTSEnabled:       &hstsOff,
		ClientMaxBodySize: &size,
	})
	if rule.HSTSEnabled == nil || *rule.HSTSEnabled {
		t.Fatalf("resolved hsts = %#v, want explicit false", rule.HSTSEnabled)
	}
	if rule.ClientMaxBodySize == nil || *rule.ClientMaxBodySize != "128m" {
		t.Fatalf("resolved body size = %#v, want 128m", rule.ClientMaxBodySize)
	}
	route := reverseProxyRouteFromRule(rule, "panel-app-x")
	if route.HSTSEnabled == nil || *route.HSTSEnabled {
		t.Fatalf("route hsts = %#v, want explicit false", route.HSTSEnabled)
	}
	if route.ClientMaxBodySize != "128m" {
		t.Fatalf("route body size = %q, want 128m", route.ClientMaxBodySize)
	}

	inherit := resolveReverseProxyRule(ReverseProxyRule{Domain: "inherit.example.test", TargetPort: 8080})
	if inherit.HSTSEnabled != nil || inherit.ClientMaxBodySize != nil {
		t.Fatalf("inherit rule overrides = %#v/%#v, want nil", inherit.HSTSEnabled, inherit.ClientMaxBodySize)
	}
	inheritRoute := reverseProxyRouteFromRule(inherit, "panel-app-x")
	if inheritRoute.HSTSEnabled != nil || inheritRoute.ClientMaxBodySize != "" {
		t.Fatalf("inherit route overrides = %#v/%q, want nil/empty", inheritRoute.HSTSEnabled, inheritRoute.ClientMaxBodySize)
	}

	empty := ""
	emptyRule := resolveReverseProxyRule(ReverseProxyRule{Domain: "empty.example.test", TargetPort: 8080, ClientMaxBodySize: &empty})
	if emptyRule.ClientMaxBodySize != nil {
		t.Fatalf("empty body size = %#v, want nil inherit", emptyRule.ClientMaxBodySize)
	}
}

func TestReverseProxyClientMaxBodySizeInvalidCode(t *testing.T) {
	size := "2g"
	base := ReverseProxyRule{
		Domain:            "invalid.example.test",
		TargetPort:        8080,
		ClientMaxBodySize: &size,
		Paths:             []ReverseProxyPath{{Path: "/"}},
	}
	assertCode := func(t *testing.T, err error) {
		t.Helper()
		var target *panelerr.Error
		if !errors.As(err, &target) || target.Code != "application_reverse_proxy_client_max_body_size_invalid" {
			t.Fatalf("error = %v, want application_reverse_proxy_client_max_body_size_invalid", err)
		}
	}
	_, err := normalizeReverseProxyRules([]ReverseProxyRule{base})
	assertCode(t, err)

	svc := &Service{}
	_, err = svc.renderReverseProxyRules(context.Background(), []ReverseProxyRule{base}, nil)
	assertCode(t, err)
}

// 整应用保存是 delete+insert：HSTS/请求体上限必须写入同名列并在读取与跨模块
// 导出时完整带回，继承时落空串。
func TestApplicationReverseProxyHSTSandBodySizeRoundTrip(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetReverseProxyPolicyProvider(fakeReverseProxyPolicyProvider{})

	hstsOn := true
	size := "128m"
	app, err := svc.Create(ctx, SaveInput{
		Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain:            "hsts.example.test",
			TargetPort:        8080,
			HSTSEnabled:       &hstsOn,
			ClientMaxBodySize: &size,
			Paths:             []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	rule := findTransportRule(t, app.ReverseProxy, "hsts.example.test")
	if rule.HSTSEnabled == nil || !*rule.HSTSEnabled || rule.ClientMaxBodySize == nil || *rule.ClientMaxBodySize != "128m" {
		t.Fatalf("created overrides = %#v/%#v", rule.HSTSEnabled, rule.ClientMaxBodySize)
	}
	assertHSTSBodySizeColumns(t, svc, app.ID, "hsts.example.test", "on", "128m")

	// 覆盖改为 off + 1g。
	hstsOff := false
	updatedSize := "1G"
	app, err = svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain:            "hsts.example.test",
			TargetPort:        8080,
			HSTSEnabled:       &hstsOff,
			ClientMaxBodySize: &updatedSize,
			Paths:             []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("update application: %v", err)
	}
	rule = findTransportRule(t, app.ReverseProxy, "hsts.example.test")
	if rule.HSTSEnabled == nil || *rule.HSTSEnabled || rule.ClientMaxBodySize == nil || *rule.ClientMaxBodySize != "1g" {
		t.Fatalf("updated overrides = %#v/%#v", rule.HSTSEnabled, rule.ClientMaxBodySize)
	}
	assertHSTSBodySizeColumns(t, svc, app.ID, "hsts.example.test", "off", "1g")

	// 未提交覆盖 -> 落空串并回填 nil，表示继承设施全局。
	app, err = svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain: "hsts.example.test", TargetPort: 8080, Paths: []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("update application with omitted overrides: %v", err)
	}
	rule = findTransportRule(t, app.ReverseProxy, "hsts.example.test")
	if rule.HSTSEnabled != nil || rule.ClientMaxBodySize != nil {
		t.Fatalf("inherit overrides = %#v/%#v, want nil", rule.HSTSEnabled, rule.ClientMaxBodySize)
	}
	assertHSTSBodySizeColumns(t, svc, app.ID, "hsts.example.test", "", "")

	// 跨模块导出必须带出覆盖值（设施 nginx 渲染的唯一输入）。
	app, err = svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: true, SpecYAML: app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain:            "hsts.example.test",
			TargetPort:        8080,
			HSTSEnabled:       &hstsOn,
			ClientMaxBodySize: &size,
			Paths:             []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("update application for export: %v", err)
	}
	configs, err := svc.ApplicationReverseProxyConfigs(ctx)
	if err != nil {
		t.Fatalf("application reverse proxy configs: %v", err)
	}
	for _, cfg := range configs {
		if cfg.ApplicationID != app.ID {
			continue
		}
		for _, route := range cfg.Routes {
			if route.Domain != "hsts.example.test" {
				continue
			}
			if route.HSTSEnabled == nil || !*route.HSTSEnabled || route.ClientMaxBodySize != "128m" {
				t.Fatalf("exported overrides = %#v/%q", route.HSTSEnabled, route.ClientMaxBodySize)
			}
			return
		}
	}
	t.Fatalf("hsts.example.test not exported: %#v", configs)
}

func assertHSTSBodySizeColumns(t *testing.T, svc *Service, appID, domain, hsts, bodySize string) {
	t.Helper()
	var gotHSTS, gotBodySize string
	if err := svc.db.QueryRow(`SELECT hsts_enabled,client_max_body_size FROM reverse_proxy_routes WHERE app_id=? AND domain=?`, appID, domain).
		Scan(&gotHSTS, &gotBodySize); err != nil {
		t.Fatalf("read HSTS/body size columns: %v", err)
	}
	if gotHSTS != hsts || gotBodySize != bodySize {
		t.Fatalf("HSTS/body size columns = %q/%q, want %q/%q", gotHSTS, gotBodySize, hsts, bodySize)
	}
}
