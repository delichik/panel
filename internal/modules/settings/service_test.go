package settings

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRuntimeSettingsUpdatePersists(t *testing.T) {
	svc := newTestService(t)
	got, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             30,
		MetricsCollectionIntervalSeconds: 120,
		ContainerReportIntervalSeconds:   3,
		CleanupSchedule:                  "weekly",
		TokenExpiration:                  TokenExpiration5Days,
		Language:                         "zh-CN",
		LogLevel:                         "debug",
		RemoteCommandTimeoutSeconds:      45,
		Branding:                         &RuntimeBrandingSettings{LoginTitle: "Operations", LoginSubtitle: "Manage infrastructure"},
		Certificates:                     &RuntimeCertificateSettings{Email: "admin@example.com", DNSPropagationDelaySeconds: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MetricsRetentionDays != 30 || got.MetricsCollectionIntervalSeconds != 120 || got.ContainerReportIntervalSeconds != 3 || got.CleanupSchedule != "weekly" || got.TokenExpiration != TokenExpiration5Days || got.Language != "zh-CN" || got.LogLevel != "debug" || got.RemoteCommandTimeoutSeconds != 45 || got.Branding.LoginTitle != "Operations" || got.Branding.LoginSubtitle != "Manage infrastructure" || got.Certificates.Email != "admin@example.com" || got.Certificates.DNSPropagationDelaySeconds != 10 {
		t.Fatalf("unexpected runtime settings: %#v", got)
	}
	if got := svc.Runtime(); got.MetricsRetentionDays != 30 {
		t.Fatalf("runtime cache did not update: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runtime().Branding != got.Branding {
		t.Fatalf("branding settings were not persisted: %#v", reloaded.Runtime().Branding)
	}
	if reloaded.Runtime().LogLevel != "debug" {
		t.Fatalf("log level was not persisted: %q", reloaded.Runtime().LogLevel)
	}
}

func TestRuntimeSettingsSetJWTSecretPersists(t *testing.T) {
	svc := newTestService(t)
	got, err := svc.SetJWTSecret(context.Background(), "a-new-long-jwt-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.JWTSecret != "a-new-long-jwt-secret" || !got.JWTSecretConfigured {
		t.Fatalf("unexpected jwt state: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.JWTSecret() != "a-new-long-jwt-secret" {
		t.Fatalf("jwt secret was not persisted: %q", reloaded.JWTSecret())
	}
}

func TestRuntimeSettingsFirstStartRandomizesDefaultJWTSecret(t *testing.T) {
	svc := newTestService(t)
	if svc.JWTSecret() == DefaultJWTSecret {
		t.Fatal("default jwt secret should be randomized on first startup")
	}
	if !svc.Runtime().JWTSecretConfigured {
		t.Fatal("randomized jwt secret should be marked as configured")
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.JWTSecret() != svc.JWTSecret() {
		t.Fatalf("randomized jwt secret should persist across reloads: %q != %q", reloaded.JWTSecret(), svc.JWTSecret())
	}
}

func TestRuntimeSettingsRotatesLegacyDefaultJWTSecret(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧版本安装：默认密钥已被旧代码固化进 runtime_settings。
	if _, err := store.AppDB().Exec(`INSERT INTO runtime_settings(key, value, updated_at) VALUES('jwtSecret', ?, 'now')`, DefaultJWTSecret); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if svc.JWTSecret() == DefaultJWTSecret {
		t.Fatal("legacy default jwt secret should be rotated on startup")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// 再次启动不应再次轮换（幂等），且与首次轮换值一致。
	store2, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	svc2, err := NewService(store2.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if svc2.JWTSecret() != svc.JWTSecret() {
		t.Fatalf("rotated jwt secret should be stable across restarts: %q != %q", svc2.JWTSecret(), svc.JWTSecret())
	}
}

func TestRuntimeSettingsPreservesExplicitConfigJWTSecret(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.JWTSecret = "explicit-configured-secret-value-123"
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.JWTSecret(); got != "explicit-configured-secret-value-123" {
		t.Fatalf("explicit jwt secret not preserved: %q", got)
	}
}
func TestRuntimeSettingsRejectInvalidSchedule(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "sometimes",
		TokenExpiration:                  DefaultTokenExpiration,
		Language:                         "en",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectInvalidLanguage(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  DefaultTokenExpiration,
		Language:                         "fr",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectInvalidLogLevel(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  DefaultTokenExpiration,
		Language:                         "en",
		LogLevel:                         "verbose",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectInvalidTokenExpiration(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  "2h",
		Language:                         "en",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectLongBranding(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  DefaultTokenExpiration,
		Language:                         "en",
		Branding:                         &RuntimeBrandingSettings{LoginTitle: strings.Repeat("a", 81)},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
