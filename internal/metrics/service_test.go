package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/linux"
	"panel/internal/storage"
)

func TestMetricsSaveQueryCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil, nil)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Minute)
	sampledAt := time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), base.Minute(), base.Second(), 345678901, time.UTC)
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt, CPUUsagePercent: 50, MemoryUsedBytes: 1, MemoryTotalBytes: 2, DiskUsedBytes: 3, DiskTotalBytes: 4}); err != nil {
		t.Fatal(err)
	}
	series, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 50 {
		t.Fatalf("unexpected series: %#v", series)
	}
	if want := sampledAt.UTC().Truncate(time.Second); !series.CPU[0].Time.Equal(want) {
		t.Fatalf("expected timestamp aligned to %s, got %s", want, series.CPU[0].Time)
	}
	if _, err := svc.Query(ctx, "srv", "7d"); err != nil {
		t.Fatalf("expected 7d range to be accepted: %v", err)
	}
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: time.Now().UTC().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	deleted, err := svc.Cleanup(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected one expired row removed, got %d", deleted)
	}
}
