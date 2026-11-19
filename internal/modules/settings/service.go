package settings

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"panel/internal/platform/config"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	"panel/internal/platform/logging"
	"panel/internal/platform/paneltls"
	"panel/internal/platform/reconciletrace"

	"go.uber.org/zap"
)

var serverVariableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// autoUpdateScheduleTimePattern 校验每日自动更新执行时间：24 小时制 HH:MM。
var autoUpdateScheduleTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type RuntimeCertificateSettings struct {
	Email                      string `json:"email"`
	DNSPropagationDelaySeconds int    `json:"dnsPropagationDelaySeconds"`
}

type RuntimeBrandingSettings struct {
	LoginTitle    string `json:"loginTitle"`
	LoginSubtitle string `json:"loginSubtitle"`
}

type RuntimePanelSettings struct {
	Domain           string `json:"domain"`
	TLSCertificateID string `json:"tlsCertificateId"`
}

// RuntimeAgentSettings configures how the panel-agent bundle reaches a target
// host. An empty DownloadBaseURL keeps the SSH upload as the only delivery
// path, which is the behaviour of a Panel that has never opted in.
type RuntimeAgentSettings struct {
	DownloadBaseURL        string `json:"downloadBaseUrl"`
	DownloadVerifyTLS      bool   `json:"downloadVerifyTls"`
	TransferTimeoutSeconds int    `json:"transferTimeoutSeconds"`
}

// RuntimeImageRegistrySettings 是「镜像仓库源」分区：拉取镜像时使用的仓库
// 地址与协议。Endpoint 为空表示官方默认仓库；非空时必须是不带 scheme 的裸
// host[:port]，协议由 Scheme 单独表达，避免把 "https://" 混进地址参与拼接。
type RuntimeImageRegistrySettings struct {
	Endpoint string `json:"endpoint"`
	Scheme   string `json:"scheme"`
}

// RuntimeImageRegistryUpdate 是 PUT /api/v1/settings/runtime/image-registry 的
// 写入负载。字段用指针区分「省略 = 保留现值」与「显式清空」：endpoint 显式
// 空串表示回到官方仓库。
type RuntimeImageRegistryUpdate struct {
	Endpoint *string `json:"endpoint"`
	Scheme   *string `json:"scheme"`
}

// RuntimeAutoUpdateSettings 是设置页「自动更新」分区（SET-RUN-023）。前四个字段
// 由用户编辑；lastRun* 是只读运行态，由 autoupdate 协调任务写入，分区 PUT 不接受。
type RuntimeAutoUpdateSettings struct {
	AppsEnabled     bool   `json:"appsEnabled"`
	PackagesEnabled bool   `json:"packagesEnabled"`
	PackagesMode    string `json:"packagesMode"`
	ScheduleTime    string `json:"scheduleTime"`
	// LastRunAt 是最近一次自动更新开始时间（RFC3339，UTC）。它是跨重启的当日
	// 去重依据：任务历史只保留 24 小时，不能只靠任务查询。
	LastRunAt     string `json:"lastRunAt,omitempty"`
	LastRunStatus string `json:"lastRunStatus,omitempty"`
	LastRunTaskID string `json:"lastRunTaskId,omitempty"`
}

// RuntimeAutoUpdateUpdate 是 PUT /api/v1/settings/runtime/auto-update 的写入负载。
// 四个字段都用指针表达「省略 = 保留现值」：布尔值若用值类型，关闭开关会被
// Update 的「零值 = 保留」规则吞掉，导致自动更新永远无法关闭。
type RuntimeAutoUpdateUpdate struct {
	AppsEnabled     *bool   `json:"appsEnabled"`
	PackagesEnabled *bool   `json:"packagesEnabled"`
	PackagesMode    *string `json:"packagesMode"`
	ScheduleTime    *string `json:"scheduleTime"`
}

// RuntimeAutoUpdateRunRequest 是 POST /api/v1/settings/runtime/auto-update/run
// 的可选临时覆盖：省略（或空串）字段沿用当前设置，覆盖只在本次运行生效，
// 不写回 runtime_settings。
type RuntimeAutoUpdateRunRequest struct {
	AppsEnabled     *bool   `json:"appsEnabled"`
	PackagesEnabled *bool   `json:"packagesEnabled"`
	PackagesMode    *string `json:"packagesMode"`
}

// RuntimeAutoUpdateRunState 是协调任务写入的只读运行态快照。
type RuntimeAutoUpdateRunState struct {
	LastRunAt     time.Time
	LastRunStatus string
	LastRunTaskID string
}

// AutoUpdateManualRunner 是手动「立即运行一次」的委托目标，由 autoupdate 模块
// 实现。装配顺序上 autoupdate 服务在 settings 之后创建，因此通过
// SetAutoUpdateRunner 注入，避免 settings 反向依赖 autoupdate。
type AutoUpdateManualRunner interface {
	RunAutoUpdateNow(context.Context, RuntimeAutoUpdateSettings) (string, error)
}

type TLSAssetProvider interface {
	AssetType(context.Context, string) (string, error)
	ReadFile(context.Context, string, string) ([]byte, string, error)
	SyncPanelTLS(context.Context, string, string) error
}

type ServerVariableDefinition struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Required bool   `json:"required"`
}

type ServerVariableDefinitionsUpdate struct {
	Definitions []ServerVariableDefinition `json:"definitions"`
}

// RuntimeUpdate 是内部使用的完整设置写入负载。设置页的写入已按分区拆分
// （SET-RUN-022），分区方法在当前内存快照上合成完整候选后委托 Update，从而
// 复用校验、原子持久化、热更新与 Panel TLS 临界区；identity 等模块仍可直接
// 使用 Update 做跨分区的小范围更新。
type RuntimeUpdate struct {
	ContainerReportIntervalSeconds int                           `json:"containerReportIntervalSeconds"`
	TokenExpiration                string                        `json:"tokenExpiration"`
	Language                       string                        `json:"language"`
	LogLevel                       string                        `json:"logLevel"`
	RemoteCommandTimeoutSeconds    int                           `json:"remoteCommandTimeoutSeconds"`
	ReconcileTraceEnabled          *bool                         `json:"reconcileTraceEnabled"`
	Branding                       *RuntimeBrandingSettings      `json:"branding"`
	Certificates                   *RuntimeCertificateSettings   `json:"certificates"`
	Panel                          *RuntimePanelSettings         `json:"panel"`
	Agent                          *RuntimeAgentSettings         `json:"agent"`
	ImageRegistry                  *RuntimeImageRegistrySettings `json:"imageRegistry"`
	AutoUpdate                     *RuntimeAutoUpdateSettings    `json:"autoUpdate"`
}

// RuntimeGeneralUpdate 是 PUT /api/v1/settings/runtime/general 的写入负载。
// 分区负载只携带本分区字段；省略的字段沿用当前快照，而不是被清零。
type RuntimeGeneralUpdate struct {
	ContainerReportIntervalSeconds int    `json:"containerReportIntervalSeconds"`
	Language                       string `json:"language"`
	LogLevel                       string `json:"logLevel"`
	ReconcileTraceEnabled          *bool  `json:"reconcileTraceEnabled"`
}

// RuntimeSecurityUpdate 是 PUT /api/v1/settings/runtime/security 的写入负载。
type RuntimeSecurityUpdate struct {
	TokenExpiration             string `json:"tokenExpiration"`
	RemoteCommandTimeoutSeconds int    `json:"remoteCommandTimeoutSeconds"`
}

