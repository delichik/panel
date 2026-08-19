package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/mail"
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
	"panel/internal/platform/reconciletrace"
)

var serverVariableKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type RuntimeCertificateSettings struct {
	Email                      string `json:"email"`
	DNSPropagationDelaySeconds int    `json:"dnsPropagationDelaySeconds"`
}

type RuntimeBrandingSettings struct {
	LoginTitle    string `json:"loginTitle"`
	LoginSubtitle string `json:"loginSubtitle"`
}

type ServerVariableDefinition struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Required bool   `json:"required"`
}

type ServerVariableDefinitionsUpdate struct {
	Definitions []ServerVariableDefinition `json:"definitions"`
}

type RuntimeUpdate struct {
	MetricsRetentionDays             int                         `json:"metricsRetentionDays"`
	RuntimeEventRetentionDays        int                         `json:"runtimeEventRetentionDays"`
	RuntimeEventDetailRetentionDays  int                         `json:"runtimeEventDetailRetentionDays"`
	MetricsCollectionIntervalSeconds int                         `json:"metricsCollectionIntervalSeconds"`
	ContainerReportIntervalSeconds   int                         `json:"containerReportIntervalSeconds"`
	CleanupSchedule                  string                      `json:"cleanupSchedule"`
	RuntimeEventCleanupSchedule      string                      `json:"runtimeEventCleanupSchedule"`
	TokenExpiration                  string                      `json:"tokenExpiration"`
	Language                         string                      `json:"language"`
	LogLevel                         string                      `json:"logLevel"`
	RemoteCommandTimeoutSeconds      int                         `json:"remoteCommandTimeoutSeconds"`
	ReconcileTraceEnabled            *bool                       `json:"reconcileTraceEnabled"`
	Branding                         *RuntimeBrandingSettings    `json:"branding"`
	Certificates                     *RuntimeCertificateSettings `json:"certificates"`
}

type RuntimeSettings struct {
	ListenAddress                    string                     `json:"listenAddress"`
	AppDatabase                      string                     `json:"appDatabase"`
	MetricsDatabase                  string                     `json:"metricsDatabase"`
	DataRoot                         string                     `json:"dataRoot"`
	MetricsRetentionDays             int                        `json:"metricsRetentionDays"`
	RuntimeEventRetentionDays        int                        `json:"runtimeEventRetentionDays"`
	RuntimeEventDetailRetentionDays  int                        `json:"runtimeEventDetailRetentionDays"`
	MetricsCollectionIntervalSeconds int                        `json:"metricsCollectionIntervalSeconds"`
	ContainerReportIntervalSeconds   int                        `json:"containerReportIntervalSeconds"`
	CleanupSchedule                  string                     `json:"cleanupSchedule"`
	RuntimeEventCleanupSchedule      string                     `json:"runtimeEventCleanupSchedule"`
	TokenExpiration                  string                     `json:"tokenExpiration"`
	Language                         string                     `json:"language"`
	LogLevel                         string                     `json:"logLevel"`
	RemoteCommandTimeoutSeconds      int                        `json:"remoteCommandTimeoutSeconds"`
	ReconcileTraceEnabled            bool                       `json:"reconcileTraceEnabled"`
	Branding                         RuntimeBrandingSettings    `json:"branding"`
	Certificates                     RuntimeCertificateSettings `json:"certificates"`
	JWTSecret                        string                     `json:"-"`
	JWTSecretConfigured              bool                       `json:"jwtSecretConfigured"`
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
	db  *sql.DB
	cfg config.Config
	mu  sync.RWMutex
	rt  RuntimeSettings
}

