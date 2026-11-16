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
	if len(a.DiskIO) != 0 || len(a.Network) != 0 || len(a.Load) != 0 {
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

func TestMetricsSaveDeduplicatesReplayedSamples(t *testing.T) {
	svc, closeStore := newMetricsTestService(t)
	defer closeStore()
	ctx := context.Background()
	sampledAt := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt, CPUUsagePercent: 11}); err != nil {
		t.Fatal(err)
	}
	// 补报重放：同一秒的样本（对齐后 time 相同）第二次写入必须被跳过，且保留首次值。
	replay := linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt.Add(456 * time.Millisecond), CPUUsagePercent: 99}
	if err := svc.Save(ctx, replay); err != nil {
		t.Fatal(err)
	}
	series, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 11 {
		t.Fatalf("replayed (server_id,time) must be de-duplicated, got %#v", series.CPU)
	}

	// 不同秒的样本仍然正常写入。
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt.Add(time.Second), CPUUsagePercent: 12}); err != nil {
		t.Fatal(err)
	}
	series, err = svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 2 {
		t.Fatalf("distinct sample seconds must both be stored, got %d points", len(series.CPU))
	}
}

func TestQueryProjectsDiskIOAndCurrentDiskOnly(t *testing.T) {
	svc, closeStore := newMetricsTestService(t)
	defer closeStore()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(-4 * time.Minute)
	sampledAt := base.Add(time.Minute)
	latestValidAt := base.Add(2 * time.Minute)
	for _, snap := range []linux.MetricsSnapshot{
		// 未采样磁盘使用率的样本：disk_used/disk_total 为 0，且没有磁盘 IO 值。
		{ServerID: "srv", Time: base},
		{ServerID: "srv", Time: sampledAt, DiskUsedBytes: 40, DiskTotalBytes: 100, DiskIOAvailable: true, DiskReadBytesRate: 10, DiskWriteBytesRate: 20},
		{ServerID: "srv", Time: latestValidAt, DiskUsedBytes: 50, DiskTotalBytes: 120, DiskIOAvailable: true, DiskReadBytesRate: 30, DiskWriteBytesRate: 40},
		// 最近一行未重新采样磁盘：diskCurrent 必须回退到范围内最近的有效行。
		{ServerID: "srv", Time: base.Add(3 * time.Minute)},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}

	series, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.DiskIO) != 2 {
		t.Fatalf("diskIo points = %d, want 2 (NULL rows skipped): %#v", len(series.DiskIO), series.DiskIO)
	}
	if !series.DiskIO[0].Time.Equal(sampledAt) || series.DiskIO[0].ReadBytesPerSecond != 10 || series.DiskIO[0].WriteBytesPerSecond != 20 {
		t.Fatalf("unexpected first diskIo point: %#v", series.DiskIO[0])
	}
	if !series.DiskIO[1].Time.Equal(latestValidAt) || series.DiskIO[1].ReadBytesPerSecond != 30 || series.DiskIO[1].WriteBytesPerSecond != 40 {
		t.Fatalf("unexpected second diskIo point: %#v", series.DiskIO[1])
	}
	if series.DiskCurrent == nil {
		t.Fatal("diskCurrent = nil, want the latest valid disk sample")
	}
	if !series.DiskCurrent.Time.Equal(latestValidAt) || series.DiskCurrent.UsedBytes != 50 || series.DiskCurrent.TotalBytes != 120 {
		t.Fatalf("unexpected diskCurrent: %#v", series.DiskCurrent)
	}

	// 没有磁盘 IO 值和有效磁盘采样的服务器：diskIo 为空，diskCurrent 为 nil。
	empty, err := svc.Query(ctx, "srv_missing", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.DiskIO) != 0 || empty.DiskCurrent != nil {
		t.Fatalf("missing server must not report disk data: %#v", empty)
	}
}

