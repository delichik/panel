package facilityapps

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"panel/internal/modules/applications"
	panelerr "panel/internal/platform/errors"
)

const (
	// 80 端口策略（FAC-RP-016）：serve 保持现状直接服务内容，redirect 仅对
	// "HTTPS 开关开且有证书"的域名把 HTTP 请求 301 到 HTTPS。
	HTTPPortModeServe    = "serve"
	HTTPPortModeRedirect = "redirect"

	defaultHSTSMaxAgeSeconds = 31536000
	maxHSTSMaxAgeSeconds     = 63072000

	// 入口代理镜像滚动更新在相邻网关之间的等待秒数（FAC-RP-015）。默认 0
	// 表示不等待；上限 600 秒，避免误配置把滚动任务拖成小时级。
	defaultImageUpdateIntervalSeconds = 0
	maxImageUpdateIntervalSeconds     = 600

	// P1 全局安全参数（FAC-RP-016）：server_tokens 默认关闭，避免泄露 nginx
	// 版本；client_max_body_size 默认 50m；TLS 最低版本默认 1.2。
	ServerTokensOn  = "on"
	ServerTokensOff = "off"

	TLSMinVersion12 = "1.2"
	TLSMinVersion13 = "1.3"

	defaultClientMaxBodySize = "50m"
	defaultServerTokens      = ServerTokensOff
	defaultTLSMinVersion     = TLSMinVersion12
)

// nginxSizePattern 校验 nginx size 语法（如 50m / 512k / 1g，正整数 + 可选单位），
// 不接受空格、小数、0 与裸 "m" 等非法值。
var nginxSizePattern = regexp.MustCompile(`^[1-9][0-9]{0,9}[kKmMgG]?$`)

// nginxDigestPattern 校验显式回滚 digest 的 sha256 形状。
var nginxDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// HSTSConfig 是入口代理 443 server 的 HSTS 输出配置（P1 安全参数）。
type HSTSConfig struct {
	Enabled           bool `json:"enabled"`
	MaxAgeSeconds     int  `json:"maxAgeSeconds"`
	IncludeSubDomains bool `json:"includeSubDomains"`
}

// FacilityProxySettings 是入口代理的全局运行参数，持久化在
// facility_app_configs.settings_json；GET 反代配置时随配置一起返回。
type FacilityProxySettings struct {
	HTTPPortMode string `json:"httpPortMode"`
	// ImageAutoUpdateEnabled 是入口代理镜像的对象级"允许自动更新"开关
	// （默认 true）。关闭只阻止自动更新，手动检查/更新仍可用。
	ImageAutoUpdateEnabled bool `json:"imageAutoUpdateEnabled"`
	// ImageUpdateIntervalSeconds 是滚动更新在相邻网关之间的等待秒数
	// （0..600，默认 0=不等待），用于人工观察/灰度。
	ImageUpdateIntervalSeconds int `json:"imageUpdateIntervalSeconds"`
	// CustomHTTPDirectives 是 http 块顶层自定义 nginx 单行指令（FAC-RP-018）。
	CustomHTTPDirectives []string   `json:"customHttpDirectives"`
	HSTS                 HSTSConfig `json:"hsts"`
	// P1 全局安全参数：server_tokens（on/off，默认 off）、client_max_body_size
	// （nginx size 语法，默认 50m）与 TLS 最低版本（1.2/1.3，默认 1.2）。
	ServerTokens      string `json:"serverTokens"`
	ClientMaxBodySize string `json:"clientMaxBodySize"`
	TLSMinVersion     string `json:"tlsMinVersion"`
}