func NewService(db *sql.DB, cfg config.Config) (*Service, error) {
	s := &Service{db: db, cfg: cfg, rt: defaultRuntimeSettings(cfg)}
	if err := s.ensureDefaultRuntimeSettings(context.Background()); err != nil {
		return nil, err
	}
	if err := s.load(context.Background()); err != nil {
		return nil, err
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
	if input.RuntimeEventRetentionDays == 0 {
		input.RuntimeEventRetentionDays = current.RuntimeEventRetentionDays
	}
	if input.RuntimeEventDetailRetentionDays == 0 {
		input.RuntimeEventDetailRetentionDays = current.RuntimeEventDetailRetentionDays
	}
	if input.RuntimeEventCleanupSchedule == "" {
		input.RuntimeEventCleanupSchedule = current.RuntimeEventCleanupSchedule
	}
	certSettings := current.Certificates
	if input.Certificates != nil {
		certSettings = *input.Certificates
	}
	brandingSettings := current.Branding
	if input.Branding != nil {
		brandingSettings = RuntimeBrandingSettings{
			LoginTitle:    strings.TrimSpace(input.Branding.LoginTitle),
			LoginSubtitle: strings.TrimSpace(input.Branding.LoginSubtitle),
		}
	}
	next := RuntimeSettings{
		ListenAddress:                    current.ListenAddress,
		AppDatabase:                      current.AppDatabase,
		MetricsDatabase:                  current.MetricsDatabase,
		DataRoot:                         current.DataRoot,
		MetricsRetentionDays:             input.MetricsRetentionDays,
		RuntimeEventRetentionDays:        input.RuntimeEventRetentionDays,
		RuntimeEventDetailRetentionDays:  input.RuntimeEventDetailRetentionDays,
		MetricsCollectionIntervalSeconds: input.MetricsCollectionIntervalSeconds,
		ContainerReportIntervalSeconds:   input.ContainerReportIntervalSeconds,
		CleanupSchedule:                  input.CleanupSchedule,
		RuntimeEventCleanupSchedule:      input.RuntimeEventCleanupSchedule,
		TokenExpiration:                  NormalizeTokenExpiration(input.TokenExpiration),
		Language:                         i18n.NormalizeLocale(input.Language),
		LogLevel:                         logging.NormalizeLevel(input.LogLevel),
		RemoteCommandTimeoutSeconds:      input.RemoteCommandTimeoutSeconds,
		ReconcileTraceEnabled:            current.ReconcileTraceEnabled,
		Branding:                         brandingSettings,
		Certificates:                     certSettings,
		JWTSecret:                        current.JWTSecret,
		JWTSecretConfigured:              current.JWTSecretConfigured,
	}
	if input.ReconcileTraceEnabled != nil {
		next.ReconcileTraceEnabled = *input.ReconcileTraceEnabled
	}
	if err := validateRuntimeSettings(next); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.saveValues(ctx, runtimeValues(next, false)); err != nil {
		return RuntimeSettings{}, err
	}
	s.mu.Lock()
	s.rt.MetricsRetentionDays = next.MetricsRetentionDays
	s.rt.RuntimeEventRetentionDays = next.RuntimeEventRetentionDays
	s.rt.RuntimeEventDetailRetentionDays = next.RuntimeEventDetailRetentionDays
	s.rt.MetricsCollectionIntervalSeconds = next.MetricsCollectionIntervalSeconds
	s.rt.ContainerReportIntervalSeconds = next.ContainerReportIntervalSeconds
	s.rt.CleanupSchedule = next.CleanupSchedule
	s.rt.RuntimeEventCleanupSchedule = next.RuntimeEventCleanupSchedule
	s.rt.TokenExpiration = next.TokenExpiration
	s.rt.Language = next.Language
	s.rt.LogLevel = next.LogLevel
	s.rt.RemoteCommandTimeoutSeconds = next.RemoteCommandTimeoutSeconds
	s.rt.ReconcileTraceEnabled = next.ReconcileTraceEnabled
	s.rt.Branding = next.Branding
	s.rt.Certificates = next.Certificates
	out := s.rt
	s.mu.Unlock()
	i18n.SetDefaultLocale(out.Language)
	_ = logging.SetLevel(out.LogLevel)
	reconciletrace.SetEnabled(out.ReconcileTraceEnabled)
	return out, nil
}

func (s *Service) SetJWTSecret(ctx context.Context, secret string) (RuntimeSettings, error) {
	secret = strings.TrimSpace(secret)
	if err := ValidateJWTSecret(secret); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.saveValues(ctx, map[string]string{RuntimeSettingJWTSecret: secret}); err != nil {
		return RuntimeSettings{}, err
	}
	s.mu.Lock()
	s.rt.JWTSecret = secret
	s.rt.JWTSecretConfigured = true
	out := s.rt
	s.mu.Unlock()
	return out, nil
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
		case "metricsRetentionDays":
			if n, err := strconv.Atoi(value); err == nil {
				next.MetricsRetentionDays = n
			}
		case "runtimeEventRetentionDays":
			if n, err := strconv.Atoi(value); err == nil {
				next.RuntimeEventRetentionDays = n
			}
		case "runtimeEventDetailRetentionDays":
			if n, err := strconv.Atoi(value); err == nil {
				next.RuntimeEventDetailRetentionDays = n
			}
		case "metricsCollectionIntervalSeconds":
			if n, err := strconv.Atoi(value); err == nil {
				next.MetricsCollectionIntervalSeconds = n
			}
		case "containerReportIntervalSeconds":
			if n, err := strconv.Atoi(value); err == nil {
				next.ContainerReportIntervalSeconds = n
			}
		case "cleanupSchedule":
			next.CleanupSchedule = value
		case "runtimeEventCleanupSchedule":
			next.RuntimeEventCleanupSchedule = value
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
		}
	}
	if err := validateRuntimeSettings(next); err != nil {
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
		ListenAddress:                    cfg.ListenAddress,
		AppDatabase:                      cfg.AppDatabase,
		MetricsDatabase:                  cfg.MetricsDatabase,
		DataRoot:                         cfg.DataRoot,
		MetricsRetentionDays:             7,
		RuntimeEventRetentionDays:        30,
		RuntimeEventDetailRetentionDays:  7,
		MetricsCollectionIntervalSeconds: 60,
		ContainerReportIntervalSeconds:   30,
		CleanupSchedule:                  "daily",
		RuntimeEventCleanupSchedule:      "daily",
		TokenExpiration:                  DefaultTokenExpiration,
		Language:                         i18n.DefaultLocale(),
		LogLevel:                         logging.DefaultLevel,
		RemoteCommandTimeoutSeconds:      remoteTimeout,
		Branding:                         RuntimeBrandingSettings{},
		Certificates: RuntimeCertificateSettings{
			Email:                      strings.TrimSpace(cfg.Certificates.Email),
			DNSPropagationDelaySeconds: dnsDelay,
		},
		JWTSecret:           jwtSecret,
		JWTSecretConfigured: jwtSecret != "",
	}
}

