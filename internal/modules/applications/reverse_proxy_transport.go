package applications

import (
	"regexp"
	"strconv"
	"strings"
)

// reverse_proxy_transport.go 收敛应用反向代理规则的“每域名传输开关 +
// 自定义 server 指令”归一化逻辑（FAC-RP-017 / FAC-RP-018）。
//
// 开关在公共保存/API DTO（ReverseProxyRule）里用 *bool 表达“未提交”：
// nil 不能被当成 false，否则旧客户端或旧编辑会话 draft 只要保存一次就会
// 悄悄关闭 HTTPS/HTTP2。渲染/跨模块导出结构（ReverseProxyRoute）使用解析后
// 的 bool，因此持久化与渲染都必须先经过这里的解析。

// reverseProxyRuleTransportSwitches 返回解析后的每域名传输开关；
// nil 指针表示客户端未提交，默认 HTTPS/HTTP2 开、HTTP3 关。
func reverseProxyRuleTransportSwitches(rule ReverseProxyRule) (https, http2, http3 bool) {
	https, http2, http3 = true, true, false
	if rule.HTTPSEnabled != nil {
		https = *rule.HTTPSEnabled
	}
	if rule.HTTP2Enabled != nil {
		http2 = *rule.HTTP2Enabled
	}
	if rule.HTTP3Enabled != nil {
		http3 = *rule.HTTP3Enabled
	}
	return https, http2, http3
}

// boolPtr 返回 value 的指针，便于把解析后的开关写回 *bool DTO 字段。
func boolPtr(value bool) *bool {
	return &value
}

// normalizeReverseProxyServerDirectives 逐行 trim 并丢弃空行，保留用户顺序。
// 这里只做存储/哈希稳定性归一化；禁项、上限与“同名覆盖”由设施渲染层负责
// （applications 不得复制一份设施校验逻辑，否则两处判定会漂移）。
func normalizeReverseProxyServerDirectives(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// normalizeReverseProxyHTTPPortMode 归一化每域名 80 端口策略覆盖：
// 空/未提交=继承全局；只允许 serve/redirect，其他值视为非法。
func normalizeReverseProxyHTTPPortMode(value *string) (string, bool) {
	if value == nil {
		return "", true
	}
	mode := strings.ToLower(strings.TrimSpace(*value))
	switch mode {
	case "", "serve", "redirect":
		return mode, true
	default:
		return "", false
	}
}

func reverseProxyHTTPPortModePtr(mode string) *string {
	if mode == "" {
		return nil
	}
	return &mode
}

func derefReverseProxyHTTPPortMode(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// clientMaxBodySizePattern 校验域名级 nginx size 语法；与全局设置不同，域名
// 覆盖额外限定在 1k..1024m（见 NormalizeClientMaxBodySize）。
var clientMaxBodySizePattern = regexp.MustCompile(`^(\d+)([kKmMgG]?)$`)

const (
	minClientMaxBodySizeBytes = 1 << 10
	maxClientMaxBodySizeBytes = 1 << 30
)

// NormalizeClientMaxBodySize 归一化并校验 nginx client_max_body_size 值：
// 空串表示继承；非空必须匹配 ^\d+[kKmMgG]?$ 且换算后落在 1k..1024m。
// 设施域名与应用反代规则共用该校验，避免两侧判定漂移。
func NormalizeClientMaxBodySize(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return "", true
	}
	match := clientMaxBodySizePattern.FindStringSubmatch(normalized)
	if match == nil {
		return "", false
	}
	amount, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return "", false
	}
	multiplier := uint64(1)
	switch match[2] {
	case "k":
		multiplier = 1 << 10
	case "m":
		multiplier = 1 << 20
	case "g":
		multiplier = 1 << 30
	}
	if amount > maxClientMaxBodySizeBytes/multiplier {
		return "", false
	}
	bytes := amount * multiplier
	if bytes < minClientMaxBodySizeBytes || bytes > maxClientMaxBodySizeBytes {
		return "", false
	}
	return normalized, true
}

// normalizedReverseProxyClientMaxBodySize 把 DTO 覆盖值归一化：nil/空白=继承。
func normalizedReverseProxyClientMaxBodySize(value *string) (string, bool) {
	if value == nil {
		return "", true
	}
	return NormalizeClientMaxBodySize(*value)
}

