package facilityapps

import (
	"encoding/json"
	"strings"
	"time"

	"panel/internal/modules/applications"
)

const (
	ReverseProxyID       = "reverse_proxy"
	supportedProxyImage  = "nginx:1.28-alpine"
	proxyApplicationID   = "facility-reverse-proxy"
	proxyInstancePrefix  = "facility-reverse-proxy-"
	proxyContainerName   = "panel-facility-reverse-proxy"
	proxyConfigRoot      = "nginx"
	proxyConfigPath      = "nginx/nginx.conf"
	proxyContainerRoot   = "/etc/panel-nginx"
	proxyConfigDir       = "nginx/conf.d"
	proxyStaticMountRoot = "/srv/panel-static"
	proxyTLSMountRoot    = "/etc/panel-certs"

	StaticSourceUploadedFile   = "uploaded_file"
	StaticSourceUploadedBundle = "uploaded_bundle"

	StaticRuleStatic    = "static"
	StaticRuleRedirect  = "redirect"
	StaticRuleProxyPass = "proxy_pass"

	ProxySourcePreserve = "preserve_source"
	ProxySourceHide     = "hide_source"

	HTTPSDomainCertificate = "domain_certificate"
	HTTPSDisabled          = "disabled"

	dnsSyncTaskType = "dns_proxy_records_sync"

	// 入口代理镜像检查/更新任务（FAC-RP-015）。检查周期 6 小时，更新任务按
	// 逐节点滚动执行；两者共享 concurrency key，避免检查与滚动互相踩踏。
	proxyImageCheckTaskType  = "facility_reverse_proxy_image_check"
	proxyImageUpdateTaskType = "facility_reverse_proxy_image_update"
	proxyImageConcurrencyKey = "facility:reverse-proxy:image"
	proxyImageCheckInterval  = 6 * time.Hour

	// facility_proxy_image_states.status 取值。
	ProxyImageStatusUpToDate        = "up_to_date"
	ProxyImageStatusUpdateAvailable = "update_available"
	ProxyImageStatusUpdating        = "updating"
	ProxyImageStatusFailed          = "failed"
	ProxyImageStatusPinned          = "pinned"
	ProxyImageStatusUnknown         = "unknown"

	// 自动更新失败退避（P1）：同一目标 digest 连续失败达到该次数后，自动更新
	// 跳过该节点（状态投影带原因），直到手动更新/回滚成功或"恢复自动更新"。
	proxyImageAutoUpdateFailureLimit = 3

	DNSSyncPending = "pending"
	DNSSyncSynced  = "synced"
	DNSSyncFailed  = "failed"
	DNSSyncSkipped = "skipped"
)

