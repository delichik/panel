package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"panel/internal/platform/database/orm"
)

// 每域名传输开关（FAC-RP-017）在公共规则 DTO 中用 *bool 表达“未提交”，
// 归一化必须显式解析为默认值：HTTPS/HTTP2 开、HTTP3 关。
func TestNormalizeReverseProxyRulesResolvesTransportDefaults(t *testing.T) {
	off, on := false, true
	rules, err := normalizeReverseProxyRules([]ReverseProxyRule{
		{Domain: "default.example.test", TargetPort: 80, Paths: []ReverseProxyPath{{Path: "/"}}},
		{
			Domain: "custom.example.test", TargetPort: 81,
			HTTPSEnabled: &on, HTTP2Enabled: &off, HTTP3Enabled: &on,
			CustomServerDirectives: []string{"  client_max_body_size 10m;  ", "   ", "proxy_read_timeout 30s;"},
			Paths:                  []ReverseProxyPath{{Path: "/"}},
		},
	})
	if err != nil {
		t.Fatalf("normalize reverse proxy rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %#v", rules)
	}
	assertTransport(t, "default.example.test", rules[0], true, true, false)
	assertTransport(t, "custom.example.test", rules[1], true, false, true)
	if len(rules[0].CustomServerDirectives) != 0 {
		t.Fatalf("default rule custom directives = %#v, want empty", rules[0].CustomServerDirectives)
	}
	got := rules[1].CustomServerDirectives
	if len(got) != 2 || got[0] != "client_max_body_size 10m;" || got[1] != "proxy_read_timeout 30s;" {
		t.Fatalf("custom directives not trimmed/dropped as expected: %#v", got)
	}
}

func assertTransport(t *testing.T, label string, rule ReverseProxyRule, https, http2, http3 bool) {
	t.Helper()
	if rule.HTTPSEnabled == nil || *rule.HTTPSEnabled != https {
		t.Fatalf("%s httpsEnabled = %#v, want %v", label, rule.HTTPSEnabled, https)
	}
	if rule.HTTP2Enabled == nil || *rule.HTTP2Enabled != http2 {
		t.Fatalf("%s http2Enabled = %#v, want %v", label, rule.HTTP2Enabled, http2)
	}
	if rule.HTTP3Enabled == nil || *rule.HTTP3Enabled != http3 {
		t.Fatalf("%s http3Enabled = %#v, want %v", label, rule.HTTP3Enabled, http3)
	}
}

func findTransportRule(t *testing.T, rules []ReverseProxyRule, domain string) ReverseProxyRule {
	t.Helper()
	for _, rule := range rules {
		if rule.Domain == domain {
			return rule
		}
	}
	t.Fatalf("reverse proxy rule for %s not found in %#v", domain, rules)
	return ReverseProxyRule{}
}

// 应用保存往返：整应用保存是 delete+insert，三开关与自定义 server 指令必须
// 完整写回并读回，nil 提交按默认值归一化（FAC-RP-017/FAC-RP-018）。
func TestApplicationReverseProxyTransportRoundTrip(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetReverseProxyPolicyProvider(fakeReverseProxyPolicyProvider{})

	off, on := false, true
	app, err := svc.Create(ctx, SaveInput{
		Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain: "transport.example.test", TargetPort: 8080,
			HTTPSEnabled: &on, HTTP2Enabled: &off, HTTP3Enabled: &on,
			CustomServerDirectives: []string{"client_max_body_size 10m;", "proxy_read_timeout 30s;"},
			Paths:                  []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	rule := findTransportRule(t, app.ReverseProxy, "transport.example.test")
	assertTransport(t, "create", rule, true, false, true)
	if len(rule.CustomServerDirectives) != 2 {
		t.Fatalf("create directives = %#v", rule.CustomServerDirectives)
	}
	assertReverseProxyTransportColumns(t, svc, app.ID, "transport.example.test", 1, 0, 1, 2)

	// 更新为另一组值：HTTP2 打开、HTTP3 关闭、自定义指令替换。
	app, err = svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain: "transport.example.test", TargetPort: 8080,
			HTTPSEnabled: boolPtr(true), HTTP2Enabled: boolPtr(true), HTTP3Enabled: boolPtr(false),
			CustomServerDirectives: []string{"proxy_buffering off;"},
			Paths:                  []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("update application: %v", err)
	}
	rule = findTransportRule(t, app.ReverseProxy, "transport.example.test")
	assertTransport(t, "update", rule, true, true, false)
	if len(rule.CustomServerDirectives) != 1 || rule.CustomServerDirectives[0] != "proxy_buffering off;" {
		t.Fatalf("update directives = %#v", rule.CustomServerDirectives)
	}
	assertReverseProxyTransportColumns(t, svc, app.ID, "transport.example.test", 1, 1, 0, 1)

	// 客户端未提交三开关（nil）→ 服务端按默认值归一化并落库。
	app, err = svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain: "transport.example.test", TargetPort: 8080, Paths: []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("update application with omitted switches: %v", err)
	}
	rule = findTransportRule(t, app.ReverseProxy, "transport.example.test")
	assertTransport(t, "defaults", rule, true, true, false)
	if len(rule.CustomServerDirectives) != 0 {
		t.Fatalf("omitted directives = %#v, want empty", rule.CustomServerDirectives)
	}
	assertReverseProxyTransportColumns(t, svc, app.ID, "transport.example.test", 1, 1, 0, 0)
}

