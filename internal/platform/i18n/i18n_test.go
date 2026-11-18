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

func TestTranslateLocalePreservesCloudflareAPIErrorDetails(t *testing.T) {
	got := TranslateLocale(LocaleSimplifiedChinese, "cloudflare_api_error", "Cloudflare API error: 1004: DNS Validation Error")
	if !strings.HasPrefix(got, "Cloudflare API 错误 ") || !strings.Contains(got, "1004: DNS Validation Error") {
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

func TestTranslateLocaleCoversNetworkProbeValidation(t *testing.T) {
	tests := map[string]string{
		"network_probe_mode_invalid":         "网络探测模式必须为 all 或 selected",
		"network_probe_servers_required":     "选择指定节点模式时必须至少选择一个探测节点",
		"network_probe_server_not_found":     "所选探测节点不存在",
		"network_probe_interval_invalid":     "探测间隔必须在 1 到 60 分钟之间",
		"network_probe_ping_count_invalid":   "每次探测的 ping 包数必须在 1 到 10 之间",
		"network_probe_ping_timeout_invalid": "单包超时必须在 1 到 10 秒之间",
	}
	for code, want := range tests {
		if got := TranslateLocale(LocaleSimplifiedChinese, code, "fallback"); got != want {
			t.Fatalf("translation for %s = %q, want %q", code, got, want)
		}
	}
}

// TestTranslateLocaleCoversAgentTimeSyncFailure pins the zh-CN message of the
// agent deployment clock-synchronization prerequisite error (AGT-DEP-009).
func TestTranslateLocaleCoversAgentTimeSyncFailure(t *testing.T) {
	if got := TranslateLocale(LocaleSimplifiedChinese, "agent_time_sync_failed", "fallback"); got != "目标服务器时钟同步失败，详情见任务日志" {
		t.Fatalf("translation for agent_time_sync_failed = %q", got)
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

// TestTranslateLocaleCoversAppearanceErrors pins the nine zh-CN messages of the
// appearance settings/background upload error codes.
func TestTranslateLocaleCoversAppearanceErrors(t *testing.T) {
	tests := map[string]string{
		"invalid_theme_mode":             "主题模式必须为 system、light 或 dark",
		"invalid_appearance_accent":      "强调色必须是受支持的预设之一",
		"invalid_background_kind":        "背景类型必须为 none、preset 或 image，且预设 id 必须有效",
		"background_required":            "缺少背景图片",
		"background_too_large":           "背景图片不能超过 8 MiB",
		"background_unsupported_type":    "背景图片只支持 JPEG 或 PNG 格式",
		"background_invalid_image":       "背景图片无效或已损坏",
		"background_dimensions_exceeded": "背景图片不能超过 4096×4096 或 1200 万像素",
		"background_store_failed":        "保存背景图片失败",
	}
	for code, want := range tests {
		if got := TranslateLocale(LocaleSimplifiedChinese, code, "fallback"); got != want {
			t.Fatalf("translation for %s = %q, want %q", code, got, want)
		}
	}
}