// proxySettingsDocument 是 settings_json 的持久化形状。指针字段用于区分
// "未写入过"与显式 false（旧库空设置必须归一化为默认值）。
type proxySettingsDocument struct {
	HTTPPortMode           string `json:"httpPortMode,omitempty"`
	ImageAutoUpdateEnabled *bool  `json:"imageAutoUpdateEnabled,omitempty"`
	// ImageUpdateIntervalSeconds 用指针区分"未写入过"与显式 0：旧库缺失该
	// 字段时必须回退默认 0，而不是被解析成其他值。
	ImageUpdateIntervalSeconds *int          `json:"imageUpdateIntervalSeconds,omitempty"`
	CustomHTTPDirectives       []string      `json:"customHttpDirectives,omitempty"`
	HSTS                       *hstsDocument `json:"hsts,omitempty"`
	ServerTokens               string        `json:"serverTokens,omitempty"`
	ClientMaxBodySize          string        `json:"clientMaxBodySize,omitempty"`
	TLSMinVersion              string        `json:"tlsMinVersion,omitempty"`
}

type hstsDocument struct {
	Enabled           bool `json:"enabled"`
	MaxAgeSeconds     int  `json:"maxAgeSeconds"`
	IncludeSubDomains bool `json:"includeSubDomains"`
}

// ProxySettingsSaveInput 是 PUT /facility-apps/reverse-proxy/settings 的负载。
// imageAutoUpdateEnabled 用指针表达"未提交则保留现值"；HTTP 策略与 P1 安全
// 参数的空白字符串同样表示"保留现值"（兼容只提交 P0 字段的旧客户端）。
type ProxySettingsSaveInput struct {
	Version                int    `json:"version,omitempty"`
	HTTPPortMode           string `json:"httpPortMode"`
	ImageAutoUpdateEnabled *bool  `json:"imageAutoUpdateEnabled,omitempty"`
	// ImageUpdateIntervalSeconds 为 nil 表示未提交（保留现值）；显式 0 表示
	// 关闭相邻网关之间的等待。
	ImageUpdateIntervalSeconds *int        `json:"imageUpdateIntervalSeconds,omitempty"`
	CustomHTTPDirectives       []string    `json:"customHttpDirectives"`
	HSTS                       *HSTSConfig `json:"hsts,omitempty"`
	ServerTokens               string      `json:"serverTokens,omitempty"`
	ClientMaxBodySize          string      `json:"clientMaxBodySize,omitempty"`
	TLSMinVersion              string      `json:"tlsMinVersion,omitempty"`
}

func defaultFacilityProxySettings() FacilityProxySettings {
	return FacilityProxySettings{
		HTTPPortMode:               HTTPPortModeServe,
		ImageAutoUpdateEnabled:     true,
		ImageUpdateIntervalSeconds: defaultImageUpdateIntervalSeconds,
		CustomHTTPDirectives:       []string{},
		HSTS: HSTSConfig{
			Enabled:           false,
			MaxAgeSeconds:     defaultHSTSMaxAgeSeconds,
			IncludeSubDomains: false,
		},
		ServerTokens:      defaultServerTokens,
		ClientMaxBodySize: defaultClientMaxBodySize,
		TLSMinVersion:     defaultTLSMinVersion,
	}
}

// clampImageUpdateInterval 把滚动更新间隔收敛到 0..600 秒。用于 GET/解析旧库
// 脏值时兜底，保存路径的越界错误由 normalizeFacilityProxySettings 显式返回。
func clampImageUpdateInterval(value int) int {
	if value < 0 {
		return 0
	}
	if value > maxImageUpdateIntervalSeconds {
		return maxImageUpdateIntervalSeconds
	}
	return value
}

// resolved 把零值/空值归一化为默认值。渲染与 GET 响应都走这里，保证
// 不同入口（旧测试直接构造零值 ReverseProxyConfig）看到同一份默认设置。
func (s FacilityProxySettings) resolved() FacilityProxySettings {
	defaults := defaultFacilityProxySettings()
	out := s
	if strings.TrimSpace(out.HTTPPortMode) == "" {
		out.HTTPPortMode = defaults.HTTPPortMode
	}
	// 零值结构无法表达"未写入"，由 loadConfig 在反序列化时已归一化 true；
	// 这里仅在 HSTS 未配置时补默认值。
	if out.HSTS.MaxAgeSeconds <= 0 {
		out.HSTS.MaxAgeSeconds = defaults.HSTS.MaxAgeSeconds
	}
	if out.CustomHTTPDirectives == nil {
		out.CustomHTTPDirectives = []string{}
	}
	if strings.TrimSpace(out.ServerTokens) == "" {
		out.ServerTokens = defaults.ServerTokens
	}
	if strings.TrimSpace(out.ClientMaxBodySize) == "" {
		out.ClientMaxBodySize = defaults.ClientMaxBodySize
	}
	if strings.TrimSpace(out.TLSMinVersion) == "" {
		out.TLSMinVersion = defaults.TLSMinVersion
	}
	out.ImageUpdateIntervalSeconds = clampImageUpdateInterval(out.ImageUpdateIntervalSeconds)
	return out
}

