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

func TestQueryManyReturnsBucketedSeriesPerServer(t *testing.T) {
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
	base := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Second)
	bucketStart := time.Unix(base.Unix()/10*10, 0).UTC()
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv_a", Time: bucketStart, CPUUsagePercent: 10, MemoryUsedBytes: 100, MemoryTotalBytes: 200, NetworkRxBytesRate: 1, NetworkTxBytesRate: 2, Status: linux.SystemStatus{Load1: 0.5}},
		{ServerID: "srv_a", Time: bucketStart.Add(4 * time.Second), CPUUsagePercent: 20, MemoryUsedBytes: 300, MemoryTotalBytes: 400, NetworkRxBytesRate: 3, NetworkTxBytesRate: 4},
		{ServerID: "srv_a", Time: bucketStart.Add(10 * time.Second), CPUUsagePercent: 30, MemoryUsedBytes: 500, MemoryTotalBytes: 600},
		{ServerID: "srv_b", Time: bucketStart, CPUUsagePercent: 40},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	fields := SeriesFields{CPU: true, Memory: true}
	byServer, err := svc.QueryMany(ctx, []string{"srv_a", "srv_b", "srv_missing"}, "1h", QueryManyOptions{BucketSeconds: 10, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	if len(byServer) != 3 {
		t.Fatalf("server count = %d, want 3 including empty entries", len(byServer))
	}
	a := byServer["srv_a"]
	if len(a.CPU) != 2 {
		t.Fatalf("srv_a CPU buckets = %d, want 2: %#v", len(a.CPU), a.CPU)
	}
	if a.CPU[0].UsagePercent != 15 || !a.CPU[0].Time.Equal(bucketStart) || a.CPU[1].UsagePercent != 30 {
		t.Fatalf("unexpected srv_a CPU buckets: %#v", a.CPU)
	}
	if len(a.Memory) != 2 || a.Memory[0].UsedBytes != 200 || a.Memory[0].TotalBytes != 300 || a.Memory[1].UsedBytes != 500 || a.Memory[1].TotalBytes != 600 {
		t.Fatalf("unexpected srv_a memory buckets: %#v", a.Memory)
	}
	if len(a.Disk) != 0 || len(a.Network) != 0 || len(a.Load) != 0 {
		t.Fatalf("unprojected series must stay empty: %#v", a)
	}
	if missing := byServer["srv_missing"]; len(missing.CPU) != 0 || len(missing.Memory) != 0 {
		t.Fatalf("missing server must keep empty series: %#v", missing)
	}

	// since 落在桶中间时先向下对齐，正在累积的桶必须被完整重算。
	midBucket := bucketStart.Add(4 * time.Second)
	delta, err := svc.QueryMany(ctx, []string{"srv_a"}, "1h", QueryManyOptions{After: &midBucket, BucketSeconds: 10, Fields: SeriesFields{CPU: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta["srv_a"].CPU) != 2 || delta["srv_a"].CPU[0].UsagePercent != 15 {
		t.Fatalf("delta from mid-bucket = %#v, want both buckets with the first recomputed", delta["srv_a"].CPU)
	}
	nextBucket := bucketStart.Add(10 * time.Second)
	delta, err = svc.QueryMany(ctx, []string{"srv_a"}, "1h", QueryManyOptions{After: &nextBucket, BucketSeconds: 10, Fields: SeriesFields{CPU: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta["srv_a"].CPU) != 1 || delta["srv_a"].CPU[0].UsagePercent != 30 {
		t.Fatalf("delta from bucket start = %#v, want only the second bucket", delta["srv_a"].CPU)
	}

	if _, err := svc.QueryMany(ctx, []string{"srv_a"}, "1h", QueryManyOptions{BucketSeconds: 0, Fields: fields}); err == nil {
		t.Fatal("expected invalid bucket seconds error")
	}
	if _, err := svc.QueryMany(ctx, []string{"srv_a"}, "bogus", QueryManyOptions{BucketSeconds: 10, Fields: fields}); err == nil {
		t.Fatal("expected invalid range error")
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
