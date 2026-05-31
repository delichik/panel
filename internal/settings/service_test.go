package settings

import (
	"context"
	"path/filepath"
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
		Language:                         "zh-CN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MetricsRetentionDays != 30 || got.MetricsCollectionIntervalSeconds != 120 || got.CleanupSchedule != "weekly" || got.Language != "zh-CN" {
		t.Fatalf("unexpected runtime settings: %#v", got)
	}
	if got := svc.Runtime(); got.MetricsRetentionDays != 30 {
		t.Fatalf("runtime cache did not update: %#v", got)
	}
}

func TestRuntimeSettingsRejectInvalidSchedule(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		CleanupSchedule:                  "sometimes",
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
		Language:                         "fr",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