// normalizeFacilityProxySettings 校验并归一化一份完整设置（保存路径）。
func normalizeFacilityProxySettings(in FacilityProxySettings) (FacilityProxySettings, error) {
	out := in.resolved()
	out.HTTPPortMode = strings.ToLower(strings.TrimSpace(out.HTTPPortMode))
	switch out.HTTPPortMode {
	case HTTPPortModeServe, HTTPPortModeRedirect:
	default:
		return FacilityProxySettings{}, panelerr.Validation("facility_http_port_mode_invalid", "HTTP port mode must be serve or redirect")
	}
	if out.HSTS.MaxAgeSeconds < 0 {
		return FacilityProxySettings{}, panelerr.Validation("facility_hsts_invalid", "HSTS max age must not be negative")
	}
	if out.HSTS.MaxAgeSeconds > maxHSTSMaxAgeSeconds {
		out.HSTS.MaxAgeSeconds = maxHSTSMaxAgeSeconds
	}
	if in.ImageUpdateIntervalSeconds < 0 || in.ImageUpdateIntervalSeconds > maxImageUpdateIntervalSeconds {
		return FacilityProxySettings{}, panelerr.WithDetails(
			panelerr.Validation("facility_image_update_interval_invalid", "Image update interval must be between 0 and 600 seconds"),
			map[string]any{"value": in.ImageUpdateIntervalSeconds},
		)
	}
	out.ImageUpdateIntervalSeconds = in.ImageUpdateIntervalSeconds
	out.ServerTokens = strings.ToLower(strings.TrimSpace(out.ServerTokens))
	switch out.ServerTokens {
	case ServerTokensOn, ServerTokensOff:
	default:
		return FacilityProxySettings{}, panelerr.Validation("facility_server_tokens_invalid", "server tokens must be on or off")
	}
	out.ClientMaxBodySize = strings.ToLower(strings.TrimSpace(out.ClientMaxBodySize))
	if !nginxSizePattern.MatchString(out.ClientMaxBodySize) {
		return FacilityProxySettings{}, panelerr.WithDetails(
			panelerr.Validation("facility_client_max_body_size_invalid", "client max body size must be a positive nginx size such as 50m"),
			map[string]any{"value": out.ClientMaxBodySize},
		)
	}
	out.TLSMinVersion = strings.TrimSpace(out.TLSMinVersion)
	switch out.TLSMinVersion {
	case TLSMinVersion12, TLSMinVersion13:
	default:
		return FacilityProxySettings{}, panelerr.Validation("facility_tls_min_version_invalid", "TLS minimum version must be 1.2 or 1.3")
	}
	lines, _, err := normalizeCustomDirectiveLines(directiveScopeHTTP, out.CustomHTTPDirectives)
	if err != nil {
		return FacilityProxySettings{}, err
	}
	out.CustomHTTPDirectives = lines
	return out, nil
}

// normalizeHTTPPortModeOverride 校验域名级 80 端口策略覆盖。nil/空白表示
// 继承设施全局设置（归一化为 nil）；其余只接受 serve/redirect，大小写与空白
// 被归一化。非法值复用全局策略的稳定错误码并附带域名上下文。
func normalizeHTTPPortModeOverride(domain string, override *string) (*string, error) {
	if override == nil {
		return nil, nil
	}
	mode := strings.ToLower(strings.TrimSpace(*override))
	if mode == "" {
		return nil, nil
	}
	switch mode {
	case HTTPPortModeServe, HTTPPortModeRedirect:
		return &mode, nil
	default:
		return nil, panelerr.WithDetails(
			panelerr.Validation("facility_http_port_mode_invalid", "HTTP port mode must be serve, redirect, or empty to inherit"),
			map[string]any{"domain": domain, "value": strings.TrimSpace(*override)},
		)
	}
}

