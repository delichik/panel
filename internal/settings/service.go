package settings

import (
	"context"
	"database/sql"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/config"
	"panel/internal/i18n"
	"panel/internal/panelerr"
)

type RuntimeNomadSettings struct {
	Namespace  string `json:"namespace"`
	Region     string `json:"region"`
	Datacenter string `json:"datacenter"`
}

type RuntimeCertificateSettings struct {
	Email                      string `json:"email"`
	DNSPropagationDelaySeconds int    `json:"dnsPropagationDelaySeconds"`
}

type RuntimeUpdate struct {
	MetricsRetentionDays             int                         `json:"metricsRetentionDays"`
	MetricsCollectionIntervalSeconds int                         `json:"metricsCollectionIntervalSeconds"`
	CleanupSchedule                  string                      `json:"cleanupSchedule"`
	TokenExpiration                  string                      `json:"tokenExpiration"`
	Language                         string                      `json:"language"`
	RemoteCommandTimeoutSeconds      int                         `json:"remoteCommandTimeoutSeconds"`
	Nomad                            *RuntimeNomadSettings       `json:"nomad"`
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
	RemoteCommandTimeoutSeconds      int                        `json:"remoteCommandTimeoutSeconds"`
	Nomad                            RuntimeNomadSettings       `json:"nomad"`
	Certificates                     RuntimeCertificateSettings `json:"certificates"`
	JWTSecret                        string                     `json:"-"`
	JWTSecretConfigured              bool                       `json:"jwtSecretConfigured"`
}

const (
	RuntimeSettingTokenExpiration                      = "tokenExpiration"
	RuntimeSettingJWTSecret                            = "jwtSecret"
	RuntimeSettingRemoteCommandTimeoutSeconds          = "remoteCommandTimeoutSeconds"
	RuntimeSettingNomadNamespace                       = "nomad.namespace"
	RuntimeSettingNomadRegion                          = "nomad.region"
	RuntimeSettingNomadDatacenter                      = "nomad.datacenter"
	RuntimeSettingCertificateEmail                     = "certificates.email"
	RuntimeSettingCertificateDNSPropagationDelaySecond = "certificates.dnsPropagationDelaySeconds"

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

func (s *Service) NomadConfig(base config.NomadConfig) config.NomadConfig {
	rt := s.Runtime()
	base.Namespace = firstNonEmpty(strings.TrimSpace(rt.Nomad.Namespace), base.Namespace, "default")
	base.Region = firstNonEmpty(strings.TrimSpace(rt.Nomad.Region), base.Region, "global")
	base.Datacenter = firstNonEmpty(strings.TrimSpace(rt.Nomad.Datacenter), base.Datacenter, "dc1")
	return base
}

func (s *Service) ApplicationNomadConfig() RuntimeNomadSettings {
	rt := s.Runtime()
	return RuntimeNomadSettings{
		Namespace:  firstNonEmpty(strings.TrimSpace(rt.Nomad.Namespace), "default"),
		Region:     firstNonEmpty(strings.TrimSpace(rt.Nomad.Region), "global"),
		Datacenter: firstNonEmpty(strings.TrimSpace(rt.Nomad.Datacenter), "dc1"),
	}
}

func (s *Service) ApplyToConfig(base config.Config) config.Config {
	rt := s.Runtime()
	base.RemoteCommandTimeoutSeconds = rt.RemoteCommandTimeoutSeconds
	base.Nomad = s.NomadConfig(base.Nomad)
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
	if input.RemoteCommandTimeoutSeconds == 0 {
		input.RemoteCommandTimeoutSeconds = current.RemoteCommandTimeoutSeconds
	}
	nomadSettings := current.Nomad
	if input.Nomad != nil {
		nomadSettings = *input.Nomad
	}
	certSettings := current.Certificates
	if input.Certificates != nil {
		certSettings = *input.Certificates
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
		RemoteCommandTimeoutSeconds:      input.RemoteCommandTimeoutSeconds,
		Nomad:                            nomadSettings,
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
	s.rt.RemoteCommandTimeoutSeconds = next.RemoteCommandTimeoutSeconds
	s.rt.Nomad = next.Nomad
	s.rt.Certificates = next.Certificates
	out := s.rt
	s.mu.Unlock()
	i18n.SetDefaultLocale(out.Language)
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
		case RuntimeSettingJWTSecret:
			next.JWTSecret = value
			next.JWTSecretConfigured = strings.TrimSpace(value) != ""
		case RuntimeSettingRemoteCommandTimeoutSeconds:
			if n, err := strconv.Atoi(value); err == nil {
				next.RemoteCommandTimeoutSeconds = n
			}
		case RuntimeSettingNomadNamespace:
			next.Nomad.Namespace = value
		case RuntimeSettingNomadRegion:
			next.Nomad.Region = value
		case RuntimeSettingNomadDatacenter:
			next.Nomad.Datacenter = value
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
		RemoteCommandTimeoutSeconds:      remoteTimeout,
		Nomad: RuntimeNomadSettings{
			Namespace:  firstNonEmpty(strings.TrimSpace(cfg.Nomad.Namespace), "default"),
			Region:     firstNonEmpty(strings.TrimSpace(cfg.Nomad.Region), "global"),
			Datacenter: firstNonEmpty(strings.TrimSpace(cfg.Nomad.Datacenter), "dc1"),
		},
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
		RuntimeSettingRemoteCommandTimeoutSeconds:          strconv.Itoa(settings.RemoteCommandTimeoutSeconds),
		RuntimeSettingNomadNamespace:                       settings.Nomad.Namespace,
		RuntimeSettingNomadRegion:                          settings.Nomad.Region,
		RuntimeSettingNomadDatacenter:                      settings.Nomad.Datacenter,
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
	if settings.RemoteCommandTimeoutSeconds < 1 {
		return panelerr.Validation("invalid_remote_command_timeout", "Remote command timeout must be at least 1 second")
	}
	if strings.TrimSpace(settings.Nomad.Namespace) == "" {
		return panelerr.Validation("invalid_nomad_namespace", "Nomad namespace is required")
	}
	if strings.TrimSpace(settings.Nomad.Region) == "" {
		return panelerr.Validation("invalid_nomad_region", "Nomad region is required")
	}
	if strings.TrimSpace(settings.Nomad.Datacenter) == "" {
		return panelerr.Validation("invalid_nomad_datacenter", "Nomad datacenter is required")
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