// reverseProxyClientMaxBodySizePtr 把归一化值写回 DTO：空=继承为 nil。
func reverseProxyClientMaxBodySizePtr(value string) *string {
	normalized, ok := NormalizeClientMaxBodySize(value)
	if !ok || normalized == "" {
		return nil
	}
	return &normalized
}

// derefReverseProxyClientMaxBodySize 返回持久化/跨模块使用的归一化值（空=继承）。
func derefReverseProxyClientMaxBodySize(value *string) string {
	normalized, ok := normalizedReverseProxyClientMaxBodySize(value)
	if !ok {
		return ""
	}
	return normalized
}

// storedReverseProxyHSTS 把 HSTS 覆盖压平成空串/on/off 持久化形式。
func storedReverseProxyHSTS(value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return "on"
	}
	return "off"
}

// reverseProxyHSTSOverridePtr 把持久化的空串/on/off 转回 DTO：空/脏值=nil 继承。
func reverseProxyHSTSOverridePtr(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on":
		enabled := true
		return &enabled
	case "off":
		disabled := false
		return &disabled
	default:
		return nil
	}
}

// cloneReverseProxyHSTS 复制 HSTS 覆盖指针，避免调用方共享可变状态。
func cloneReverseProxyHSTS(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// resolveReverseProxyRule 返回把开关解析成显式指针、自定义指令归一化后的副本。
// 持久化映射（reverseProxyRuleToModel）、渲染映射（renderReverseProxyRules）
// 与导出映射（ApplicationReverseProxyConfigs）共用它，确保 nil/默认值不会
// 在保存往返中产生不同的 JSON 或哈希。
func resolveReverseProxyRule(rule ReverseProxyRule) ReverseProxyRule {
	https, http2, http3 := reverseProxyRuleTransportSwitches(rule)
	rule.HTTPSEnabled = boolPtr(https)
	rule.HTTP2Enabled = boolPtr(http2)
	rule.HTTP3Enabled = boolPtr(http3)
	rule.CustomServerDirectives = normalizeReverseProxyServerDirectives(rule.CustomServerDirectives)
	if mode, ok := normalizeReverseProxyHTTPPortMode(rule.HTTPPortMode); ok {
		rule.HTTPPortMode = reverseProxyHTTPPortModePtr(mode)
	}
	rule.HSTSEnabled = cloneReverseProxyHSTS(rule.HSTSEnabled)
	if size, ok := normalizedReverseProxyClientMaxBodySize(rule.ClientMaxBodySize); ok {
		rule.ClientMaxBodySize = reverseProxyClientMaxBodySizePtr(size)
	}
	return rule
}

// resolveAutoUpdateEnabled 解析对象级“允许自动更新”开关：nil 表示调用方未提交，
// 创建时回落到 true、更新时回落到当前值。
func resolveAutoUpdateEnabled(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// reverseProxyRouteFromRule 把规则映射成渲染/跨模块导出结构，显式带出解析后的
// 每域名传输开关与自定义 server 指令。ApplicationReverseProxyConfigs 是设施
// nginx 渲染的唯一输入，漏字段会让每域名 HTTPS/HTTP2/HTTP3 配置整体丢失。
func reverseProxyRouteFromRule(rule ReverseProxyRule, targetContainer string) ReverseProxyRoute {
	https, http2, http3 := reverseProxyRuleTransportSwitches(rule)
	mode, _ := normalizeReverseProxyHTTPPortMode(rule.HTTPPortMode)
	size, _ := normalizedReverseProxyClientMaxBodySize(rule.ClientMaxBodySize)
	return ReverseProxyRoute{
		Domain:                 rule.Domain,
		TargetPort:             rule.TargetPort,
		TargetContainer:        targetContainer,
		OriginServerIDs:        append([]string(nil), rule.OriginServerIDs...),
		AnyAccess:              rule.AnyAccess,
		Paths:                  append([]ReverseProxyPath(nil), rule.Paths...),
		HTTPSEnabled:           https,
		HTTP2Enabled:           http2,
		HTTP3Enabled:           http3,
		CustomServerDirectives: normalizeReverseProxyServerDirectives(rule.CustomServerDirectives),
		HTTPPortMode:           mode,
		HSTSEnabled:            cloneReverseProxyHSTS(rule.HSTSEnabled),
		ClientMaxBodySize:      size,
	}
}
