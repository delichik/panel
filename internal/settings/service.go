package settings

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"

	"panel/internal/config"
	"panel/internal/i18n"
	"panel/internal/panelerr"
)

type RuntimeUpdate struct {
	MetricsRetentionDays             int    `json:"metricsRetentionDays"`
	MetricsCollectionIntervalSeconds int    `json:"metricsCollectionIntervalSeconds"`
	CleanupSchedule                  string `json:"cleanupSchedule"`
	TokenExpiration                  string `json:"tokenExpiration"`
	Language                         string `json:"language"`
}

type RuntimeSettings struct {
	ListenAddress                    string `json:"listenAddress"`
	AppDatabase                      string `json:"appDatabase"`
	MetricsDatabase                  string `json:"metricsDatabase"`
	DataRoot                         string `json:"dataRoot"`
	MetricsRetentionDays             int    `json:"metricsRetentionDays"`
	MetricsCollectionIntervalSeconds int    `json:"metricsCollectionIntervalSeconds"`
	CleanupSchedule                  string `json:"cleanupSchedule"`
	TokenExpiration                  string `json:"tokenExpiration"`
	Language                         string `json:"language"`
}

const (
	RuntimeSettingTokenExpiration = "tokenExpiration"

	TokenExpiration10Minutes = "10m"
	TokenExpiration1Hour     = "1h"
	TokenExpiration1Day      = "1d"
	TokenExpiration5Days     = "5d"
	TokenExpiration30Days    = "30d"
	TokenExpirationNever     = "never"

	DefaultTokenExpiration = TokenExpiration1Day
)

type Service struct {
	db  *sql.DB
	cfg config.Config
	mu  sync.RWMutex
	rt  RuntimeSettings
}

func NewService(db *sql.DB, cfg config.Config) (*Service, error) {
	s := &Service{db: db, cfg: cfg, rt: defaultRuntimeSettings(cfg)}
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

func (s *Service) Update(ctx context.Context, input RuntimeUpdate) (RuntimeSettings, error) {
	if input.TokenExpiration == "" {
		input.TokenExpiration = s.Runtime().TokenExpiration
	}
	if err := validateRuntimeUpdate(input); err != nil {
		return RuntimeSettings{}, err
	}
	tokenExpiration := NormalizeTokenExpiration(input.TokenExpiration)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuntimeSettings{}, err
	}
	values := map[string]string{
		"metricsRetentionDays":             strconv.Itoa(input.MetricsRetentionDays),
		"metricsCollectionIntervalSeconds": strconv.Itoa(input.MetricsCollectionIntervalSeconds),
		"cleanupSchedule":                  input.CleanupSchedule,
		RuntimeSettingTokenExpiration:      tokenExpiration,
		"language":                         i18n.NormalizeLocale(input.Language),
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO runtime_settings(key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, key, value, now); err != nil {
			_ = tx.Rollback()
			return RuntimeSettings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RuntimeSettings{}, err
	}
	s.mu.Lock()
	s.rt.MetricsRetentionDays = input.MetricsRetentionDays
	s.rt.MetricsCollectionIntervalSeconds = input.MetricsCollectionIntervalSeconds
	s.rt.CleanupSchedule = input.CleanupSchedule
	s.rt.TokenExpiration = tokenExpiration
	s.rt.Language = i18n.NormalizeLocale(input.Language)
	next := s.rt
	s.mu.Unlock()
	i18n.SetDefaultLocale(next.Language)
	return next, nil
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
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := validateRuntimeUpdate(RuntimeUpdate{
		MetricsRetentionDays:             next.MetricsRetentionDays,
		MetricsCollectionIntervalSeconds: next.MetricsCollectionIntervalSeconds,
		CleanupSchedule:                  next.CleanupSchedule,
		TokenExpiration:                  next.TokenExpiration,
		Language:                         next.Language,
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.rt = next
	s.mu.Unlock()
	i18n.SetDefaultLocale(next.Language)
	return nil
}

func defaultRuntimeSettings(cfg config.Config) RuntimeSettings {
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
	}
}

func validateRuntimeUpdate(input RuntimeUpdate) error {
	if input.MetricsRetentionDays < 1 {
		return panelerr.Validation("invalid_metrics_retention", "Metrics retention must be at least 1 day")
	}
	if input.MetricsCollectionIntervalSeconds < 10 {
		return panelerr.Validation("invalid_metrics_interval", "Metrics collection interval must be at least 10 seconds")
	}
	switch input.CleanupSchedule {
	case "hourly", "daily", "weekly":
	default:
		return panelerr.Validation("invalid_cleanup_schedule", "Cleanup schedule must be hourly, daily, or weekly")
	}
	if tokenExpiration := NormalizeTokenExpiration(input.TokenExpiration); tokenExpiration == "" {
		return panelerr.Validation("invalid_token_expiration", "Token expiration must be 10 minutes, 1 hour, 1 day, 5 days, 30 days, or never")
	}
	if locale := i18n.NormalizeLocale(input.Language); locale == "" {
		return panelerr.Validation("invalid_language", "Language must be English or Simplified Chinese")
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