// normalizeClientMaxBodySizeOverride 校验域名级 client_max_body_size 覆盖。
// nil/空白表示继承设施全局设置（归一化为 nil）；非空必须是 1k..1024m 的
// nginx size。非法值复用全局设置的稳定错误码并附带域名上下文。
func normalizeClientMaxBodySizeOverride(domain string, override *string) (*string, error) {
	if override == nil {
		return nil, nil
	}
	normalized, ok := applications.NormalizeClientMaxBodySize(*override)
	if !ok {
		return nil, panelerr.WithDetails(
			panelerr.Validation("facility_client_max_body_size_invalid", "client max body size must be an nginx size between 1k and 1024m"),
			map[string]any{"domain": domain, "value": strings.TrimSpace(*override)},
		)
	}
	if normalized == "" {
		return nil, nil
	}
	return &normalized, nil
}

func parseFacilityProxySettings(raw string) FacilityProxySettings {
	defaults := defaultFacilityProxySettings()
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return defaults
	}
	var doc proxySettingsDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return defaults
	}
	out := defaults
	if mode := strings.ToLower(strings.TrimSpace(doc.HTTPPortMode)); mode == HTTPPortModeServe || mode == HTTPPortModeRedirect {
		out.HTTPPortMode = mode
	}
	if doc.ImageAutoUpdateEnabled != nil {
		out.ImageAutoUpdateEnabled = *doc.ImageAutoUpdateEnabled
	}
	if doc.ImageUpdateIntervalSeconds != nil {
		out.ImageUpdateIntervalSeconds = clampImageUpdateInterval(*doc.ImageUpdateIntervalSeconds)
	}
	if doc.CustomHTTPDirectives != nil {
		out.CustomHTTPDirectives = append([]string{}, doc.CustomHTTPDirectives...)
	}
	if doc.HSTS != nil {
		out.HSTS = HSTSConfig{
			Enabled:           doc.HSTS.Enabled,
			MaxAgeSeconds:     doc.HSTS.MaxAgeSeconds,
			IncludeSubDomains: doc.HSTS.IncludeSubDomains,
		}
		if out.HSTS.MaxAgeSeconds <= 0 {
			out.HSTS.MaxAgeSeconds = defaultHSTSMaxAgeSeconds
		} else if out.HSTS.MaxAgeSeconds > maxHSTSMaxAgeSeconds {
			out.HSTS.MaxAgeSeconds = maxHSTSMaxAgeSeconds
		}
	}
	// 旧库/手工写入的非法 P1 参数回退到默认值，避免渲染出非法 nginx 配置。
	if value := strings.ToLower(strings.TrimSpace(doc.ServerTokens)); value == ServerTokensOn || value == ServerTokensOff {
		out.ServerTokens = value
	}
	if value := strings.ToLower(strings.TrimSpace(doc.ClientMaxBodySize)); nginxSizePattern.MatchString(value) {
		out.ClientMaxBodySize = value
	}
	if value := strings.TrimSpace(doc.TLSMinVersion); value == TLSMinVersion12 || value == TLSMinVersion13 {
		out.TLSMinVersion = value
	}
	return out.resolved()
}