func runtimeValues(settings RuntimeSettings, includeJWT bool) map[string]string {
	values := map[string]string{
		"metricsRetentionDays":                             strconv.Itoa(settings.MetricsRetentionDays),
		"runtimeEventRetentionDays":                        strconv.Itoa(settings.RuntimeEventRetentionDays),
		"runtimeEventDetailRetentionDays":                  strconv.Itoa(settings.RuntimeEventDetailRetentionDays),
		"metricsCollectionIntervalSeconds":                 strconv.Itoa(settings.MetricsCollectionIntervalSeconds),
		"containerReportIntervalSeconds":                   strconv.Itoa(settings.ContainerReportIntervalSeconds),
		"cleanupSchedule":                                  settings.CleanupSchedule,
		"runtimeEventCleanupSchedule":                      settings.RuntimeEventCleanupSchedule,
		RuntimeSettingTokenExpiration:                      settings.TokenExpiration,
		"language":                                         settings.Language,
		RuntimeSettingLogLevel:                             settings.LogLevel,
		RuntimeSettingRemoteCommandTimeoutSeconds:          strconv.Itoa(settings.RemoteCommandTimeoutSeconds),
		RuntimeSettingReconcileTrace:                       strconv.FormatBool(settings.ReconcileTraceEnabled),
		RuntimeSettingBrandingLoginTitle:                   settings.Branding.LoginTitle,
		RuntimeSettingBrandingLoginSubtitle:                settings.Branding.LoginSubtitle,
		RuntimeSettingCertificateEmail:                     settings.Certificates.Email,
		RuntimeSettingCertificateDNSPropagationDelaySecond: strconv.Itoa(settings.Certificates.DNSPropagationDelaySeconds),
	}
	if includeJWT {
		values[RuntimeSettingJWTSecret] = settings.JWTSecret
	}
	return values
}

func validateRuntimeSettings(settings RuntimeSettings) error {
	if settings.MetricsRetentionDays < 1 {
		return panelerr.Validation("invalid_metrics_retention", "Metrics retention must be at least 1 day")
	}
	if settings.RuntimeEventRetentionDays < 1 {
		return panelerr.Validation("invalid_runtime_event_retention", "Runtime event retention must be at least 1 day")
	}
	if settings.RuntimeEventDetailRetentionDays < 1 {
		return panelerr.Validation("invalid_runtime_event_detail_retention", "Runtime event detail retention must be at least 1 day")
	}
	if settings.RuntimeEventRetentionDays < settings.RuntimeEventDetailRetentionDays {
		return panelerr.Validation("invalid_runtime_event_retention_order", "Runtime event retention must be greater than or equal to detail retention")
	}
	if settings.MetricsCollectionIntervalSeconds < 1 {
		return panelerr.Validation("invalid_metrics_interval", "Metrics collection interval must be at least 1 second")
	}
	if settings.ContainerReportIntervalSeconds < 1 {
		return panelerr.Validation("invalid_container_report_interval", "Container report interval must be at least 1 second")
	}
	switch settings.CleanupSchedule {
	case "hourly", "daily", "weekly":
	default:
		return panelerr.Validation("invalid_cleanup_schedule", "Cleanup schedule must be hourly, daily, or weekly")
	}
	switch settings.RuntimeEventCleanupSchedule {
	case "hourly", "daily", "weekly":
	default:
		return panelerr.Validation("invalid_runtime_event_cleanup_schedule", "Runtime event cleanup schedule must be hourly, daily, or weekly")
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
	return ValidateJWTSecret(settings.JWTSecret)
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