func assertReverseProxyTransportColumns(t *testing.T, svc *Service, appID, domain string, https, http2, http3, directives int) {
	t.Helper()
	var gotHTTPS, gotHTTP2, gotHTTP3 int
	var directivesJSON string
	if err := svc.db.QueryRow(`SELECT https_enabled,http2_enabled,http3_enabled,custom_server_directives FROM reverse_proxy_routes WHERE app_id=? AND domain=?`, appID, domain).
		Scan(&gotHTTPS, &gotHTTP2, &gotHTTP3, &directivesJSON); err != nil {
		t.Fatalf("read transport columns for %s/%s: %v", appID, domain, err)
	}
	if gotHTTPS != https || gotHTTP2 != http2 || gotHTTP3 != http3 {
		t.Fatalf("transport columns = %d/%d/%d, want %d/%d/%d", gotHTTPS, gotHTTP2, gotHTTP3, https, http2, http3)
	}
	var lines []string
	if directivesJSON == "" {
		directivesJSON = "[]"
	}
	if err := json.Unmarshal([]byte(directivesJSON), &lines); err != nil {
		t.Fatalf("custom_server_directives %q is not a JSON array of strings: %v", directivesJSON, err)
	}
	if len(lines) != directives {
		t.Fatalf("custom_server_directives = %#v, want %d lines", lines, directives)
	}
}

// 整应用替换函数直接回归：delete+insert 不得丢三开关与自定义指令，也不得
// 依赖 DB 默认值把用户配置悄悄改成默认。
func TestReplaceApplicationReverseProxyRoutesPersistsTransportFields(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	const appID = "app-transport-fields"

	replace := func(rules []ReverseProxyRule) {
		t.Helper()
		if err := orm.WithTx(ctx, svc.db, func(tx *sql.Tx) error {
			return replaceApplicationReverseProxyRoutes(ctx, tx, appID, rules)
		}); err != nil {
			t.Fatalf("replace reverse proxy routes: %v", err)
		}
	}

	replace([]ReverseProxyRule{{
		Domain: "transport.example.test", TargetPort: 8080,
		HTTPSEnabled: boolPtr(false), HTTP2Enabled: boolPtr(true), HTTP3Enabled: boolPtr(true),
		CustomServerDirectives: []string{"client_max_body_size 20m;"},
		Paths:                  []ReverseProxyPath{{Path: "/"}},
	}})
	rules, err := svc.loadReverseProxyRoutes(ctx, appID)
	if err != nil {
		t.Fatalf("load reverse proxy routes: %v", err)
	}
	rule := findTransportRule(t, rules, "transport.example.test")
	assertTransport(t, "replace", rule, false, true, true)
	if len(rule.CustomServerDirectives) != 1 || rule.CustomServerDirectives[0] != "client_max_body_size 20m;" {
		t.Fatalf("replace directives = %#v", rule.CustomServerDirectives)
	}

	// 改回默认（nil）时显式写入默认值，而不是依赖列默认值。
	replace([]ReverseProxyRule{{Domain: "transport.example.test", TargetPort: 8080, Paths: []ReverseProxyPath{{Path: "/"}}}})
	rules, err = svc.loadReverseProxyRoutes(ctx, appID)
	if err != nil {
		t.Fatalf("reload reverse proxy routes: %v", err)
	}
	rule = findTransportRule(t, rules, "transport.example.test")
	assertTransport(t, "replace defaults", rule, true, true, false)
	if len(rule.CustomServerDirectives) != 0 {
		t.Fatalf("replace default directives = %#v, want empty", rule.CustomServerDirectives)
	}
}

// ApplicationReverseProxyConfigs 是设施 nginx 渲染的唯一输入，必须把解析后的
// 每域名开关与自定义指令完整带出（FAC-RP-017/FAC-RP-018）。
func TestApplicationReverseProxyConfigsCarriesTransportFields(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetReverseProxyPolicyProvider(fakeReverseProxyPolicyProvider{})

	app, err := svc.Create(ctx, SaveInput{
		Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy: []ReverseProxyRule{{
			Domain: "configs.example.test", TargetPort: 8080,
			HTTPSEnabled: boolPtr(true), HTTP2Enabled: boolPtr(false), HTTP3Enabled: boolPtr(true),
			CustomServerDirectives: []string{"  proxy_read_timeout 45s;  "},
			Paths:                  []ReverseProxyPath{{Path: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	configs, err := svc.ApplicationReverseProxyConfigs(ctx)
	if err != nil {
		t.Fatalf("application reverse proxy configs: %v", err)
	}
	var route *ReverseProxyRoute
	for i := range configs {
		if configs[i].ApplicationID != app.ID {
			continue
		}
		for j := range configs[i].Routes {
			if configs[i].Routes[j].Domain == "configs.example.test" {
				route = &configs[i].Routes[j]
			}
		}
	}
	if route == nil {
		t.Fatalf("route configs.example.test not exported in %#v", configs)
	}
	if !route.HTTPSEnabled || route.HTTP2Enabled || !route.HTTP3Enabled {
		t.Fatalf("exported transport switches = %v/%v/%v, want true/false/true", route.HTTPSEnabled, route.HTTP2Enabled, route.HTTP3Enabled)
	}
	if len(route.CustomServerDirectives) != 1 || route.CustomServerDirectives[0] != "proxy_read_timeout 45s;" {
		t.Fatalf("exported custom directives = %#v", route.CustomServerDirectives)
	}
	if route.TargetContainer != runtimeContainerName(app) {
		t.Fatalf("exported target container = %q", route.TargetContainer)
	}
}
