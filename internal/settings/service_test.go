package settings

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/config"
	"panel/internal/storage"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
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
		CleanupSchedule:                  "weekly",
		TokenExpiration:                  TokenExpiration5Days,
		Language:                         "zh-CN",
		RemoteCommandTimeoutSeconds:      45,
		Branding:                         &RuntimeBrandingSettings{LoginTitle: "Operations", LoginSubtitle: "Manage infrastructure"},
		Certificates:                     &RuntimeCertificateSettings{Email: "admin@example.com", DNSPropagationDelaySeconds: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MetricsRetentionDays != 30 || got.MetricsCollectionIntervalSeconds != 120 || got.CleanupSchedule != "weekly" || got.TokenExpiration != TokenExpiration5Days || got.Language != "zh-CN" || got.RemoteCommandTimeoutSeconds != 45 || got.Branding.LoginTitle != "Operations" || got.Branding.LoginSubtitle != "Manage infrastructure" || got.Certificates.Email != "admin@example.com" || got.Certificates.DNSPropagationDelaySeconds != 10 {
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
