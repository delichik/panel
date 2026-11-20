package applications

import "testing"

// 每域名 80 端口策略覆盖：未提交/空=继承设施全局；只允许 serve|redirect；
// 持久化与跨模块导出必须带出解析值，非法值被拒绝。
func TestReverseProxyHTTPPortModeNormalization(t *testing.T) {
	cases := []struct {
		name    string
		input   *string
		want    string
		wantErr bool
	}{
		{name: "omitted inherits", input: nil, want: ""},
		{name: "empty inherits", input: strPtr("  "), want: ""},
		{name: "serve", input: strPtr(" SErve "), want: "serve"},
		{name: "redirect", input: strPtr("redirect"), want: "redirect"},
		{name: "invalid", input: strPtr("proxy"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeReverseProxyHTTPPortMode(tc.input)
			if tc.wantErr {
				if ok {
					t.Fatalf("normalize = %q ok, want error", got)
				}
				return
			}
			if !ok || got != tc.want {
				t.Fatalf("normalize = %q,%v want %q", got, ok, tc.want)
			}
		})
	}
}

func TestReverseProxyRuleCarriesHTTPPortMode(t *testing.T) {
	mode := "redirect"
	rule := resolveReverseProxyRule(ReverseProxyRule{Domain: "app.example.test", TargetPort: 8080, HTTPPortMode: &mode})
	if rule.HTTPPortMode == nil || *rule.HTTPPortMode != "redirect" {
		t.Fatalf("resolved mode = %#v", rule.HTTPPortMode)
	}
	route := reverseProxyRouteFromRule(rule, "panel-app-x")
	if route.HTTPPortMode != "redirect" {
		t.Fatalf("route mode = %q", route.HTTPPortMode)
	}
}

func strPtr(value string) *string { return &value }