// DNSSyncState is the per-domain reverse proxy DNS sync status.
type DNSSyncState struct {
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ReverseProxyConfig struct {
	Deployments       []FacilityDeployment                         `json:"deployments"`
	ID                string                                       `json:"id"`
	Version           int                                          `json:"version"`
	DeploymentServers []string                                     `json:"deploymentServers"`
	Domains           []FacilityRouteDomain                        `json:"domains"`
	StaticAssets      []StaticAsset                                `json:"staticAssets"`
	RouteSummaries    []RouteSummary                               `json:"routeSummaries"`
	ApplicationRoutes []applications.ApplicationReverseProxyConfig `json:"applicationRoutes"`
	Settings          FacilityProxySettings                        `json:"settings"`
	Image             ProxyImageStatus                             `json:"image"`
	Operation         *applications.LifecycleOperation             `json:"operation,omitempty"`
	ReconcileStopped  bool                                         `json:"reconcileStopped,omitempty"`
	LastError         string                                       `json:"lastError,omitempty"`
	Warnings          []string                                     `json:"warnings,omitempty"`
	UpdatedAt         time.Time                                    `json:"updatedAt"`
	Routes            int                                          `json:"routes"`
	EnabledServers    []string                                     `json:"enabledServers"`
	DNSSync           map[string]DNSSyncState                      `json:"dnsSync,omitempty"`
}

type ReverseProxySaveInput struct {
	DeploymentServers []string              `json:"deploymentServers"`
	Domains           []FacilityRouteDomain `json:"domains"`
}

// ProxyPlaneState is the gateway/container plane (not a business config document).
type ProxyPlaneState struct {
	ID                string               `json:"id"`
	DeploymentServers []string             `json:"deploymentServers"`
	Deployments       []FacilityDeployment `json:"deployments"`
	ReconcileStopped  bool                 `json:"reconcileStopped,omitempty"`
	LastError         string               `json:"lastError,omitempty"`
	UpdatedAt         time.Time            `json:"updatedAt"`
}

type ProxyPlaneSaveInput struct {
	DeploymentServers []string `json:"deploymentServers"`
}

// ConfigRecord is one independently versioned reverse-proxy configuration.
type ConfigRecord struct {
	RecordID        string              `json:"recordId"`
	Kind            string              `json:"kind"` // facility_domain | application_route
	Version         int                 `json:"version"`
	Domain          FacilityRouteDomain `json:"domain"`
	StaticAssets    []StaticAsset       `json:"staticAssets,omitempty"`
	ApplicationID   string              `json:"applicationId,omitempty"`
	ApplicationName string              `json:"applicationName,omitempty"`
	UpdatedAt       time.Time           `json:"updatedAt"`
}

type FacilityRouteDomain struct {
	Domain          string                       `json:"domain"`
	Version         int                          `json:"version,omitempty"`
	OriginServerIDs []string                     `json:"originServerIds"`
	AnyAccess       applications.AnyAccessConfig `json:"anyAccess"`
	Paths           []FacilityRoutePath          `json:"paths"`
	// 每域名传输开关（FAC-RP-017）。nil 表示调用方未提交，归一化后默认
	// HTTPS/HTTP2 开、HTTP3 关；从数据库读出的记录总是非 nil。
	HTTPSEnabled *bool `json:"httpsEnabled,omitempty"`
	HTTP2Enabled *bool `json:"http2Enabled,omitempty"`
	HTTP3Enabled *bool `json:"http3Enabled,omitempty"`
	// HTTPPortMode 是该域名的 80 端口策略覆盖（FAC-RP-019）：nil 或空串表示
	// 继承设施全局设置；serve 保持 80 直接服务，redirect 在域名真正启用
	// HTTPS（开关开且有证书）时把 HTTP 请求 301 到 HTTPS。响应回填解析后的
	// 覆盖值，继承时保持 nil。
	HTTPPortMode *string `json:"httpPortMode,omitempty"`
	// HSTSEnabled 是该域名的 HSTS 覆盖：nil=继承设施全局 HSTS，true=启用，
	// false=关闭；maxAge/includeSubDomains 仍取设施全局设置。
	HSTSEnabled *bool `json:"hstsEnabled,omitempty"`
	// ClientMaxBodySize 是该域名的请求体上限覆盖（nginx size 语法，1k..1024m）；
	// nil/空=继承设施全局 clientMaxBodySize。
	ClientMaxBodySize *string `json:"clientMaxBodySize,omitempty"`
	// CustomServerDirectives 是该域名 server 块的自定义 nginx 单行指令
	// （FAC-RP-018）；同名指令覆盖面板生成项，其余追加。
	CustomServerDirectives []string `json:"customServerDirectives,omitempty"`
}

// proxyTransport 是解析后的每域名传输开关，渲染/摘要/中继统一使用该结构，
// 避免三处各自判断造成漂移。
type proxyTransport struct {
	HTTPS bool
	HTTP2 bool
	HTTP3 bool
}

func defaultProxyTransport() proxyTransport {
	return proxyTransport{HTTPS: true, HTTP2: true, HTTP3: false}
}

// resolveProxyTransport 把可能缺省的指针开关归一化为解析值。
func resolveProxyTransport(httpsEnabled, http2Enabled, http3Enabled *bool) proxyTransport {
	out := defaultProxyTransport()
	if httpsEnabled != nil {
		out.HTTPS = *httpsEnabled
	}
	if http2Enabled != nil {
		out.HTTP2 = *http2Enabled
	}
	if http3Enabled != nil {
		out.HTTP3 = *http3Enabled
	}
	return out
}

func proxyTransportPointers(transport proxyTransport) (*bool, *bool, *bool) {
	httpsValue, http2Value, http3Value := transport.HTTPS, transport.HTTP2, transport.HTTP3
	return &httpsValue, &http2Value, &http3Value
}

func (d FacilityRouteDomain) Transport() proxyTransport {
	return resolveProxyTransport(d.HTTPSEnabled, d.HTTP2Enabled, d.HTTP3Enabled)
}

// transportSignature 用于去重与日志等需要稳定字符串的场景。
func (t proxyTransport) signature() string {
	out := "https=0"
	if t.HTTPS {
		out = "https=1"
	}
	if t.HTTP2 {
		out += ",http2=1"
	} else {
		out += ",http2=0"
	}
	if t.HTTP3 {
		out += ",http3=1"
	} else {
		out += ",http3=0"
	}
	return out
}

// effectiveHTTPS 是"该域名真的会生成 443 server"的唯一判定：开关开启且有
// 匹配证书。渲染、中继回源协议与 routeSummaries 必须共用它。
func effectiveHTTPS(transport proxyTransport, certificatePresent bool) bool {
	return transport.HTTPS && certificatePresent
}

// httpPortModeIsOverride 判断一个已归一化的 80 端口策略是否为真实覆盖值
// （空串表示继承，不是覆盖）。
func httpPortModeIsOverride(value string) bool {
	return value == HTTPPortModeServe || value == HTTPPortModeRedirect
}

// httpPortModeOverridePtr 把持久化/上游值转成 DTO 形状：只有 serve/redirect
// 才是覆盖，其余（含空串与历史脏值）统一表示为 nil=继承。
func httpPortModeOverridePtr(value string) *string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !httpPortModeIsOverride(value) {
		return nil
	}
	return &value
}