func TestQueryManyProjectsDiskIOAndCurrentDisk(t *testing.T) {
	svc, closeStore := newMetricsTestService(t)
	defer closeStore()
	ctx := context.Background()
	base := time.Unix(time.Now().Unix()/10*10, 0).UTC().Add(-3 * time.Minute)
	t1 := base
	t2 := base.Add(10 * time.Second)
	t3 := base.Add(20 * time.Second)
	t4 := base.Add(30 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv_a", Time: t1, CPUUsagePercent: 1},
		{ServerID: "srv_a", Time: t2, CPUUsagePercent: 2, DiskUsedBytes: 40, DiskTotalBytes: 100, DiskIOAvailable: true, DiskReadBytesRate: 10, DiskWriteBytesRate: 20},
		{ServerID: "srv_a", Time: t3, CPUUsagePercent: 3, DiskUsedBytes: 50, DiskTotalBytes: 120, DiskIOAvailable: true, DiskReadBytesRate: 30, DiskWriteBytesRate: 40},
		{ServerID: "srv_a", Time: t4, CPUUsagePercent: 4},
		{ServerID: "srv_b", Time: t3, CPUUsagePercent: 5},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}
	fields := SeriesFields{DiskIO: true, DiskCurrent: true}
	byServer, err := svc.QueryMany(ctx, []string{"srv_a", "srv_b", "srv_missing"}, "1h", QueryManyOptions{BucketSeconds: 10, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	a := byServer["srv_a"]
	// t1（两个可空列都 NULL）不产生 diskIo 点，t2/t3 各一个。
	if len(a.DiskIO) != 2 || !a.DiskIO[0].Time.Equal(t2) || a.DiskIO[0].ReadBytesPerSecond != 10 || a.DiskIO[0].WriteBytesPerSecond != 20 || !a.DiskIO[1].Time.Equal(t3) {
		t.Fatalf("unexpected srv_a diskIo buckets: %#v", a.DiskIO)
	}
	if a.DiskCurrent == nil || !a.DiskCurrent.Time.Equal(t3) || a.DiskCurrent.UsedBytes != 50 || a.DiskCurrent.TotalBytes != 120 {
		t.Fatalf("unexpected srv_a diskCurrent: %#v", a.DiskCurrent)
	}
	if b := byServer["srv_b"]; len(b.DiskIO) != 0 || b.DiskCurrent != nil || len(b.CPU) != 0 {
		t.Fatalf("srv_b must keep empty projected series: %#v", b)
	}
	if missing := byServer["srv_missing"]; len(missing.DiskIO) != 0 || missing.DiskCurrent != nil {
		t.Fatalf("missing server must keep empty projected series: %#v", missing)
	}

	// 增量查询的 After 晚于最后有效磁盘行：diskIo 为空，但 diskCurrent 仍返回当前值。
	after := t4
	delta, err := svc.QueryMany(ctx, []string{"srv_a"}, "1h", QueryManyOptions{After: &after, BucketSeconds: 10, Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	deltaA := delta["srv_a"]
	if len(deltaA.DiskIO) != 0 {
		t.Fatalf("delta diskIo = %#v, want empty after last valid disk row", deltaA.DiskIO)
	}
	if deltaA.DiskCurrent == nil || deltaA.DiskCurrent.TotalBytes != 120 {
		t.Fatalf("diskCurrent must ignore After and stay current: %#v", deltaA.DiskCurrent)
	}

	// 只请求 DiskCurrent 的磁盘卡：不产生其他指标序列，但仍返回当前值。
	onlyCurrent, err := svc.QueryMany(ctx, []string{"srv_a"}, "1h", QueryManyOptions{BucketSeconds: 10, Fields: SeriesFields{DiskCurrent: true}})
	if err != nil {
		t.Fatal(err)
	}
	onlyA := onlyCurrent["srv_a"]
	if len(onlyA.CPU) != 0 || len(onlyA.Memory) != 0 || len(onlyA.Network) != 0 || len(onlyA.Load) != 0 || len(onlyA.DiskIO) != 0 {
		t.Fatalf("disk card must only materialize diskCurrent: %#v", onlyA)
	}
	if onlyA.DiskCurrent == nil || onlyA.DiskCurrent.TotalBytes != 120 {
		t.Fatalf("disk card diskCurrent = %#v, want current disk sample", onlyA.DiskCurrent)
	}
}

func newMetricsTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return NewService(store.MetricsDB(), nil), func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}
}