func facilityProxySettingsJSON(settings FacilityProxySettings) (string, error) {
	settings = settings.resolved()
	enabled := settings.ImageAutoUpdateEnabled
	interval := settings.ImageUpdateIntervalSeconds
	doc := proxySettingsDocument{
		HTTPPortMode:               settings.HTTPPortMode,
		ImageAutoUpdateEnabled:     &enabled,
		ImageUpdateIntervalSeconds: &interval,
		CustomHTTPDirectives:       settings.CustomHTTPDirectives,
		HSTS: &hstsDocument{
			Enabled:           settings.HSTS.Enabled,
			MaxAgeSeconds:     settings.HSTS.MaxAgeSeconds,
			IncludeSubDomains: settings.HSTS.IncludeSubDomains,
		},
		ServerTokens:      settings.ServerTokens,
		ClientMaxBodySize: settings.ClientMaxBodySize,
		TLSMinVersion:     settings.TLSMinVersion,
	}
	if len(doc.CustomHTTPDirectives) == 0 {
		doc.CustomHTTPDirectives = []string{}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// nginxTLSProtocols 把 TLS 最低版本映射为 http 块 ssl_protocols 的参数：
// 1.2 → TLSv1.2 TLSv1.3，1.3 → 仅 TLSv1.3。未知值按默认 1.2 渲染。
func nginxTLSProtocols(minVersion string) string {
	if strings.EqualFold(strings.TrimSpace(minVersion), TLSMinVersion13) {
		return "TLSv1.3"
	}
	return "TLSv1.2 TLSv1.3"
}

// SaveProxySettings 保存入口代理全局参数：先校验后 CAS 持久化，再触发所有
// 网关的配置平面协调（nginx -t + reload）。失败整笔不生效。
func (s *Service) SaveProxySettings(ctx context.Context, in ProxySettingsSaveInput) (ReverseProxyConfig, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	previous, err := s.loadConfig(ctx)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	settings := previous.Settings.resolved()
	if mode := strings.TrimSpace(in.HTTPPortMode); mode != "" {
		settings.HTTPPortMode = mode
	}
	if in.ImageAutoUpdateEnabled != nil {
		settings.ImageAutoUpdateEnabled = *in.ImageAutoUpdateEnabled
	}
	if in.ImageUpdateIntervalSeconds != nil {
		settings.ImageUpdateIntervalSeconds = *in.ImageUpdateIntervalSeconds
	}
	settings.CustomHTTPDirectives = append([]string{}, in.CustomHTTPDirectives...)
	if in.HSTS != nil {
		settings.HSTS = *in.HSTS
	}
	// P1 安全参数空白表示"保留现值"，兼容只提交 P0 字段的旧客户端。
	if value := strings.TrimSpace(in.ServerTokens); value != "" {
		settings.ServerTokens = value
	}
	if value := strings.TrimSpace(in.ClientMaxBodySize); value != "" {
		settings.ClientMaxBodySize = value
	}
	if value := strings.TrimSpace(in.TLSMinVersion); value != "" {
		settings.TLSMinVersion = value
	}
	normalized, err := normalizeFacilityProxySettings(settings)
	if err != nil {
		return ReverseProxyConfig{}, err
	}
	expectedVersion := in.Version
	if expectedVersion <= 0 {
		expectedVersion = previous.Version
	}
	if err := s.saveProxySettingsRow(ctx, previous.Version, expectedVersion, normalized); err != nil {
		return ReverseProxyConfig{}, err
	}
	_ = s.setLastError(ctx, "")
	if err := s.triggerReverseProxyReconcile(ctx, "facility_app", nil, nil); err != nil {
		_ = s.setLastError(ctx, err.Error())
		return ReverseProxyConfig{}, err
	}
	return s.GetReverseProxy(ctx)
}

func (s *Service) saveProxySettingsRow(ctx context.Context, currentVersion, expectedVersion int, settings FacilityProxySettings) error {
	raw, err := facilityProxySettingsJSON(settings)
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	if currentVersion == 0 {
		result, err := s.db.ExecContext(ctx, `INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,dns_sync_json,settings_json,last_error,updated_at)
			VALUES(?,1,'[]','{}',?,'',?) ON CONFLICT(id) DO NOTHING`, ReverseProxyID, raw, now)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return panelerr.Conflict("resource_version_conflict", "facility proxy settings changed")
		}
		return nil
	}
	if expectedVersion <= 0 {
		expectedVersion = currentVersion
	}
	result, err := s.db.ExecContext(ctx, `UPDATE facility_app_configs SET version=version+1,settings_json=?,updated_at=? WHERE id=? AND version=?`, raw, now, ReverseProxyID, expectedVersion)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return panelerr.Conflict("resource_version_conflict", "facility proxy settings changed")
	}
	return nil
}
