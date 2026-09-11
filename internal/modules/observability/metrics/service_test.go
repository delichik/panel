package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
)

func TestMetricsSaveQueryCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Minute)
	sampledAt := time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), base.Minute(), base.Second(), 345678901, time.UTC)
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt, CPUUsagePercent: 50, MemoryUsedBytes: 1, MemoryTotalBytes: 2, DiskUsedBytes: 3, DiskTotalBytes: 4, Status: linux.SystemStatus{Load1: 0.1, Load5: 0.2, Load15: 0.3}}); err != nil {
		t.Fatal(err)
	}
	series, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 50 {
		t.Fatalf("unexpected series: %#v", series)
	}
	if len(series.Load) != 1 || series.Load[0].Load1 != 0.1 || series.Load[0].Load5 != 0.2 || series.Load[0].Load15 != 0.3 {
		t.Fatalf("unexpected load series: %#v", series.Load)
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

func TestQueryAfterReturnsOnlyNewerPoints(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	older := base.Add(-2 * time.Minute)
	newer := base.Add(-30 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv", Time: older, CPUUsagePercent: 10},
		{ServerID: "srv", Time: newer, CPUUsagePercent: 20},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}

	all, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.CPU) != 2 {
		t.Fatalf("full query length = %d, want 2", len(all.CPU))
	}

	after, err := svc.QueryAfter(ctx, "srv", "1h", older)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.CPU) != 1 || after.CPU[0].UsagePercent != 20 {
		t.Fatalf("delta query = %#v, want only the newer point", after.CPU)
	}

	empty, err := svc.QueryAfter(ctx, "srv", "1h", newer)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.CPU) != 0 {
		t.Fatalf("delta after newest point = %#v, want empty", empty.CPU)
	}

	if _, err := svc.QueryAfter(ctx, "srv", "bogus", older); err == nil {
		t.Fatal("expected invalid range error")
	}
}
func TestMetricsCleanupRejectsInvalidRetention(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cleanup(ctx, 0); err == nil {
		t.Fatal("expected invalid retention to be rejected")
	}
	latest, err := svc.LatestAtMany(ctx, []string{"srv"})
	if err != nil {
		t.Fatal(err)
	}
	if latest["srv"] == nil {
		t.Fatal("invalid retention must not clear the metrics table")
	}
}

func TestQueryManyReturnsSeriesPerServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	older := base.Add(-2 * time.Minute)
	newer := base.Add(-30 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv_a", Time: older, CPUUsagePercent: 10},
		{ServerID: "srv_a", Time: newer, CPUUsagePercent: 20},
		{ServerID: "srv_b", Time: older, CPUUsagePercent: 30},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	byServer, err := svc.QueryMany(ctx, []string{"srv_a", "srv_b"}, "1h", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(byServer["srv_a"].CPU) != 2 || len(byServer["srv_b"].CPU) != 1 {
		t.Fatalf("unexpected batch series: %#v", byServer)
	}
	if byServer["srv_b"].CPU[0].UsagePercent != 30 {
		t.Fatalf("unexpected srv_b point: %#v", byServer["srv_b"].CPU[0])
	}
	after, err := svc.QueryMany(ctx, []string{"srv_a", "srv_b"}, "1h", &older)
	if err != nil {
		t.Fatal(err)
	}
	if len(after["srv_a"].CPU) != 1 || after["srv_a"].CPU[0].UsagePercent != 20 || len(after["srv_b"].CPU) != 0 {
		t.Fatalf("unexpected after-series: %#v", after)
	}
}
func TestLatestBatchQueries(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	t1 := base.Add(-2 * time.Minute)
	t2 := base.Add(-30 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv_a", Time: t1, CPUUsagePercent: 10, Status: linux.SystemStatus{LoadAverage: "1.00"}},
		{ServerID: "srv_a", Time: t2, CPUUsagePercent: 20, Status: linux.SystemStatus{LoadAverage: "2.00"}},
		{ServerID: "srv_b", Time: t1, CPUUsagePercent: 30, Status: linux.SystemStatus{LoadAverage: "3.00"}},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	latestAt, err := svc.LatestAtMany(ctx, []string{"srv_a", "srv_b"})
	if err != nil {
		t.Fatal(err)
	}
	if latestAt["srv_a"] == nil || !latestAt["srv_a"].Equal(t2) || latestAt["srv_b"] == nil || !latestAt["srv_b"].Equal(t1) {
		t.Fatalf("unexpected latest times: %#v", latestAt)
	}
	loads, err := svc.LatestLoadMany(ctx, []string{"srv_a", "srv_b"})
	if err != nil {
		t.Fatal(err)
	}
	if loads["srv_a"] != "2.00" || loads["srv_b"] != "3.00" {
		t.Fatalf("unexpected latest loads: %#v", loads)
	}
}