// RuntimeCertificatesUpdate 是 PUT /api/v1/settings/runtime/certificates 的
// 写入负载。Panel 与 Certificates 用指针表达分组是否提供：省略的分组保持
// 现值，避免半套负载把 Panel 域名清零后触发校验失败。
type RuntimeCertificatesUpdate struct {
	Panel        *RuntimePanelSettings       `json:"panel"`
	Certificates *RuntimeCertificateSettings `json:"certificates"`
}

type RuntimeSettings struct {
	ListenAddress                  string                       `json:"listenAddress"`
	AppDatabase                    string                       `json:"appDatabase"`
	MetricsDatabase                string                       `json:"metricsDatabase"`
	DataRoot                       string                       `json:"dataRoot"`
	ContainerReportIntervalSeconds int                          `json:"containerReportIntervalSeconds"`
	TokenExpiration                string                       `json:"tokenExpiration"`
	Language                       string                       `json:"language"`
	LogLevel                       string                       `json:"logLevel"`
	RemoteCommandTimeoutSeconds    int                          `json:"remoteCommandTimeoutSeconds"`
	ReconcileTraceEnabled          bool                         `json:"reconcileTraceEnabled"`
	Branding                       RuntimeBrandingSettings      `json:"branding"`
	Certificates                   RuntimeCertificateSettings   `json:"certificates"`
	Panel                          RuntimePanelSettings         `json:"panel"`
	Agent                          RuntimeAgentSettings         `json:"agent"`
	ImageRegistry                  RuntimeImageRegistrySettings `json:"imageRegistry"`
	AutoUpdate                     RuntimeAutoUpdateSettings    `json:"autoUpdate"`
	JWTSecret                      string                       `json:"-"`
	JWTSecretConfigured            bool                         `json:"jwtSecretConfigured"`
}

const (
	RuntimeSettingTokenExpiration                      = "tokenExpiration"
	RuntimeSettingLogLevel                             = "log.level"
	RuntimeSettingJWTSecret                            = "jwtSecret"
	RuntimeSettingRemoteCommandTimeoutSeconds          = "remoteCommandTimeoutSeconds"
	RuntimeSettingReconcileTrace                       = "reconcile.trace"
	RuntimeSettingBrandingLoginTitle                   = "branding.loginTitle"
	RuntimeSettingBrandingLoginSubtitle                = "branding.loginSubtitle"
	RuntimeSettingCertificateEmail                     = "certificates.email"
	RuntimeSettingCertificateDNSPropagationDelaySecond = "certificates.dnsPropagationDelaySeconds"
	RuntimeSettingServerVariableDefinitions            = "serverVariables.definitions"
	RuntimeSettingPanelDomain                          = "panel.domain"
	RuntimeSettingPanelTLSCertificateID                = "panel.tlsCertificateId"
	RuntimeSettingAgentDownloadBaseURL                 = "agent.downloadBaseUrl"
	RuntimeSettingAgentDownloadVerifyTLS               = "agent.downloadVerifyTls"
	RuntimeSettingAgentTransferTimeoutSeconds          = "agent.transferTimeoutSeconds"
	RuntimeSettingAutoUpdateAppsEnabled                = "autoUpdate.appsEnabled"
	RuntimeSettingAutoUpdatePackagesEnabled            = "autoUpdate.packagesEnabled"
	RuntimeSettingAutoUpdatePackagesMode               = "autoUpdate.packagesMode"
	RuntimeSettingAutoUpdateScheduleTime               = "autoUpdate.scheduleTime"
	RuntimeSettingAutoUpdateLastRunAt                  = "autoUpdate.lastRunAt"
	RuntimeSettingAutoUpdateLastRunStatus              = "autoUpdate.lastRunStatus"
	RuntimeSettingAutoUpdateLastRunTaskID              = "autoUpdate.lastRunTaskId"
	RuntimeSettingImageRegistryEndpoint                = "imageRegistry.endpoint"
	RuntimeSettingImageRegistryScheme                  = "imageRegistry.scheme"

	// 镜像仓库源协议（SET-RUN-024）。Endpoint 是裸 host[:port]，协议只允许
	// http/https；默认 https，endpoint 为空表示官方默认仓库。
	ImageRegistrySchemeHTTP  = "http"
	ImageRegistrySchemeHTTPS = "https"

	DefaultImageRegistryScheme = ImageRegistrySchemeHTTPS

	// 自动更新软件包范围（SET-RUN-023）。仅安全范围只升级 Source 命中
	// security 的软件包（PKG-API-011），绝不隐式退化为全部升级。
	AutoUpdatePackagesModeSecurity = "security"
	AutoUpdatePackagesModeAll      = "all"

	// DefaultAutoUpdateScheduleTime 是每日自动更新的默认执行时间（Panel 本地
	// 时区，24 小时制 HH:MM）。
	DefaultAutoUpdateScheduleTime = "04:00"

	// DefaultAgentTransferTimeoutSeconds bounds one agent bundle transfer on
	// both sides. It is deliberately larger than the shared remote command
	// timeout: a compressed bundle still takes time on a slow international
	// link, while an unbounded download would let a stuck target hold the task
	// open forever. The target-side budget is derived from this value, so
	// raising it is the supported way to serve slower links.
	DefaultAgentTransferTimeoutSeconds = 300
	MinAgentTransferTimeoutSeconds     = 60
	MaxAgentTransferTimeoutSeconds     = 3600

	TokenExpiration10Minutes = "10m"
	TokenExpiration1Hour     = "1h"
	TokenExpiration1Day      = "1d"
	TokenExpiration5Days     = "5d"
	TokenExpiration30Days    = "30d"
	TokenExpirationNever     = "never"

	DefaultTokenExpiration = TokenExpiration1Day
	DefaultJWTSecret       = "change-me-panel-jwt-secret"
)

type Service struct {
	db        *sql.DB
	cfg       config.Config
	mu        sync.RWMutex
	rt        RuntimeSettings
	tlsAssets TLSAssetProvider
	// autoUpdateRunner 由 autoupdate 模块在装配后注入（见 SetAutoUpdateRunner）。
	autoUpdateRunner AutoUpdateManualRunner
}

type Option func(*Service)

func WithTLSAssetProvider(provider TLSAssetProvider) Option {
	return func(s *Service) { s.tlsAssets = provider }
}

func NewService(db *sql.DB, cfg config.Config, options ...Option) (*Service, error) {
	s := &Service{db: db, cfg: cfg, rt: defaultRuntimeSettings(cfg)}
	for _, option := range options {
		option(s)
	}
	if err := s.ensureDefaultRuntimeSettings(context.Background()); err != nil {
		return nil, err
	}
	if err := s.load(context.Background()); err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.rt.Panel.TLSCertificateID) != "" {
		if err := s.syncPanelTLS(context.Background(), s.rt.Panel); err != nil {
			logging.From(logging.Background("settings")).Warn("failed to synchronize Panel TLS certificate", zap.Error(err))
		}
	}
	return s, nil
}

func (s *Service) Runtime() RuntimeSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rt
}

func (s *Service) JWTSecret() string {
	secret := strings.TrimSpace(s.Runtime().JWTSecret)
	if secret == "" {
		return DefaultJWTSecret
	}
	return secret
}