// storedHTTPPortMode 把 DTO 覆盖值压平成持久化形式（""=继承）。
func storedHTTPPortMode(override *string) string {
	if override == nil {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(*override))
	if !httpPortModeIsOverride(value) {
		return ""
	}
	return value
}

// resolvedHTTPPortMode 解析 80 端口策略的继承关系：域名覆盖非空优先，否则
// 使用设施全局策略。返回值总是 serve 或 redirect（不会为空）。
func resolvedHTTPPortMode(override, global string) string {
	if httpPortModeIsOverride(override) {
		return override
	}
	if httpPortModeIsOverride(strings.ToLower(strings.TrimSpace(global))) {
		return strings.ToLower(strings.TrimSpace(global))
	}
	return HTTPPortModeServe
}

// effectiveHTTPPortMode 是渲染/摘要的最终语义：在继承解析之后，redirect 仅当
// 域名真的会生成 443 server（effectiveHTTPS）时生效，否则保持 serve。
func effectiveHTTPPortMode(override, global string, httpsEffective bool) string {
	if resolvedHTTPPortMode(override, global) == HTTPPortModeRedirect && httpsEffective {
		return HTTPPortModeRedirect
	}
	return HTTPPortModeServe
}

// hstsEnabledOverridePtr 把持久化的空串/on/off 转回 DTO：空/脏值统一表示为
// nil=继承设施全局 HSTS。
func hstsEnabledOverridePtr(value string) *bool {
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

// storedHSTSEnabled 把 DTO 覆盖压平成持久化形式（""=继承，on/off 显式覆盖）。
func storedHSTSEnabled(override *bool) string {
	if override == nil {
		return ""
	}
	if *override {
		return "on"
	}
	return "off"
}

// resolvedHSTSEnabled 解析 HSTS 继承关系：域名覆盖优先，否则设施全局开关。
func resolvedHSTSEnabled(override *bool, global bool) bool {
	if override != nil {
		return *override
	}
	return global
}

// cloneHSTSEnabledOverride 复制 HSTS 覆盖指针，避免调用方共享可变状态。
func cloneHSTSEnabledOverride(override *bool) *bool {
	if override == nil {
		return nil
	}
	value := *override
	return &value
}

// clientMaxBodySizeOverridePtr 把持久化/上游值转成 DTO 形状：空或非法（历史
// 脏值）统一表示为 nil=继承。合法值保留归一化后的小写形式。
func clientMaxBodySizeOverridePtr(value string) *string {
	normalized, ok := applications.NormalizeClientMaxBodySize(value)
	if !ok || normalized == "" {
		return nil
	}
	return &normalized
}

// storedClientMaxBodySize 把 DTO 覆盖压平成持久化形式（""=继承）。
func storedClientMaxBodySize(override *string) string {
	if override == nil {
		return ""
	}
	normalized, ok := applications.NormalizeClientMaxBodySize(*override)
	if !ok {
		return ""
	}
	return normalized
}

// resolvedClientMaxBodySize 解析请求体上限继承关系：域名覆盖非空优先，否则
// 设施全局 clientMaxBodySize；全局值沿用现有全局校验语法（可 >1024m），只有
// 域名覆盖受 1k..1024m 限制。全局也缺失/非法时回落到默认 50m。
func resolvedClientMaxBodySize(override *string, global string) string {
	if override != nil {
		if normalized, ok := applications.NormalizeClientMaxBodySize(*override); ok && normalized != "" {
			return normalized
		}
	}
	if normalized := strings.ToLower(strings.TrimSpace(global)); nginxSizePattern.MatchString(normalized) {
		return normalized
	}
	return defaultClientMaxBodySize
}

// resolvedClientMaxBodySizeValue 是 resolvedClientMaxBodySize 的字符串入参版本，
// 供应用反代规则（已解析为 string）复用。
func resolvedClientMaxBodySizeValue(value, global string) string {
	return resolvedClientMaxBodySize(clientMaxBodySizeOverridePtr(value), global)
}

type FacilityRoutePath struct {
	Path            string                        `json:"path"`
	RuleType        string                        `json:"ruleType,omitempty"`
	SourceType      string                        `json:"sourceType"`
	AssetName       string                        `json:"assetName,omitempty"`
	AssetID         string                        `json:"-"`
	RedirectURL     string                        `json:"redirectUrl,omitempty"`
	RedirectCode    int                           `json:"redirectCode,omitempty"`
	ProxyURL        string                        `json:"proxyUrl,omitempty"`
	ProxySourceMode string                        `json:"proxySourceMode,omitempty"`
	Options         applications.HTTPRouteOptions `json:"options,omitempty"`
}

// UnmarshalJSON accepts the pre-name route reference while keeping new
// responses free of the physical asset identifier.
func (p *FacilityRoutePath) UnmarshalJSON(data []byte) error {
	type routePathAlias FacilityRoutePath
	var value routePathAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var legacy struct {
		AssetID string `json:"assetId"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*p = FacilityRoutePath(value)
	p.AssetID = legacy.AssetID
	return nil
}

func (p FacilityRoutePath) MarshalJSON() ([]byte, error) {
	type routePathAlias FacilityRoutePath
	value := routePathAlias(p)
	if strings.TrimSpace(value.AssetName) == "" {
		value.AssetName = strings.TrimSpace(value.AssetID)
	}
	value.AssetID = ""
	return json.Marshal(value)
}

type StaticAsset struct {
	// ID is a storage key and is intentionally not part of the public asset
	// contract.  Routes and clients address an asset by its facility-local name.
	ID          string    `json:"-"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	ContentMode string    `json:"contentMode"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type RouteSummary struct {
	Domain       string   `json:"domain"`
	Path         string   `json:"path"`
	Source       string   `json:"source"`
	ServerIDs    []string `json:"serverIds"`
	HTTPSStatus  string   `json:"httpsStatus"`
	HTTPSEnabled bool     `json:"httpsEnabled"`
	HTTP2Enabled bool     `json:"http2Enabled"`
	HTTP3Enabled bool     `json:"http3Enabled"`
	// EffectiveHTTPS 表示该域名实际会生成 443 server（开关开且有证书）。
	EffectiveHTTPS bool `json:"effectiveHttps"`
	// HTTPPortMode 是该域名实际生效的 80 端口策略：域名覆盖/设施全局继承
	// 之后再套用 effectiveHTTPS 回退，无证书时 redirect 投影为 serve。
	HTTPPortMode string `json:"httpPortMode"`
	// HSTSEnabled/ClientMaxBodySize 是该域名实际生效的 HSTS 开关与请求体上限
	// （域名覆盖优先，否则设施全局）。
	HSTSEnabled       bool     `json:"hstsEnabled"`
	ClientMaxBodySize string   `json:"clientMaxBodySize"`
	CertificateID     string   `json:"certificateId,omitempty"`
	CertificateName   string   `json:"certificateName,omitempty"`
	MatchedDomains    []string `json:"matchedDomains,omitempty"`
	ApplicationID     string   `json:"applicationId,omitempty"`
	ApplicationName   string   `json:"applicationName,omitempty"`
}

// ProxyImageServerStatus 是单网关的入口代理镜像状态。
type ProxyImageServerStatus struct {
	ServerID       string `json:"serverId"`
	Status         string `json:"status"`
	Pinned         bool   `json:"pinned,omitempty"`
	ObservedDigest string `json:"observedDigest,omitempty"`
	TargetDigest   string `json:"targetDigest,omitempty"`
	PreviousDigest string `json:"previousDigest,omitempty"`
	// ImageVersion 是该网关运行中 nginx 的实际版本（如 1.28.3），来自 Agent
	// `nginx -v` 的周期采集；空表示尚未采集或采集失败。
	ImageVersion string `json:"imageVersion,omitempty"`
	// AutoUpdateFailures/AutoUpdatePaused/AutoUpdateSkipReason 投影自动更新失败
	// 退避状态（P1）：达到阈值后自动更新跳过该节点，并说明原因。
	AutoUpdateFailures   int    `json:"autoUpdateFailures,omitempty"`
	AutoUpdatePaused     bool   `json:"autoUpdatePaused,omitempty"`
	AutoUpdateSkipReason string `json:"autoUpdateSkipReason,omitempty"`
	// FailedDigest 是连续失败退避归因的目标 digest（成功/手动更新/回滚后清空）。
	FailedDigest string     `json:"failedDigest,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	CheckedAt    *time.Time `json:"checkedAt,omitempty"`
}

// ProxyImageStatus 是入口代理镜像的聚合状态（GET 反代配置的 image 字段）。
type ProxyImageStatus struct {
	Reference        string     `json:"reference"`
	Status           string     `json:"status"`
	TargetDigest     string     `json:"targetDigest,omitempty"`
	LatestDigest     string     `json:"latestDigest,omitempty"`
	PreviousDigest   string     `json:"previousDigest,omitempty"`
	UpdateAvailable  bool       `json:"updateAvailable"`
	Pinned           bool       `json:"pinned,omitempty"`
	AutoUpdatePaused bool       `json:"autoUpdatePaused,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	CheckedAt        *time.Time `json:"checkedAt,omitempty"`
	UpdatedAt        *time.Time `json:"updatedAt,omitempty"`
	// ImageVersion 是聚合展示的代表版本：取第一个非空的网关实际版本。
	ImageVersion string                   `json:"imageVersion,omitempty"`
	Servers      []ProxyImageServerStatus `json:"servers"`
}

// ProxyImageAction 是手动检查/更新/回滚接口的受理结果。Skipped 表示本次请求
// 是幂等空操作（无回滚目标或节点已固定），不会创建任务。
type ProxyImageAction struct {
	TaskID  string `json:"taskId,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Pinned  bool   `json:"pinned,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ProxyImageUpdateInput 是 POST .../image/update 的负载。
type ProxyImageUpdateInput struct {
	ServerIDs []string `json:"serverIds,omitempty"`
	Confirm   bool     `json:"confirm"`
}

// ProxyImageRollbackInput 是 POST .../image/rollback 的负载。Digest 省略时使用
// 各网关自己的 previous_digest；Confirm 必须显式为 true。
type ProxyImageRollbackInput struct {
	ServerIDs []string `json:"serverIds,omitempty"`
	Confirm   bool     `json:"confirm"`
	Digest    string   `json:"digest,omitempty"`
}

// ProxyImageResumeInput 是 POST .../image/resume 的负载：清除 pinned 与自动更新
// 失败退避，把镜像引用恢复到受管标签线。
type ProxyImageResumeInput struct {
	ServerIDs []string `json:"serverIds,omitempty"`
}

type ReconcileResult struct {
	Config ReverseProxyConfig `json:"config"`
}

const (
	FacilityEditSessionActive     = "active"
	FacilityEditSessionCommitting = "committing"
	FacilityEditSessionCommitted  = "committed"
	FacilityEditSessionConflict   = "conflict"
	FacilityEditSessionDiscarded  = "discarded"
	FacilityEditSessionExpired    = "expired"
)

type FacilityEditAsset struct {
	// AssetKey and SourceAssetID are storage references kept for commit and
	// migration handling; clients use the facility-local name as identity.
	AssetKey      string    `json:"-"`
	SourceAssetID string    `json:"-"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	ContentMode   string    `json:"contentMode"`
	Filename      string    `json:"filename"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type FacilityEditSession struct {
	ID                  string                       `json:"id"`
	RecordID            string                       `json:"recordId"`
	ClientDraftKey      string                       `json:"clientDraftKey,omitempty"`
	State               string                       `json:"state"`
	BaseResourceVersion applications.ResourceVersion `json:"baseResourceVersion"`
	Draft               FacilityRouteDomain          `json:"draft"`
	Revision            int                          `json:"revision"`
	Assets              []FacilityEditAsset          `json:"assets"`
	PreviewToken        *applications.PreviewToken   `json:"previewToken,omitempty"`
	IdleExpiresAt       time.Time                    `json:"idleExpiresAt"`
	AbsoluteExpiresAt   time.Time                    `json:"absoluteExpiresAt"`
	CreatedAt           time.Time                    `json:"createdAt"`
	UpdatedAt           time.Time                    `json:"updatedAt"`
	CommittedAt         *time.Time                   `json:"committedAt,omitempty"`
	CommitResult        *FacilityEditCommitResult    `json:"commitResult,omitempty"`
}

type BeginFacilityEditSessionInput struct {
	RecordID       string               `json:"recordId"`
	ClientDraftKey string               `json:"clientDraftKey,omitempty"`
	Draft          *FacilityRouteDomain `json:"draft,omitempty"`
}

type PatchFacilityEditSessionInput struct {
	Revision            int                 `json:"revision"`
	BaseResourceVersion string              `json:"baseResourceVersion,omitempty"`
	Draft               FacilityRouteDomain `json:"draft"`
}

type FacilityEditAssetInput struct {
	Revision          int
	ClientOperationID string
	Name              string
	Kind              string
	ContentMode       string
	FileName          string
	Content           []byte
}

type FacilityEditMutationInput struct {
	Revision          int    `json:"revision"`
	ClientOperationID string `json:"clientOperationId"`
}

type FacilityEditValidationResult struct {
	Valid       bool                      `json:"valid"`
	Revision    int                       `json:"revision"`
	Diagnostics []applications.Diagnostic `json:"diagnostics"`
}

type FacilityEditPreviewResult struct {
	Revision    int                       `json:"revision"`
	Diagnostics []applications.Diagnostic `json:"diagnostics"`
	Token       applications.PreviewToken `json:"token"`
	ExpiresAt   time.Time                 `json:"expiresAt"`
}

type CommitFacilityEditSessionInput struct {
	Revision            int    `json:"revision"`
	BaseResourceVersion string `json:"baseResourceVersion"`
	PreviewToken        string `json:"previewToken"`
}

type FacilityEditCommitResult struct {
	Record          ConfigRecord                 `json:"record"`
	Config          ReverseProxyConfig           `json:"config,omitempty"` // aggregate snapshot for callers that still expect it
	ResourceVersion applications.ResourceVersion `json:"resourceVersion"`
	ApplyRequested  bool                         `json:"applyRequested"`
	ApplyNeeded     bool                         `json:"applyNeeded"`
	ApplyServerIDs  []string                     `json:"applyServerIds,omitempty"`
	Diagnostics     []applications.Diagnostic    `json:"diagnostics,omitempty"`
}

// ReconcileReverseProxyInput controls proxy-plane ensure vs config-plane apply.
// Apply=true selects config-plane reload (force=false). Empty Apply is proxy ensure/force user reconcile.
type ReconcileReverseProxyInput struct {
	ServerIDs     []string `json:"serverIds"`
	StopServerIDs []string `json:"stopServerIds"`
	Apply         bool     `json:"apply"`
	RecordID      string   `json:"recordId,omitempty"`
}
