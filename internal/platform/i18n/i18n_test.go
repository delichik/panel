package i18n

import (
	"strings"
	"testing"
)

func TestTranslateLocalePreservesRuntimeErrorDetails(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "application_runtime_operation_failed", "Application runtime operation failed: create container failed")
	if !strings.HasPrefix(got, "应用运行时操作失败：") || !strings.Contains(got, "create container failed") {
		t.Fatalf("translation = %q", got)
	}
}

func TestTranslateLocaleUsesSpecificRuntimeDeploymentPrefix(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "application_runtime_operation_failed", "Application runtime operation failed: deployment failed on 1 of 2 targets: srv-a: agent down")
	if !strings.HasPrefix(got, "应用运行时操作失败：部署失败目标 1 of 2 targets") || !strings.Contains(got, "srv-a: agent down") {
		t.Fatalf("translation = %q", got)
	}
}

func TestTranslateLocaleUsesCodeSpecificPrefix(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "cloudflare_unreachable", "Cloudflare API unreachable: dial tcp timeout")
	if !strings.HasPrefix(got, "无法访问 Cloudflare API：") || !strings.Contains(got, "dial tcp timeout") {
		t.Fatalf("translation = %q", got)
	}
}

func TestTranslateLocaleCoversReverseProxyFacilityValidation(t *testing.T) {
	tests := map[string]string{
		"facility_domain_servers_required":              "每个域名必须至少选择一个入口节点",
		"facility_upstream_domain_application_conflict": "上游模式域名由反向代理设施独占，不能与普通应用共用",
		"facility_reverse_proxy_config_changed":         "反向代理设施配置已被其他操作修改，请重新加载后再保存",
		"reverse_proxy_header_name_invalid":             "自定义 Header 名称无效",
	}
	for code, want := range tests {
		if got := TranslateLocale(LocaleSimplifiedChinese, code, "fallback"); got != want {
			t.Fatalf("translation for %s = %q, want %q", code, got, want)
		}
	}
}

func TestTranslateLocaleCoversNewErrorCodes(t *testing.T) {
	tests := map[string]string{
		"application_deletion_requested": "已请求删除该应用",
		"task_cancel_unsupported":        "该任务类型不能取消",
	}
	for code, want := range tests {
		if got := TranslateLocale(LocaleSimplifiedChinese, code, "fallback"); got != want {
			t.Fatalf("translation for %s = %q, want %q", code, got, want)
		}
	}
}

func TestTranslateLocaleCoversNewExactMessages(t *testing.T) {
	tests := []struct{ code, message, want string }{
		{"not_found", "certificate not found", "证书不存在"},
		{"not_found", "agent resource not found", "Agent 资源不存在"},
		{"remote_timeout", "Remote command timed out", "远程命令超时"},
	}
	for _, tt := range tests {
		if got := TranslateLocale(LocaleSimplifiedChinese, tt.code, tt.message); got != tt.want {
			t.Fatalf("translation for %q = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestTranslateLocaleUsesAgentRequestInvalidPrefix(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "agent_request_invalid", "Agent request invalid: bad input")
	if got != "Agent 请求无效：bad input" {
		t.Fatalf("translation = %q", got)
	}
}