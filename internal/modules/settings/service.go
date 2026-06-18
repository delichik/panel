package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"panel/internal/platform/config"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	"panel/internal/platform/logging"
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
	MetricsCollectionIntervalSeconds int                         `json:"metricsCollectionIntervalSeconds"`
	CleanupSchedule                  string                      `json:"cleanupSchedule"`
	TokenExpiration                  string                      `json:"tokenExpiration"`
	Language                         string                      `json:"language"`
	LogLevel                         string                      `json:"logLevel"`
	RemoteCommandTimeoutSeconds      int                         `json:"remoteCommandTimeoutSeconds"`
	Branding                         *RuntimeBrandingSettings    `json:"branding"`
	Certificates                     *RuntimeCertificateSettings `json:"certificates"`
}

type RuntimeSettings struct {
	ListenAddress                    string                     `json:"listenAddress"`
	AppDatabase                      string                     `json:"appDatabase"`
	MetricsDatabase                  string                     `json:"metricsDatabase"`
	DataRoot                         string                     `json:"dataRoot"`
	MetricsRetentionDays             int                        `json:"metricsRetentionDays"`
	MetricsCollectionIntervalSeconds int                        `json:"metricsCollectionIntervalSeconds"`
	CleanupSchedule                  string                     `json:"cleanupSchedule"`
	TokenExpiration                  string                     `json:"tokenExpiration"`
	Language                         string                     `json:"language"`
	LogLevel                         string                     `json:"logLevel"`
	RemoteCommandTimeoutSeconds      int                        `json:"remoteCommandTimeoutSeconds"`
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
		MetricsCollectionIntervalSeconds: input.MetricsCollectionIntervalSeconds,
		CleanupSchedule:                  input.CleanupSchedule,
		TokenExpiration:                  NormalizeTokenExpiration(input.TokenExpiration),
		Language:                         i18n.NormalizeLocale(input.Language),
		LogLevel:                         logging.NormalizeLevel(input.LogLevel),
		RemoteCommandTimeoutSeconds:      input.RemoteCommandTimeoutSeconds,
		Branding:                         brandingSettings,
		Certificates:                     certSettings,
		JWTSecret:                        current.JWTSecret,
		JWTSecretConfigured:              current.JWTSecretConfigured,
	}
	if err := validateRuntimeSettings(next); err != nil {
		return RuntimeSettings{}, err
	}
	if err := s.saveValues(ctx, runtimeValues(next, false)); err != nil {
		return RuntimeSettings{}, err
	}
	s.mu.Lock()
	s.rt.MetricsRetentionDays = next.MetricsRetentionDays
	s.rt.MetricsCollectionIntervalSeconds = next.MetricsCollectionIntervalSeconds
	s.rt.CleanupSchedule = next.CleanupSchedule
	s.rt.TokenExpiration = next.TokenExpiration
	s.rt.Language = next.Language
	s.rt.LogLevel = next.LogLevel
	s.rt.RemoteCommandTimeoutSeconds = next.RemoteCommandTimeoutSeconds
	s.rt.Branding = next.Branding
	s.rt.Certificates = next.Certificates
	out := s.rt
	s.mu.Unlock()
	i18n.SetDefaultLocale(out.Language)
	_ = logging.SetLevel(out.LogLevel)
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
	err := s.db.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key=?`, RuntimeSettingServerVariableDefinitions).Scan(&raw)
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, value := range runtimeValues(defaults, true) {
		if _, err := s.db.ExecContext(ctx, `
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO runtime_settings(key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, key, value, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Service) load(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM runtime_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := defaultRuntimeSettings(s.cfg)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		switch key {
		case "metricsRetentionDays":
			if n, err := strconv.Atoi(value); err == nil {
				next.MetricsRetentionDays = n
			}
		case "metricsCollectionIntervalSeconds":
			if n, err := strconv.Atoi(value); err == nil {
				next.MetricsCollectionIntervalSeconds = n
			}
		case "cleanupSchedule":
			next.CleanupSchedule = value
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
	if err := rows.Err(); err != nil {
		return err
	}
	if err := validateRuntimeSettings(next); err != nil {
		return err
	}
	s.mu.Lock()
	s.rt = next
	s.mu.Unlock()
	i18n.SetDefaultLocale(next.Language)
	_ = logging.SetLevel(next.LogLevel)
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
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "daily",
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
		"metricsCollectionIntervalSeconds":                 strconv.Itoa(settings.MetricsCollectionIntervalSeconds),
		"cleanupSchedule":                                  settings.CleanupSchedule,
		RuntimeSettingTokenExpiration:                      settings.TokenExpiration,
		"language":                                         settings.Language,
		RuntimeSettingLogLevel:                             settings.LogLevel,
		RuntimeSettingRemoteCommandTimeoutSeconds:          strconv.Itoa(settings.RemoteCommandTimeoutSeconds),
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
	if settings.MetricsCollectionIntervalSeconds < 10 {
		return panelerr.Validation("invalid_metrics_interval", "Metrics collection interval must be at least 10 seconds")
	}
	switch settings.CleanupSchedule {
	case "hourly", "daily", "weekly":
	default:
		return panelerr.Validation("invalid_cleanup_schedule", "Cleanup schedule must be hourly, daily, or weekly")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