func (s *Service) RemoteTimeout() time.Duration {
	seconds := s.Runtime().RemoteCommandTimeoutSeconds
	if seconds < 1 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func (s *Service) ApplyToConfig(base config.Config) config.Config {
	rt := s.Runtime()
	base.RemoteCommandTimeoutSeconds = rt.RemoteCommandTimeoutSeconds
	base.Certificates.Email = rt.Certificates.Email
	base.Certificates.DNSPropagationDelaySeconds = rt.Certificates.DNSPropagationDelaySeconds
	return base
}

func (s *Service) Update(ctx context.Context, input RuntimeUpdate) (RuntimeSettings, error) {
	current := s.Runtime()
	if input.TokenExpiration == "" {
		input.TokenExpiration = current.TokenExpiration
	}
	if input.Language == "" {
		input.Language = current.Language
	}
	if input.LogLevel == "" {
		input.LogLevel = current.LogLevel
	}
	if input.RemoteCommandTimeoutSeconds == 0 {
		input.RemoteCommandTimeoutSeconds = current.RemoteCommandTimeoutSeconds
	}
	if input.ContainerReportIntervalSeconds == 0 {
		input.ContainerReportIntervalSeconds = current.ContainerReportIntervalSeconds
	}
	certSettings := current.Certificates
	if input.Certificates != nil {
		certSettings = *input.Certificates
	}
	panelSettings := current.Panel
	if input.Panel != nil {
		panelSettings = RuntimePanelSettings{Domain: strings.ToLower(strings.TrimSpace(input.Panel.Domain)), TLSCertificateID: strings.TrimSpace(input.Panel.TLSCertificateID)}
	}
	brandingSettings := current.Branding
	if input.Branding != nil {
		brandingSettings = RuntimeBrandingSettings{
			LoginTitle:    strings.TrimSpace(input.Branding.LoginTitle),
			LoginSubtitle: strings.TrimSpace(input.Branding.LoginSubtitle),
		}
	}
	agentSettings := current.Agent
	if input.Agent != nil {
		baseURL, err := normalizeAgentDownloadBaseURL(input.Agent.DownloadBaseURL)
		if err != nil {
			return RuntimeSettings{}, err
		}
		transferTimeout := input.Agent.TransferTimeoutSeconds
		if transferTimeout == 0 {
			transferTimeout = current.Agent.TransferTimeoutSeconds
		}
		agentSettings = RuntimeAgentSettings{
			DownloadBaseURL:        baseURL,
			DownloadVerifyTLS:      input.Agent.DownloadVerifyTLS,
			TransferTimeoutSeconds: transferTimeout,
		}
	}
	// 镜像仓库源分区：endpoint 归一为小写裸 host[:port]，显式空串合法
	// （官方仓库）；scheme 省略或空串沿用现值，其余必须是 http/https。
	imageRegistrySettings := current.ImageRegistry
	if input.ImageRegistry != nil {
		endpoint, err := normalizeImageRegistryEndpoint(input.ImageRegistry.Endpoint)
		if err != nil {
			return RuntimeSettings{}, err
		}
		scheme := strings.ToLower(strings.TrimSpace(input.ImageRegistry.Scheme))
		if scheme == "" {
			scheme = current.ImageRegistry.Scheme
		}
		imageRegistrySettings = RuntimeImageRegistrySettings{Endpoint: endpoint, Scheme: scheme}
	}
	// 自动更新分区的写入负载是整组替换；lastRun* 只读态始终沿用内存快照，
	// 由 RecordAutoUpdateLastRun 单独写入（不进 runtimeValues）。
	autoUpdateSettings := current.AutoUpdate
	if input.AutoUpdate != nil {
		packagesMode := strings.TrimSpace(input.AutoUpdate.PackagesMode)
		if packagesMode == "" {
			packagesMode = current.AutoUpdate.PackagesMode
		} else if normalized := normalizeAutoUpdatePackagesMode(packagesMode); normalized != "" {
			packagesMode = normalized
		}
		scheduleTime := strings.TrimSpace(input.AutoUpdate.ScheduleTime)
		if scheduleTime == "" {
			scheduleTime = current.AutoUpdate.ScheduleTime
		}
		autoUpdateSettings = RuntimeAutoUpdateSettings{
			AppsEnabled:     input.AutoUpdate.AppsEnabled,
			PackagesEnabled: input.AutoUpdate.PackagesEnabled,
			PackagesMode:    packagesMode,
			ScheduleTime:    scheduleTime,
			LastRunAt:       current.AutoUpdate.LastRunAt,
			LastRunStatus:   current.AutoUpdate.LastRunStatus,
			LastRunTaskID:   current.AutoUpdate.LastRunTaskID,
		}
	}
	next := RuntimeSettings{
		ListenAddress:                  current.ListenAddress,
		AppDatabase:                    current.AppDatabase,
		MetricsDatabase:                current.MetricsDatabase,
		DataRoot:                       current.DataRoot,
		ContainerReportIntervalSeconds: input.ContainerReportIntervalSeconds,
		TokenExpiration:                NormalizeTokenExpiration(input.TokenExpiration),
		Language:                       i18n.NormalizeLocale(input.Language),
		LogLevel:                       logging.NormalizeLevel(input.LogLevel),
		RemoteCommandTimeoutSeconds:    input.RemoteCommandTimeoutSeconds,
		ReconcileTraceEnabled:          current.ReconcileTraceEnabled,
		Branding:                       brandingSettings,
		Certificates:                   certSettings,
		Panel:                          panelSettings,
		Agent:                          agentSettings,
		ImageRegistry:                  imageRegistrySettings,
		AutoUpdate:                     autoUpdateSettings,
		JWTSecret:                      current.JWTSecret,
		JWTSecretConfigured:            current.JWTSecretConfigured,
	}
	if input.ReconcileTraceEnabled != nil {
		next.ReconcileTraceEnabled = *input.ReconcileTraceEnabled
	}
	if err := validateRuntimeSettings(next); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.validatePanelTLS(next); err != nil {
		return RuntimeSettings{}, err
	}
	if input.Panel != nil {
		if err := paneltls.WithUpdate(func() error {
			snapshot, err := paneltls.SnapshotFixedPair(s.cfg.DataRoot)
			if err != nil {
				return err
			}
			if err := s.syncPanelTLS(ctx, next.Panel); err != nil {
				return errors.Join(err, paneltls.RestoreFixedPair(s.cfg.DataRoot, snapshot))
			}
			if err := s.saveValues(ctx, runtimeValues(next, false)); err != nil {
				return errors.Join(err, paneltls.RestoreFixedPair(s.cfg.DataRoot, snapshot))
			}
			return nil
		}); err != nil {
			return RuntimeSettings{}, err
		}
	} else if err := s.saveValues(ctx, runtimeValues(next, false)); err != nil {
		return RuntimeSettings{}, err
	}
	s.mu.Lock()
	s.rt.ContainerReportIntervalSeconds = next.ContainerReportIntervalSeconds
	s.rt.TokenExpiration = next.TokenExpiration
	s.rt.Language = next.Language
	s.rt.LogLevel = next.LogLevel
	s.rt.RemoteCommandTimeoutSeconds = next.RemoteCommandTimeoutSeconds
	s.rt.ReconcileTraceEnabled = next.ReconcileTraceEnabled
	s.rt.Branding = next.Branding
	s.rt.Certificates = next.Certificates
	s.rt.Panel = next.Panel
	s.rt.Agent = next.Agent
	s.rt.ImageRegistry = next.ImageRegistry
	// 只覆盖可编辑字段：并发的 RecordAutoUpdateLastRun 可能已写入更新的
	// lastRun*，整组赋值会把运行态回退到本候选快照的旧值。
	s.rt.AutoUpdate.AppsEnabled = next.AutoUpdate.AppsEnabled
	s.rt.AutoUpdate.PackagesEnabled = next.AutoUpdate.PackagesEnabled
	s.rt.AutoUpdate.PackagesMode = next.AutoUpdate.PackagesMode
	s.rt.AutoUpdate.ScheduleTime = next.AutoUpdate.ScheduleTime
	out := s.rt
	s.mu.Unlock()
	i18n.SetDefaultLocale(out.Language)
	_ = logging.SetLevel(out.LogLevel)
	reconciletrace.SetEnabled(out.ReconcileTraceEnabled)
	return out, nil
}

// UpdateGeneral 只更新运行时/常规分区：容器上报间隔、界面语言、日志级别与
// 协调追踪开关。其余分区沿用当前内存快照，热更新语义与 Update 一致。
func (s *Service) UpdateGeneral(ctx context.Context, input RuntimeGeneralUpdate) (RuntimeSettings, error) {
	return s.Update(ctx, RuntimeUpdate{
		ContainerReportIntervalSeconds: input.ContainerReportIntervalSeconds,
		Language:                       input.Language,
		LogLevel:                       input.LogLevel,
		ReconcileTraceEnabled:          input.ReconcileTraceEnabled,
	})
}

// UpdateSecurity 只更新安全分区：令牌有效期与远程命令超时。
func (s *Service) UpdateSecurity(ctx context.Context, input RuntimeSecurityUpdate) (RuntimeSettings, error) {
	return s.Update(ctx, RuntimeUpdate{
		TokenExpiration:             input.TokenExpiration,
		RemoteCommandTimeoutSeconds: input.RemoteCommandTimeoutSeconds,
	})
}

// UpdateCertificates 只更新证书/Panel HTTPS 分区。Panel 选择仍走
// paneltls.WithUpdate 临界区与 fixed pair 回滚（SET-TLS-004）。
func (s *Service) UpdateCertificates(ctx context.Context, input RuntimeCertificatesUpdate) (RuntimeSettings, error) {
	return s.Update(ctx, RuntimeUpdate{
		Panel:        input.Panel,
		Certificates: input.Certificates,
	})
}

// UpdateAgent 只更新 Agent 投递分区。
func (s *Service) UpdateAgent(ctx context.Context, input RuntimeAgentSettings) (RuntimeSettings, error) {
	return s.Update(ctx, RuntimeUpdate{Agent: &input})
}

// UpdateImageRegistry 只更新镜像仓库源分区。省略的字段沿用当前快照；endpoint
// 显式空串表示回到官方仓库（合法），scheme 显式空串视为省略。
func (s *Service) UpdateImageRegistry(ctx context.Context, input RuntimeImageRegistryUpdate) (RuntimeSettings, error) {
	next := s.Runtime().ImageRegistry
	if input.Endpoint != nil {
		next.Endpoint = *input.Endpoint
	}
	if input.Scheme != nil {
		if scheme := strings.TrimSpace(*input.Scheme); scheme != "" {
			next.Scheme = scheme
		}
	}
	return s.Update(ctx, RuntimeUpdate{ImageRegistry: &next})
}

// UpdateBranding 只更新品牌分区（登录页标题与副标题）。
func (s *Service) UpdateBranding(ctx context.Context, input RuntimeBrandingSettings) (RuntimeSettings, error) {
	return s.Update(ctx, RuntimeUpdate{Branding: &input})
}

// UpdateAutoUpdate 只更新自动更新分区（SET-RUN-023）。省略的字段沿用当前
// 快照；布尔值用指针负载，关闭开关才能真正落库。
func (s *Service) UpdateAutoUpdate(ctx context.Context, input RuntimeAutoUpdateUpdate) (RuntimeSettings, error) {
	current := s.Runtime().AutoUpdate
	next := RuntimeAutoUpdateSettings{
		AppsEnabled:     current.AppsEnabled,
		PackagesEnabled: current.PackagesEnabled,
		PackagesMode:    current.PackagesMode,
		ScheduleTime:    current.ScheduleTime,
		LastRunAt:       current.LastRunAt,
		LastRunStatus:   current.LastRunStatus,
		LastRunTaskID:   current.LastRunTaskID,
	}
	if input.AppsEnabled != nil {
		next.AppsEnabled = *input.AppsEnabled
	}
	if input.PackagesEnabled != nil {
		next.PackagesEnabled = *input.PackagesEnabled
	}
	if input.PackagesMode != nil {
		next.PackagesMode = strings.TrimSpace(*input.PackagesMode)
	}
	if input.ScheduleTime != nil {
		next.ScheduleTime = strings.TrimSpace(*input.ScheduleTime)
	}
	return s.Update(ctx, RuntimeUpdate{AutoUpdate: &next})
}

// SetAutoUpdateRunner 注入手动「立即运行一次」的任务创建入口。装配顺序上
// autoupdate 服务在 settings 之后创建，因此用 setter 而不是构造 Option。
func (s *Service) SetAutoUpdateRunner(runner AutoUpdateManualRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoUpdateRunner = runner
}

// RunAutoUpdateNow 手动「立即运行一次」（SET-RUN-023）：在当前设置上叠加可选
// 临时覆盖后校验，再委托 autoupdate 模块创建 auto_update_run 任务（TriggerType
// 为 user，跳过计划时间窗口与当日去重）。临时覆盖不落库；两开关（含覆盖后）
// 都关时返回 422 auto_update_disabled。
func (s *Service) RunAutoUpdateNow(ctx context.Context, input RuntimeAutoUpdateRunRequest) (string, error) {
	s.mu.RLock()
	runner := s.autoUpdateRunner
	s.mu.RUnlock()
	if runner == nil {
		return "", panelerr.New(http.StatusServiceUnavailable, "auto_update_unavailable", "Automatic update runner is unavailable")
	}
	current := s.Runtime().AutoUpdate
	effective := RuntimeAutoUpdateSettings{
		AppsEnabled:     current.AppsEnabled,
		PackagesEnabled: current.PackagesEnabled,
		PackagesMode:    current.PackagesMode,
		ScheduleTime:    current.ScheduleTime,
	}
	if input.AppsEnabled != nil {
		effective.AppsEnabled = *input.AppsEnabled
	}
	if input.PackagesEnabled != nil {
		effective.PackagesEnabled = *input.PackagesEnabled
	}
	if input.PackagesMode != nil {
		// 与分区 PUT 一致：显式空串视为省略，保留当前范围。
		if mode := strings.TrimSpace(*input.PackagesMode); mode != "" {
			normalized := normalizeAutoUpdatePackagesMode(mode)
			if normalized == "" {
				return "", panelerr.Validation("invalid_auto_update_packages_mode", "Package update mode must be security or all")
			}
			effective.PackagesMode = normalized
		}
	}
	if !effective.AppsEnabled && !effective.PackagesEnabled {
		return "", panelerr.Validation("auto_update_disabled", "Automatic updates are disabled")
	}
	return runner.RunAutoUpdateNow(ctx, effective)
}

// AutoUpdateLastRun 返回自动更新协调任务持久化的只读运行态。调度器用它做
// 当日去重（任务历史只保留 24 小时，不能依赖任务查询）。
func (s *Service) AutoUpdateLastRun() RuntimeAutoUpdateRunState {
	rt := s.Runtime().AutoUpdate
	state := RuntimeAutoUpdateRunState{
		LastRunStatus: rt.LastRunStatus,
		LastRunTaskID: rt.LastRunTaskID,
	}
	if parsed, err := time.Parse(time.RFC3339Nano, rt.LastRunAt); err == nil {
		state.LastRunAt = parsed
	}
	return state
}

// RecordAutoUpdateLastRun 写入只读运行态键并同步内存快照。这些键刻意不进入
// runtimeValues：普通分区保存不得覆盖调度器刚写入的运行态。
func (s *Service) RecordAutoUpdateLastRun(ctx context.Context, state RuntimeAutoUpdateRunState) error {
	at := state.LastRunAt
	if at.IsZero() {
		at = time.Now()
	}
	values := map[string]string{
		RuntimeSettingAutoUpdateLastRunAt:     at.UTC().Format(time.RFC3339Nano),
		RuntimeSettingAutoUpdateLastRunStatus: strings.TrimSpace(state.LastRunStatus),
		RuntimeSettingAutoUpdateLastRunTaskID: strings.TrimSpace(state.LastRunTaskID),
	}
	if err := s.saveValues(ctx, values); err != nil {
		return err
	}
	s.mu.Lock()
	s.rt.AutoUpdate.LastRunAt = values[RuntimeSettingAutoUpdateLastRunAt]
	s.rt.AutoUpdate.LastRunStatus = values[RuntimeSettingAutoUpdateLastRunStatus]
	s.rt.AutoUpdate.LastRunTaskID = values[RuntimeSettingAutoUpdateLastRunTaskID]
	s.mu.Unlock()
	return nil
}

func (s *Service) syncPanelTLS(ctx context.Context, panel RuntimePanelSettings) error {
	if s.tlsAssets == nil {
		return nil
	}
	return s.tlsAssets.SyncPanelTLS(ctx, panel.Domain, panel.TLSCertificateID)
}

func (s *Service) SetJWTSecret(ctx context.Context, secret string) (RuntimeSettings, error) {
	secret = strings.TrimSpace(secret)
	if err := ValidateJWTSecret(secret); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.saveValues(ctx, map[string]string{RuntimeSettingJWTSecret: secret}); err != nil {
		return RuntimeSettings{}, err
	}
	return s.ApplyJWTSecret(secret), nil
}

// PersistJWTSecretTx writes the JWT secret into an existing AppDB transaction.
// Callers must invoke ApplyJWTSecret only after the transaction commits.
func (s *Service) PersistJWTSecretTx(ctx context.Context, tx *sql.Tx, secret string) error {
	secret = strings.TrimSpace(secret)
	if err := ValidateJWTSecret(secret); err != nil {
		return err
	}
	if tx == nil {
		return errors.New("jwt secret transaction is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := orm.RawExec(ctx, tx, `
		INSERT INTO runtime_settings(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, RuntimeSettingJWTSecret, secret, now)
	return err
}

// ApplyJWTSecret updates the in-memory runtime snapshot after a committed JWT
// secret write (standalone or as part of an atomic account update).
func (s *Service) ApplyJWTSecret(secret string) RuntimeSettings {
	secret = strings.TrimSpace(secret)
	s.mu.Lock()
	s.rt.JWTSecret = secret
	s.rt.JWTSecretConfigured = true
	out := s.rt
	s.mu.Unlock()
	return out
}

func (s *Service) ServerVariableDefinitions(ctx context.Context) ([]ServerVariableDefinition, error) {
	var raw string
	err := orm.New(s.db).From("runtime_settings").Where("key = ?", RuntimeSettingServerVariableDefinitions).Select("value").ScanValue(ctx, &raw)
	if err == sql.ErrNoRows || strings.TrimSpace(raw) == "" {
		return []ServerVariableDefinition{}, nil
	}
	if err != nil {
		return nil, err
	}
	var defs []ServerVariableDefinition
	if err := json.Unmarshal([]byte(raw), &defs); err != nil {
		return nil, err
	}
	return normalizeServerVariableDefinitions(defs)
}

func (s *Service) UpdateServerVariableDefinitions(ctx context.Context, input ServerVariableDefinitionsUpdate) ([]ServerVariableDefinition, error) {
	defs, err := normalizeServerVariableDefinitions(input.Definitions)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(defs)
	if err != nil {
		return nil, err
	}
	if err := s.saveValues(ctx, map[string]string{RuntimeSettingServerVariableDefinitions: string(raw)}); err != nil {
		return nil, err
	}
	return defs, nil
}

func (s *Service) ensureDefaultRuntimeSettings(ctx context.Context) error {
	defaults := defaultRuntimeSettings(s.cfg)
	if !s.hasConfiguredJWTSecret() {
		secret, err := randomJWTSecret()
		if err != nil {
			return err
		}
		defaults.JWTSecret = secret
		defaults.JWTSecretConfigured = true
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, value := range runtimeValues(defaults, true) {
		if key == RuntimeSettingJWTSecret {
			// 存量安装可能已在 runtime_settings 里固化旧默认密钥（升级前由旧
			// 代码写入）；检测到默认值就轮换为随机密钥，避免升级后继续用可预测
			// 的默认密钥签名会话。轮换会使既有会话令牌失效（需重新登录一次），
			// 属安全修复的预期副作用；轮换后值不再是默认值，后续启动不会重复。
			if _, err := orm.RawExec(ctx, s.db, `
				INSERT INTO runtime_settings(key, value, updated_at)
				VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET
					value=CASE WHEN runtime_settings.value='' OR runtime_settings.value=? THEN excluded.value ELSE runtime_settings.value END,
					updated_at=CASE WHEN runtime_settings.value='' OR runtime_settings.value=? THEN excluded.updated_at ELSE runtime_settings.updated_at END
			`, key, value, now, DefaultJWTSecret, DefaultJWTSecret); err != nil {
				return err
			}
			continue
		}
		if _, err := orm.RawExec(ctx, s.db, `
			INSERT INTO runtime_settings(key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO NOTHING
		`, key, value, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) saveValues(ctx context.Context, values map[string]string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		for key, value := range values {
			if _, err := orm.RawExec(ctx, tx, `
				INSERT INTO runtime_settings(key, value, updated_at)
				VALUES (?, ?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
			`, key, value, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) load(ctx context.Context) error {
	var rows []models.RuntimeSetting
	if err := orm.New(s.db).From("runtime_settings").All(ctx, &rows); err != nil {
		return err
	}
	next := defaultRuntimeSettings(s.cfg)
	for _, row := range rows {
		key, value := row.Key, row.Value
		switch key {
		case "containerReportIntervalSeconds":
			if n, err := strconv.Atoi(value); err == nil {
				next.ContainerReportIntervalSeconds = n
			}
		case RuntimeSettingTokenExpiration:
			if tokenExpiration := NormalizeTokenExpiration(value); tokenExpiration != "" {
				next.TokenExpiration = tokenExpiration
			}
		case "language":
			if locale := i18n.NormalizeLocale(value); locale != "" {
				next.Language = locale
			}
		case RuntimeSettingLogLevel:
			if level := logging.NormalizeLevel(value); level != "" {
				next.LogLevel = level
			}
		case RuntimeSettingJWTSecret:
			next.JWTSecret = value
			next.JWTSecretConfigured = strings.TrimSpace(value) != ""
		case RuntimeSettingRemoteCommandTimeoutSeconds:
			if n, err := strconv.Atoi(value); err == nil {
				next.RemoteCommandTimeoutSeconds = n
			}
		case RuntimeSettingReconcileTrace:
			next.ReconcileTraceEnabled = strings.TrimSpace(value) == "true"
		case RuntimeSettingBrandingLoginTitle:
			next.Branding.LoginTitle = value
		case RuntimeSettingBrandingLoginSubtitle:
			next.Branding.LoginSubtitle = value
		case RuntimeSettingCertificateEmail:
			next.Certificates.Email = value
		case RuntimeSettingCertificateDNSPropagationDelaySecond:
			if n, err := strconv.Atoi(value); err == nil {
				next.Certificates.DNSPropagationDelaySeconds = n
			}
		case RuntimeSettingPanelDomain:
			next.Panel.Domain = value
		case RuntimeSettingPanelTLSCertificateID:
			next.Panel.TLSCertificateID = value
		case RuntimeSettingAgentDownloadBaseURL:
			if baseURL, err := normalizeAgentDownloadBaseURL(value); err == nil {
				next.Agent.DownloadBaseURL = baseURL
			}
		case RuntimeSettingAgentDownloadVerifyTLS:
			next.Agent.DownloadVerifyTLS = strings.TrimSpace(value) == "true"
		case RuntimeSettingAgentTransferTimeoutSeconds:
			if n, err := strconv.Atoi(value); err == nil {
				next.Agent.TransferTimeoutSeconds = n
			}
		case RuntimeSettingImageRegistryEndpoint:
			// 旧库/手工改库的非法值静默回退到官方仓库，启动不失败。
			if endpoint, err := normalizeImageRegistryEndpoint(value); err == nil {
				next.ImageRegistry.Endpoint = endpoint
			}
		case RuntimeSettingImageRegistryScheme:
			if scheme := normalizeImageRegistryScheme(value); scheme != "" {
				next.ImageRegistry.Scheme = scheme
			}
		case RuntimeSettingAutoUpdateAppsEnabled:
			next.AutoUpdate.AppsEnabled = strings.TrimSpace(value) == "true"
		case RuntimeSettingAutoUpdatePackagesEnabled:
			next.AutoUpdate.PackagesEnabled = strings.TrimSpace(value) == "true"
		case RuntimeSettingAutoUpdatePackagesMode:
			// 旧库/手工改库的非法值静默归一：保留默认 security，启动不失败。
			if mode := normalizeAutoUpdatePackagesMode(value); mode != "" {
				next.AutoUpdate.PackagesMode = mode
			}
		case RuntimeSettingAutoUpdateScheduleTime:
			if schedule := strings.TrimSpace(value); autoUpdateScheduleTimePattern.MatchString(schedule) {
				next.AutoUpdate.ScheduleTime = schedule
			}
		case RuntimeSettingAutoUpdateLastRunAt:
			next.AutoUpdate.LastRunAt = strings.TrimSpace(value)
		case RuntimeSettingAutoUpdateLastRunStatus:
			next.AutoUpdate.LastRunStatus = strings.TrimSpace(value)
		case RuntimeSettingAutoUpdateLastRunTaskID:
			next.AutoUpdate.LastRunTaskID = strings.TrimSpace(value)
		}
	}
	if err := validateRuntimeSettings(next); err != nil {
		return err
	}
	if err := s.validatePanelTLS(next); err != nil {
		return err
	}
	s.mu.Lock()
	s.rt = next
	s.mu.Unlock()
	i18n.SetDefaultLocale(next.Language)
	_ = logging.SetLevel(next.LogLevel)
	reconciletrace.SetEnabled(next.ReconcileTraceEnabled)
	return nil
}

func defaultRuntimeSettings(cfg config.Config) RuntimeSettings {
	jwtSecret := firstNonEmpty(strings.TrimSpace(cfg.JWTSecret), DefaultJWTSecret)
	remoteTimeout := cfg.RemoteCommandTimeoutSeconds
	if remoteTimeout < 1 {
		remoteTimeout = 30
	}
	dnsDelay := cfg.Certificates.DNSPropagationDelaySeconds
	if dnsDelay < 0 {
		dnsDelay = 30
	}
	return RuntimeSettings{
		ListenAddress:                  cfg.ListenAddress,
		AppDatabase:                    cfg.AppDatabase,
		MetricsDatabase:                cfg.MetricsDatabase,
		DataRoot:                       cfg.DataRoot,
		ContainerReportIntervalSeconds: 30,
		TokenExpiration:                DefaultTokenExpiration,
		Language:                       i18n.DefaultLocale(),
		LogLevel:                       logging.DefaultLevel,
		RemoteCommandTimeoutSeconds:    remoteTimeout,
		Branding:                       RuntimeBrandingSettings{},
		Certificates: RuntimeCertificateSettings{
			Email:                      strings.TrimSpace(cfg.Certificates.Email),
			DNSPropagationDelaySeconds: dnsDelay,
		},
		Panel: RuntimePanelSettings{Domain: "localhost"},
		Agent: RuntimeAgentSettings{
			TransferTimeoutSeconds: DefaultAgentTransferTimeoutSeconds,
		},
		ImageRegistry: RuntimeImageRegistrySettings{
			Scheme: DefaultImageRegistryScheme,
		},
		AutoUpdate: RuntimeAutoUpdateSettings{
			PackagesMode: AutoUpdatePackagesModeSecurity,
			ScheduleTime: DefaultAutoUpdateScheduleTime,
		},
		JWTSecret:           jwtSecret,
		JWTSecretConfigured: jwtSecret != "",
	}
}

func runtimeValues(settings RuntimeSettings, includeJWT bool) map[string]string {
	values := map[string]string{
		"containerReportIntervalSeconds":                   strconv.Itoa(settings.ContainerReportIntervalSeconds),
		RuntimeSettingTokenExpiration:                      settings.TokenExpiration,
		"language":                                         settings.Language,
		RuntimeSettingLogLevel:                             settings.LogLevel,
		RuntimeSettingRemoteCommandTimeoutSeconds:          strconv.Itoa(settings.RemoteCommandTimeoutSeconds),
		RuntimeSettingReconcileTrace:                       strconv.FormatBool(settings.ReconcileTraceEnabled),
		RuntimeSettingBrandingLoginTitle:                   settings.Branding.LoginTitle,
		RuntimeSettingBrandingLoginSubtitle:                settings.Branding.LoginSubtitle,
		RuntimeSettingCertificateEmail:                     settings.Certificates.Email,
		RuntimeSettingCertificateDNSPropagationDelaySecond: strconv.Itoa(settings.Certificates.DNSPropagationDelaySeconds),
		RuntimeSettingPanelDomain:                          settings.Panel.Domain,
		RuntimeSettingPanelTLSCertificateID:                settings.Panel.TLSCertificateID,
		RuntimeSettingAgentDownloadBaseURL:                 settings.Agent.DownloadBaseURL,
		RuntimeSettingAgentDownloadVerifyTLS:               strconv.FormatBool(settings.Agent.DownloadVerifyTLS),
		RuntimeSettingAgentTransferTimeoutSeconds:          strconv.Itoa(settings.Agent.TransferTimeoutSeconds),
		RuntimeSettingImageRegistryEndpoint:                settings.ImageRegistry.Endpoint,
		RuntimeSettingImageRegistryScheme:                  settings.ImageRegistry.Scheme,
		RuntimeSettingAutoUpdateAppsEnabled:                strconv.FormatBool(settings.AutoUpdate.AppsEnabled),
		RuntimeSettingAutoUpdatePackagesEnabled:            strconv.FormatBool(settings.AutoUpdate.PackagesEnabled),
		RuntimeSettingAutoUpdatePackagesMode:               settings.AutoUpdate.PackagesMode,
		RuntimeSettingAutoUpdateScheduleTime:               settings.AutoUpdate.ScheduleTime,
	}
	if includeJWT {
		values[RuntimeSettingJWTSecret] = settings.JWTSecret
	}
	return values
}

func validateRuntimeSettings(settings RuntimeSettings) error {
	if settings.ContainerReportIntervalSeconds < 1 {
		return panelerr.Validation("invalid_container_report_interval", "Container report interval must be at least 1 second")
	}
	if tokenExpiration := NormalizeTokenExpiration(settings.TokenExpiration); tokenExpiration == "" {
		return panelerr.Validation("invalid_token_expiration", "Token expiration must be 10 minutes, 1 hour, 1 day, 5 days, 30 days, or never")
	}
	if strings.TrimSpace(settings.Language) == "" || i18n.NormalizeLocale(settings.Language) == "" {
		return panelerr.Validation("invalid_language", "Language must be English or Simplified Chinese")
	}
	if strings.TrimSpace(settings.LogLevel) == "" || logging.NormalizeLevel(settings.LogLevel) == "" {
		return panelerr.Validation("invalid_log_level", "Log level must be debug, info, warn, or error")
	}
	if settings.RemoteCommandTimeoutSeconds < 1 {
		return panelerr.Validation("invalid_remote_command_timeout", "Remote command timeout must be at least 1 second")
	}
	if utf8.RuneCountInString(settings.Branding.LoginTitle) > 80 {
		return panelerr.Validation("invalid_branding_login_title", "Login title must be 80 characters or fewer")
	}
	if utf8.RuneCountInString(settings.Branding.LoginSubtitle) > 240 {
		return panelerr.Validation("invalid_branding_login_subtitle", "Login subtitle must be 240 characters or fewer")
	}
	if settings.Certificates.DNSPropagationDelaySeconds < 0 {
		return panelerr.Validation("invalid_certificate_dns_delay", "Certificate DNS propagation delay cannot be negative")
	}
	if email := strings.TrimSpace(settings.Certificates.Email); email != "" {
		if _, err := mail.ParseAddress(email); err != nil {
			return panelerr.Validation("invalid_certificate_email", "Certificate email must be valid")
		}
	}
	if err := validatePanelDomain(settings.Panel.Domain); err != nil {
		return err
	}
	if err := validateAgentSettings(settings.Agent); err != nil {
		return err
	}
	if err := validateImageRegistrySettings(settings.ImageRegistry); err != nil {
		return err
	}
	if err := validateAutoUpdateSettings(settings.AutoUpdate); err != nil {
		return err
	}
	return ValidateJWTSecret(settings.JWTSecret)
}

// validateAutoUpdateSettings 校验自动更新分区的枚举与时间格式（SET-RUN-023）。
// 运行态 lastRun* 只由协调任务写入，不在这里校验。
func validateAutoUpdateSettings(settings RuntimeAutoUpdateSettings) error {
	switch settings.PackagesMode {
	case AutoUpdatePackagesModeSecurity, AutoUpdatePackagesModeAll:
	default:
		return panelerr.Validation("invalid_auto_update_packages_mode", "Package update mode must be security or all")
	}
	if !autoUpdateScheduleTimePattern.MatchString(strings.TrimSpace(settings.ScheduleTime)) {
		return panelerr.Validation("invalid_auto_update_schedule_time", "Automatic update schedule time must be in HH:MM format")
	}
	return nil
}

// normalizeAutoUpdatePackagesMode 把合法模式归一为稳定小写枚举；非法值返回空串，
// 调用方据此决定是保留默认值（load）还是交给校验报错（Update）。
func normalizeAutoUpdatePackagesMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AutoUpdatePackagesModeSecurity:
		return AutoUpdatePackagesModeSecurity
	case AutoUpdatePackagesModeAll:
		return AutoUpdatePackagesModeAll
	default:
		return ""
	}
}

// ParseAutoUpdateScheduleTime 解析每日自动更新执行时间 HH:MM（24 小时制）。
// 调度器与设置校验共用同一份格式判定，避免两处漂移。
func ParseAutoUpdateScheduleTime(value string) (hour, minute int, ok bool) {
	trimmed := strings.TrimSpace(value)
	if !autoUpdateScheduleTimePattern.MatchString(trimmed) {
		return 0, 0, false
	}
	hour, _ = strconv.Atoi(trimmed[0:2])
	minute, _ = strconv.Atoi(trimmed[3:5])
	return hour, minute, true
}

func validateAgentSettings(settings RuntimeAgentSettings) error {
	if _, err := normalizeAgentDownloadBaseURL(settings.DownloadBaseURL); err != nil {
		return err
	}
	if settings.TransferTimeoutSeconds < MinAgentTransferTimeoutSeconds || settings.TransferTimeoutSeconds > MaxAgentTransferTimeoutSeconds {
		return panelerr.Validation("invalid_agent_transfer_timeout", "Agent transfer timeout must be between 60 and 3600 seconds")
	}
	return nil
}

// normalizeAgentDownloadBaseURL validates and normalizes the public base URL an
// agent bundle is downloaded from. An empty value disables HTTP delivery.
//
// The value is embedded into a remote shell command, so quotes, whitespace and
// shell metacharacters are rejected outright rather than escaped: a base URL
// never legitimately contains them. The value is also restricted to a bare
// origin, because the Panel appends the artifact path itself and must not let a
// configured prefix change which path is served.
func normalizeAgentDownloadBaseURL(value string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return "", nil
	}
	if strings.ContainsAny(trimmed, " \t\r\n\"'`\\$;&|<>(){}[]*?!") {
		return "", panelerr.Validation("invalid_agent_download_base_url", "Agent download base URL must not contain whitespace, quotes or shell metacharacters")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", panelerr.Validation("invalid_agent_download_base_url", "Agent download base URL must be a valid http or https origin")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", panelerr.Validation("invalid_agent_download_base_url", "Agent download base URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", panelerr.Validation("invalid_agent_download_base_url", "Agent download base URL must be a bare origin without a path, query, fragment or credentials")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// validateImageRegistrySettings 校验镜像仓库源分区：endpoint 为空或裸
// host[:port]，scheme 仅 http/https。
func validateImageRegistrySettings(settings RuntimeImageRegistrySettings) error {
	if _, err := normalizeImageRegistryEndpoint(settings.Endpoint); err != nil {
		return err
	}
	if normalizeImageRegistryScheme(settings.Scheme) == "" {
		return panelerr.Validation("invalid_image_registry_scheme", "Image registry scheme must be http or https")
	}
	return nil
}

// normalizeImageRegistryScheme 把合法协议归一为稳定小写枚举；非法值返回空串，
// 调用方据此决定是保留默认值（load）还是交给校验报错（Update）。
func normalizeImageRegistryScheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ImageRegistrySchemeHTTP:
		return ImageRegistrySchemeHTTP
	case ImageRegistrySchemeHTTPS:
		return ImageRegistrySchemeHTTPS
	default:
		return ""
	}
}

// normalizeImageRegistryEndpoint 校验并归一化镜像仓库地址。空串合法，表示使用
// 官方默认仓库；非空值必须是裸 host[:port]：不允许携带 scheme、path、query、
// userinfo、空白、引号或反斜杠，端口范围为 1..65535。带端口的 IPv6 字面量必须
// 加方括号，避免与 host:port 产生歧义。
func normalizeImageRegistryEndpoint(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "", nil
	}
	if strings.ContainsAny(trimmed, " \t\r\n\"'`\\/?#@") {
		return "", invalidImageRegistryEndpoint()
	}
	if strings.HasPrefix(trimmed, "[") {
		return normalizeBracketedImageRegistryEndpoint(trimmed)
	}
	if strings.Count(trimmed, ":") > 1 {
		return "", invalidImageRegistryEndpoint()
	}
	host, port := trimmed, ""
	hasPort := false
	if index := strings.Index(trimmed, ":"); index >= 0 {
		host, port, hasPort = trimmed[:index], trimmed[index+1:], true
	}
	if err := validateImageRegistryHost(host); err != nil {
		return "", err
	}
	if !hasPort {
		return host, nil
	}
	normalizedPort, err := normalizeImageRegistryPort(port)
	if err != nil {
		return "", err
	}
	return host + ":" + normalizedPort, nil
}

func normalizeBracketedImageRegistryEndpoint(value string) (string, error) {
	closing := strings.Index(value, "]")
	if closing < 0 {
		return "", invalidImageRegistryEndpoint()
	}
	host := value[1:closing]
	rest := value[closing+1:]
	if net.ParseIP(host) == nil || !strings.Contains(host, ":") {
		return "", invalidImageRegistryEndpoint()
	}
	normalized := "[" + host + "]"
	if rest == "" {
		return normalized, nil
	}
	if !strings.HasPrefix(rest, ":") {
		return "", invalidImageRegistryEndpoint()
	}
	port, err := normalizeImageRegistryPort(rest[1:])
	if err != nil {
		return "", err
	}
	return normalized + ":" + port, nil
}

func validateImageRegistryHost(host string) error {
	if host == "" || len(host) > 253 {
		return invalidImageRegistryEndpoint()
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	// 解析失败的纯数字/点串（如 999.999.999.999）既不是 IP 也不是合法主机名。
	if isNumericHost(host) {
		return invalidImageRegistryEndpoint()
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return invalidImageRegistryEndpoint()
		}
		for i := 0; i < len(label); i++ {
			ch := label[i]
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return invalidImageRegistryEndpoint()
			}
		}
	}
	return nil
}

func isNumericHost(host string) bool {
	if host == "" {
		return false
	}
	for i := 0; i < len(host); i++ {
		ch := host[i]
		if (ch < '0' || ch > '9') && ch != '.' {
			return false
		}
	}
	return true
}

func normalizeImageRegistryPort(value string) (string, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", invalidImageRegistryEndpoint()
	}
	return strconv.Itoa(port), nil
}

func invalidImageRegistryEndpoint() error {
	return panelerr.Validation("invalid_image_registry_endpoint", "Image registry endpoint must be a bare host or host:port without a scheme, path, query, credentials or whitespace")
}

func (s *Service) validatePanelTLS(settings RuntimeSettings) error {
	if strings.TrimSpace(settings.Panel.TLSCertificateID) == "" {
		return nil
	}
	if s.tlsAssets == nil {
		return panelerr.Validation("invalid_panel_tls_certificate", "Panel TLS certificate provider is unavailable")
	}
	id := strings.TrimSpace(settings.Panel.TLSCertificateID)
	assetType, err := s.tlsAssets.AssetType(context.Background(), id)
	if err != nil {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate is unavailable")
	}
	if assetType != "tls_certificate" {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel certificate must be a TLS certificate")
	}
	certPEM, _, err := s.tlsAssets.ReadFile(context.Background(), id, "certificate")
	if err != nil {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate cannot be read")
	}
	keyPEM, _, err := s.tlsAssets.ReadFile(context.Background(), id, "private_key")
	if err != nil {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS private key cannot be read")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil || len(cert.Certificate) == 0 {
		return panelerr.Validation("invalid_panel_tls_certificate", "Selected Panel TLS certificate and private key do not match")
	}
	if err := paneltls.ValidateListenerCertificate(cert, ""); err != nil {
		return panelerr.Validation("invalid_panel_tls_certificate", err.Error())
	}
	return nil
}

func validatePanelDomain(value string) error {
	domain := strings.ToLower(strings.TrimSpace(value))
	if domain == "" || strings.ContainsAny(domain, "/\\?#@ :") || strings.Contains(domain, "..") {
		return panelerr.Validation("invalid_panel_domain", "Panel domain must be a valid hostname or IP address")
	}
	if net.ParseIP(domain) == nil && domain != "localhost" && (!strings.Contains(domain, ".") || len(domain) > 253) {
		return panelerr.Validation("invalid_panel_domain", "Panel domain must be a valid hostname or IP address")
	}
	return nil
}

func normalizeServerVariableDefinitions(in []ServerVariableDefinition) ([]ServerVariableDefinition, error) {
	out := make([]ServerVariableDefinition, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		name := strings.TrimSpace(item.Name)
		key := strings.TrimSpace(item.Key)
		if name == "" {
			return nil, panelerr.Validation("invalid_server_variable_name", "Server variable display name is required")
		}
		if !serverVariableKeyPattern.MatchString(key) {
			return nil, panelerr.Validation("invalid_server_variable_key", "Server variable key must start with a letter or underscore and contain only letters, digits, or underscores")
		}
		if _, ok := seen[key]; ok {
			return nil, panelerr.Validation("duplicate_server_variable_key", "Server variable key must be unique")
		}
		seen[key] = struct{}{}
		out = append(out, ServerVariableDefinition{Name: name, Key: key, Required: item.Required})
	}
	return out, nil
}

func ValidateJWTSecret(secret string) error {
	if len(strings.TrimSpace(secret)) < 16 {
		return panelerr.Validation("invalid_jwt_secret", "JWT secret must be at least 16 characters")
	}
	return nil
}

func NormalizeTokenExpiration(value string) string {
	switch value {
	case TokenExpiration10Minutes, TokenExpiration1Hour, TokenExpiration1Day, TokenExpiration5Days, TokenExpiration30Days, TokenExpirationNever:
		return value
	default:
		return ""
	}
}

func TokenExpirationDuration(value string) (time.Duration, bool) {
	switch NormalizeTokenExpiration(value) {
	case TokenExpiration10Minutes:
		return 10 * time.Minute, true
	case TokenExpiration1Hour:
		return time.Hour, true
	case TokenExpiration1Day:
		return 24 * time.Hour, true
	case TokenExpiration5Days:
		return 5 * 24 * time.Hour, true
	case TokenExpiration30Days:
		return 30 * 24 * time.Hour, true
	case TokenExpirationNever:
		return 0, true
	default:
		return 0, false
	}
}

// hasConfiguredJWTSecret reports whether the JWT secret was explicitly set in
// the config file or environment rather than left at the public default
// constant. Explicitly configured secrets are treated as intentional and are
// preserved; the default constant is randomized on first startup instead.
func (s *Service) hasConfiguredJWTSecret() bool {
	secret := strings.TrimSpace(s.cfg.JWTSecret)
	return secret != "" && secret != DefaultJWTSecret
}

func randomJWTSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
