package rpc

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	agentpb "panel/internal/agent/pb"
	agentprobe "panel/internal/agent/probe"
	agentsystem "panel/internal/agent/system"
	"panel/internal/platform/linux"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNextAlignedUsesUnixIntervalBoundary(t *testing.T) {
	got := nextAligned(time.Unix(10, 800_000_000), 3*time.Second)
	want := time.Unix(12, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("nextAligned = %s, want %s", got, want)
	}
}

func TestNextAlignedKeepsExactBoundary(t *testing.T) {
	got := nextAligned(time.Unix(12, 0), 3*time.Second)
	want := time.Unix(12, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("nextAligned = %s, want %s", got, want)
	}
}

func TestReportHubRunsOnlyWhileWatched(t *testing.T) {
	hub := newReportHub(agentsystem.LocalCollector{}, nil)
	if hub.isRunning() {
		t.Fatal("hub should not collect without watchers")
	}
	watcher := hub.add(reportConfig{})
	if !hub.isRunning() {
		t.Fatal("hub should start when the first watcher is added")
	}
	hub.remove(watcher.id)
	deadline := time.Now().Add(time.Second)
	for hub.isRunning() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.isRunning() {
		t.Fatal("hub should stop after the last watcher is removed")
	}
}

func TestNextReportDueUsesSharedEarliestBoundary(t *testing.T) {
	watchers := []reportWatcherSnapshot{
		{cfg: reportConfig{serverID: "s1", metricsInterval: 5 * time.Second}},
		{cfg: reportConfig{serverID: "s1", containerInterval: 3 * time.Second}},
	}
	got, ok := nextReportDue(time.Unix(10, 500_000_000), watchers)
	if !ok {
		t.Fatal("nextReportDue should find a due time")
	}
	want := time.Unix(12, 0).UTC()
	if !got.Equal(want) {
		t.Fatalf("nextReportDue = %s, want %s", got, want)
	}
}

func TestReportIntervalDueUsesUnixBoundary(t *testing.T) {
	if !reportIntervalDue(time.Unix(12, 0), 3*time.Second) {
		t.Fatal("12 should be due for a 3 second interval")
	}
	if reportIntervalDue(time.Unix(13, 0), 3*time.Second) {
		t.Fatal("13 should not be due for a 3 second interval")
	}
}

func TestMetricCacheSnapshotRejectsStaleMetrics(t *testing.T) {
	c := &metricCache{}
	base := time.Now().UTC().Add(-2 * time.Second)
	c.setCPU(12)
	c.setMemory(2048, 1024)
	c.setDisk(8192, 4096)
	c.accumulateNetworkAt(100, 50, base)
	c.accumulateNetworkAt(150, 60, base.Add(time.Second))
	c.setStatus(linux.SystemStatus{})
	if _, ok := c.snapshot(); !ok {
		t.Fatal("expected a fresh cache to snapshot ok")
	}
	// 某个指标持续采样失败超过新鲜度窗口后，快照必须被拒绝，
	// 否则冻结的旧值会随整点时间前进伪装成新样本。
	now := time.Now().UTC()
	c.mu.Lock()
	c.cpuUpdatedAt = now.Add(-metricFreshnessWindow - time.Second)
	c.mu.Unlock()
	if _, ok := c.snapshotAt(now); ok {
		t.Fatal("expected a cache with a stale metric to be rejected")
	}
	c.setCPU(13)
	c.accumulateNetworkAt(200, 80, base.Add(2*time.Second))
	if _, ok := c.snapshot(); !ok {
		t.Fatal("expected the cache to recover after a fresh sample")
	}
}

type okReportCollector struct{}

func (okReportCollector) CPUUsage(context.Context) (float64, error) { return 12, nil }
func (okReportCollector) MemoryStats(context.Context) (int64, int64, error) {
	return 2048, 1024, nil
}
func (okReportCollector) DiskUsage(context.Context) (int64, int64, error) {
	return 8192, 4096, nil
}
func (okReportCollector) DiskIOTotals(context.Context) (int64, int64, error) {
	return 5000, 2500, nil
}
func (okReportCollector) NetworkTotals(context.Context) (int64, int64, error) {
	return 100, 200, nil
}
func (okReportCollector) SystemStatus(context.Context) (linux.SystemStatus, error) {
	return linux.SystemStatus{LoadAverage: "1.00 0.50 0.25", Load1: 1, Load5: 0.5, Load15: 0.25}, nil
}
func (okReportCollector) PackageUpdates(context.Context) ([]linux.PackageUpdate, error) {
	return nil, nil
}

func TestReportHubKeepsSchedulingWhenWatchersChurn(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	w := hub.add(reportConfig{serverID: "s1", metricsInterval: 1 * time.Second})
	defer hub.remove(w.id)
	for i := 0; i < 50; i++ {
		other := hub.add(reportConfig{serverID: "s2", containerInterval: 1 * time.Second})
		hub.remove(other.id)
	}
	select {
	case <-w.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher starved; hub did not keep scheduling reports")
	}
}

type failingReportCollector struct{}

func (failingReportCollector) CPUUsage(context.Context) (float64, error) {
	return 0, errors.New("cpu unavailable")
}
func (failingReportCollector) MemoryStats(context.Context) (int64, int64, error) {
	return 0, 0, errors.New("memory unavailable")
}
func (failingReportCollector) DiskUsage(context.Context) (int64, int64, error) {
	return 0, 0, errors.New("disk unavailable")
}
func (failingReportCollector) DiskIOTotals(context.Context) (int64, int64, error) {
	return 0, 0, errors.New("disk io unavailable")
}
func (failingReportCollector) NetworkTotals(context.Context) (int64, int64, error) {
	return 0, 0, errors.New("network unavailable")
}
func (failingReportCollector) SystemStatus(context.Context) (linux.SystemStatus, error) {
	return linux.SystemStatus{}, errors.New("status unavailable")
}
func (failingReportCollector) PackageUpdates(context.Context) ([]linux.PackageUpdate, error) {
	return nil, errors.New("packages unavailable")
}

func TestCollectAndBroadcastKeepsFailedCollectionsNil(t *testing.T) {
	hub := newReportHub(failingReportCollector{}, nil)
	watcher := hub.add(reportConfig{serverID: "s1", metricsInterval: time.Second, containerInterval: time.Second})
	defer hub.remove(watcher.id)

	hub.collectAndBroadcast(time.Unix(10, 0).UTC(), false, "")
	select {
	case msg := <-watcher.ch:
		t.Fatalf("expected no report when both collections fail, got %#v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

// seedNetworkCache 用两次过去时刻的读数把网络累加器填到可消费状态：
// 1 秒读数窗口内累计 1000/500 字节。
func seedNetworkCache(c *metricCache) {
	base := time.Now().UTC().Add(-2 * time.Second)
	c.accumulateNetworkAt(1000, 500, base)
	c.accumulateNetworkAt(2000, 1000, base.Add(time.Second))
}

func TestMetricCacheRequiresAllParts(t *testing.T) {
	c := &metricCache{}
	if _, ok := c.snapshot(); ok {
		t.Fatal("cache without values must not be ready")
	}
	c.setCPU(12)
	c.setMemory(2048, 1024)
	c.setDisk(8192, 4096)
	c.setStatus(linux.SystemStatus{LoadAverage: "1.00 0.50 0.25"})
	base := time.Now().UTC().Add(-2 * time.Second)
	c.accumulateNetworkAt(1000, 500, base)
	if _, ok := c.snapshot(); ok {
		t.Fatal("a single network reading has no window yet and must not be ready")
	}
	c.accumulateNetworkAt(2000, 1000, base.Add(time.Second))
	snap, ok := c.snapshot()
	if !ok {
		t.Fatal("cache with all parts must be ready")
	}
	if snap.CPUUsagePercent != 12 || snap.MemoryUsedBytes != 1024 || snap.NetworkRxBytesRate != 1000 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}

func TestMetricCacheAveragesNetworkOverWindow(t *testing.T) {
	c := &metricCache{}
	base := time.Unix(1000, 0).UTC()
	// AGT-RPT-004：速率等于两次消费之间累计字节差除以读数窗口时长，
	// 与采集间隔一致，而不是某个 1 秒瞬时样本。
	c.accumulateNetworkAt(1000, 0, base)
	c.accumulateNetworkAt(3000, 500, base.Add(time.Second))
	c.accumulateNetworkAt(8000, 2000, base.Add(4*time.Second))
	c.mu.Lock()
	rx, tx, ok := c.consumeNetworkLocked()
	c.mu.Unlock()
	// 4 秒窗口累计 rx 7000、tx 2000 字节。
	if !ok || rx != 1750 || tx != 500 {
		t.Fatalf("consume = (%v, %v, %v), want (1750, 500, true)", rx, tx, ok)
	}
	// 消费后窗口重置；没有新读数时窗口时长为 0，不产出指标。
	c.mu.Lock()
	_, _, ok = c.consumeNetworkLocked()
	c.mu.Unlock()
	if ok {
		t.Fatal("a second consume without new readings must not produce a rate")
	}
}

func TestMetricCacheSkipsNetworkCounterReset(t *testing.T) {
	c := &metricCache{}
	base := time.Unix(1000, 0).UTC()
	c.accumulateNetworkAt(8000, 2000, base)
	c.accumulateNetworkAt(9000, 2500, base.Add(time.Second))
	// 计数器回退（网卡重建/回绕）按 0 增量跳过，不产生负值或突刺；
	// 窗口起点不因回退重置。
	c.accumulateNetworkAt(500, 100, base.Add(2*time.Second))
	c.accumulateNetworkAt(2500, 1100, base.Add(3*time.Second))
	c.mu.Lock()
	rx, tx, ok := c.consumeNetworkLocked()
	c.mu.Unlock()
	// 3 秒窗口累计 rx 3000、tx 1500 字节（回退段记 0）。
	if !ok || rx != 1000 || tx != 500 {
		t.Fatalf("consume = (%v, %v, %v), want (1000, 500, true)", rx, tx, ok)
	}
}

func TestCollectAndBroadcastUsesCachedMetrics(t *testing.T) {
	hub := newReportHub(failingReportCollector{}, nil)
	hub.metrics.setCPU(42)
	hub.metrics.setMemory(2048, 1024)
	hub.metrics.setDisk(8192, 4096)
	seedNetworkCache(hub.metrics)
	hub.metrics.setStatus(linux.SystemStatus{LoadAverage: "1.00 0.50 0.25"})
	watcher := hub.add(reportConfig{serverID: "s1", metricsInterval: time.Second, containerInterval: 0})
	defer hub.remove(watcher.id)

	hub.collectAndBroadcast(time.Unix(10, 0).UTC(), false, "")
	select {
	case msg := <-watcher.ch:
		if msg.GetMetrics() == nil {
			t.Fatal("expected cached metrics in report")
		}
		if msg.GetMetrics().GetCpuUsagePercent() != 42 {
			t.Fatalf("unexpected cpu value: %v", msg.GetMetrics().GetCpuUsagePercent())
		}
	case <-time.After(time.Second):
		t.Fatal("expected a report with cached metrics")
	}
}

// seedFreshMetrics 用当前时刻附近的两条网络读数把缓存填充到可快照状态；
// 磁盘使用率与磁盘 IO 由调用方按用例补充。
func seedFreshMetrics(c *metricCache, now time.Time) {
	c.setCPU(12)
	c.setMemory(2048, 1024)
	c.setStatus(linux.SystemStatus{LoadAverage: "1.00 0.50 0.25"})
	c.accumulateNetworkAt(1000, 500, now.Add(-2*time.Second))
	c.accumulateNetworkAt(2000, 1000, now.Add(-time.Second))
}

func TestMetricCacheReusesDiskUsageOutsideFreshnessGate(t *testing.T) {
	c := &metricCache{}
	now := time.Now().UTC()
	c.setDisk(8192, 4096)
	seedFreshMetrics(c, now)
	// 磁盘使用率 5 分钟才采样一次，采样时间超出 15 秒窗口也必须继续复用。
	c.mu.Lock()
	c.diskUpdatedAt = now.Add(-time.Hour)
	c.mu.Unlock()
	snapshot, ok := c.snapshotAt(now)
	if !ok {
		t.Fatal("磁盘使用率不应参与 15 秒全量新鲜度门槛")
	}
	if !snapshot.DiskUsageSampled {
		t.Fatal("重新采样磁盘使用率的样本必须标记 disk_usage_sampled")
	}
	if snapshot.DiskUsedBytes != 4096 || snapshot.DiskTotalBytes != 8192 {
		t.Fatalf("unexpected disk values: %#v", snapshot)
	}
	// 第二轮没有重新采样磁盘：继续携带最近值，但不再标记 sampled。
	c.setCPU(13)
	c.setMemory(2048, 1024)
	c.setStatus(linux.SystemStatus{LoadAverage: "1.00 0.50 0.25"})
	c.accumulateNetworkAt(3000, 1500, now)
	snapshot, ok = c.snapshotAt(now)
	if !ok {
		t.Fatal("expected the cache to stay ready between disk samples")
	}
	if snapshot.DiskUsageSampled {
		t.Fatal("未重新采样磁盘的样本不得标记 disk_usage_sampled")
	}
	if snapshot.DiskUsedBytes != 4096 || snapshot.DiskTotalBytes != 8192 {
		t.Fatalf("disk values must be reused, got %#v", snapshot)
	}
}

func TestMetricCacheSnapshotIncludesFreshDiskIO(t *testing.T) {
	c := &metricCache{}
	now := time.Now().UTC()
	c.setDisk(8192, 4096)
	seedFreshMetrics(c, now)
	c.accumulateDiskIOAt(0, 0, now.Add(-2*time.Second))
	c.accumulateDiskIOAt(2048, 1024, now.Add(-time.Second))
	snapshot, ok := c.snapshotAt(now)
	if !ok {
		t.Fatal("expected a fresh cache to snapshot ok")
	}
	if !snapshot.DiskIOAvailable {
		t.Fatal("expected disk IO to be available")
	}
	if snapshot.DiskReadBytesRate != 2048 || snapshot.DiskWriteBytesRate != 1024 {
		t.Fatalf("disk IO rates = (%v, %v), want (2048, 1024)", snapshot.DiskReadBytesRate, snapshot.DiskWriteBytesRate)
	}
}

func TestMetricCacheSnapshotOmitsStaleDiskIO(t *testing.T) {
	c := &metricCache{}
	now := time.Now().UTC()
	c.setDisk(8192, 4096)
	seedFreshMetrics(c, now)
	// 读数已超出 15 秒窗口：不能把冻结的旧速率当成新样本，但其它指标不受影响。
	c.accumulateDiskIOAt(0, 0, now.Add(-30*time.Second))
	c.accumulateDiskIOAt(2048, 1024, now.Add(-29*time.Second))
	snapshot, ok := c.snapshotAt(now)
	if !ok {
		t.Fatal("磁盘 IO 未就绪不得拖停其它指标")
	}
	if snapshot.DiskIOAvailable || snapshot.DiskReadBytesRate != 0 || snapshot.DiskWriteBytesRate != 0 {
		t.Fatalf("stale disk IO must be reported as unavailable: %#v", snapshot)
	}
}

func TestMetricCacheClearDiskIOResetsAvailability(t *testing.T) {
	c := &metricCache{}
	now := time.Now().UTC()
	c.setDisk(8192, 4096)
	seedFreshMetrics(c, now)
	c.accumulateDiskIOAt(0, 0, now.Add(-2*time.Second))
	c.accumulateDiskIOAt(2048, 1024, now.Add(-time.Second))
	c.clearDiskIO()
	snapshot, ok := c.snapshotAt(now)
	if !ok {
		t.Fatal("expected snapshot to stay ready after disk IO failure")
	}
	if snapshot.DiskIOAvailable || snapshot.DiskReadBytesRate != 0 {
		t.Fatalf("cleared disk IO must stay unavailable: %#v", snapshot)
	}
	// 重新读数需要两次采样建立窗口，第一次只建立基线。
	c.accumulateDiskIOAt(4096, 2048, now)
	if _, _, ok := c.consumeDiskIOLocked(); ok {
		t.Fatal("a single reading after a reset has no window yet")
	}
}

func TestMetricCacheDiskIOAveragesOverWindow(t *testing.T) {
	c := &metricCache{}
	base := time.Unix(1000, 0).UTC()
	c.accumulateDiskIOAt(1000, 0, base)
	c.accumulateDiskIOAt(3000, 500, base.Add(time.Second))
	c.accumulateDiskIOAt(8000, 2000, base.Add(4*time.Second))
	c.mu.Lock()
	read, write, ok := c.consumeDiskIOLocked()
	c.mu.Unlock()
	// 4 秒窗口累计读 7000、写 2000 字节。
	if !ok || read != 1750 || write != 500 {
		t.Fatalf("consume disk IO = (%v, %v, %v), want (1750, 500, true)", read, write, ok)
	}
	c.mu.Lock()
	_, _, ok = c.consumeDiskIOLocked()
	c.mu.Unlock()
	if ok {
		t.Fatal("a second consume without new readings must not produce a rate")
	}
}

func TestMetricCacheSkipsDiskIOCounterReset(t *testing.T) {
	c := &metricCache{}
	base := time.Unix(1000, 0).UTC()
	c.accumulateDiskIOAt(8000, 2000, base)
	c.accumulateDiskIOAt(9000, 2500, base.Add(time.Second))
	// 计数器回退（设备重建/回绕）按 0 增量跳过，不产生负值或突刺。
	c.accumulateDiskIOAt(500, 100, base.Add(2*time.Second))
	c.accumulateDiskIOAt(2500, 1100, base.Add(3*time.Second))
	c.mu.Lock()
	read, write, ok := c.consumeDiskIOLocked()
	c.mu.Unlock()
	// 3 秒窗口累计读 3000、写 1500 字节。
	if !ok || read != 1000 || write != 500 {
		t.Fatalf("consume disk IO = (%v, %v, %v), want (1000, 500, true)", read, write, ok)
	}
}

func metricReport(at time.Time) *agentpb.AgentReport {
	return &agentpb.AgentReport{
		SampleAt: timestamppb.New(at.UTC()),
		Reason:   "scheduled",
		Metrics:  &agentpb.MetricsSnapshotResponse{CpuUsagePercent: 12},
	}
}

func TestSpoolUnsentDrainsQueueAndKeepsOnlyMetrics(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	now := time.Now().UTC()
	watcher := &reportWatcher{ch: make(chan *agentpb.AgentReport, 4)}
	failed := metricReport(now)
	failed.Containers = &agentpb.DockerContainersResponse{}
	watcher.ch <- &agentpb.AgentReport{SampleAt: timestamppb.New(now), Reason: "scheduled", Containers: &agentpb.DockerContainersResponse{}}
	watcher.ch <- metricReport(now.Add(-time.Second))
	hub.spoolUnsent(watcher, failed)
	if remaining := len(watcher.ch); remaining != 0 {
		t.Fatalf("watcher queue must be drained, %d reports left", remaining)
	}
	if len(hub.spool) != 2 {
		t.Fatalf("spool length = %d, want 2", len(hub.spool))
	}
	for _, report := range hub.spool {
		if report.GetMetrics() == nil {
			t.Fatalf("spooled report must carry metrics: %#v", report)
		}
		if report.GetContainers() != nil {
			t.Fatalf("replayed reports must be metrics-only: %#v", report)
		}
	}
}

func TestMetricsSpoolKeepsNewestSamplesAndDropsAged(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	now := time.Now().UTC()
	watcher := &reportWatcher{ch: make(chan *agentpb.AgentReport, 1)}
	hub.spoolUnsent(watcher, metricReport(now.Add(-metricsSpoolMaxAge-time.Minute)))
	if len(hub.spool) != 0 {
		t.Fatal("超龄指标样本不得进入补报缓冲")
	}
	total := metricsSpoolCapacity + 5
	for index := 0; index < total; index++ {
		hub.spoolUnsent(watcher, metricReport(now.Add(-time.Duration(total-index)*time.Second)))
	}
	if len(hub.spool) != metricsSpoolCapacity {
		t.Fatalf("spool length = %d, want %d", len(hub.spool), metricsSpoolCapacity)
	}
	oldest := hub.spool[0].GetSampleAt().AsTime()
	newest := hub.spool[len(hub.spool)-1].GetSampleAt().AsTime()
	if want := now.Add(-time.Duration(metricsSpoolCapacity) * time.Second); oldest.Unix() != want.Unix() {
		t.Fatalf("oldest spooled sample = %s, want ~%s", oldest, want)
	}
	if want := now.Add(-time.Second); newest.Unix() != want.Unix() {
		t.Fatalf("newest spooled sample = %s, want ~%s", newest, want)
	}
}

func TestTakeSpoolSortsBySampleTimeAndClears(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	now := time.Now().UTC()
	hub.spool = []*agentpb.AgentReport{
		metricReport(now.Add(-2 * time.Second)),
		metricReport(now.Add(-4 * time.Second)),
		metricReport(now.Add(-3 * time.Second)),
	}
	items := hub.takeSpool()
	if len(items) != 3 {
		t.Fatalf("takeSpool returned %d items, want 3", len(items))
	}
	for index := 1; index < len(items); index++ {
		if !items[index-1].GetSampleAt().AsTime().Before(items[index].GetSampleAt().AsTime()) {
			t.Fatalf("takeSpool must be sorted ascending: %#v", items)
		}
	}
	if len(hub.takeSpool()) != 0 {
		t.Fatal("takeSpool must clear the buffer")
	}
}

func TestReplaySpoolSendsAscendingAndMarksReplayed(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	now := time.Now().UTC()
	hub.spool = []*agentpb.AgentReport{
		metricReport(now.Add(-2 * time.Second)),
		metricReport(now.Add(-3 * time.Second)),
		metricReport(now.Add(-time.Second)),
	}
	stream := &fakeReportStream{ctx: context.Background()}
	if err := hub.replaySpool(stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != 3 {
		t.Fatalf("replayed %d reports, want 3", len(stream.sent))
	}
	for index, report := range stream.sent {
		if !report.GetReplayed() {
			t.Fatalf("replayed report %d must be marked replayed", index)
		}
	}
	if len(hub.takeSpool()) != 0 {
		t.Fatal("replay must empty the spool")
	}
}

func TestReplaySpoolRestoresRemainingOnSendFailure(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	now := time.Now().UTC()
	hub.spool = []*agentpb.AgentReport{
		metricReport(now.Add(-3 * time.Second)),
		metricReport(now.Add(-2 * time.Second)),
		metricReport(now.Add(-time.Second)),
	}
	sends := 0
	stream := &fakeReportStream{ctx: context.Background(), send: func(*agentpb.AgentReport) error {
		sends++
		if sends == 2 {
			return errors.New("send failed")
		}
		return nil
	}}
	if err := hub.replaySpool(stream); err == nil {
		t.Fatal("expected replay to fail")
	}
	remaining := hub.takeSpool()
	if len(remaining) != 2 {
		t.Fatalf("restored %d reports, want 2", len(remaining))
	}
	if !remaining[0].GetSampleAt().AsTime().Equal(now.Add(-2 * time.Second)) {
		t.Fatalf("first restored sample = %s", remaining[0].GetSampleAt().AsTime())
	}
}

func TestHandlerReportReplaysSpoolOnReconnect(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	handler := &Handler{reports: hub}
	now := time.Now().UTC()
	hub.spool = []*agentpb.AgentReport{
		metricReport(now.Add(-2 * time.Second)),
		metricReport(now.Add(-time.Second)),
	}
	recvCalls := 0
	stream := &fakeReportStream{ctx: context.Background(), recv: func() (*agentpb.AgentReportControl, error) {
		recvCalls++
		if recvCalls == 1 {
			return &agentpb.AgentReportControl{ServerId: "s1"}, nil
		}
		return nil, io.EOF
	}}
	if err := handler.Report(stream); !errors.Is(err, io.EOF) {
		t.Fatalf("Report = %v, want io.EOF", err)
	}
	if len(stream.sent) != 2 {
		t.Fatalf("client received %d replayed reports, want 2", len(stream.sent))
	}
	for _, report := range stream.sent {
		if !report.GetReplayed() {
			t.Fatal("reconnected client must receive replayed=true")
		}
	}
}

func TestHandlerReportSpoolsUnsentMetricsOnSendFailure(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	handler := &Handler{reports: hub}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := true
	stream := &fakeReportStream{
		ctx: ctx,
		recv: func() (*agentpb.AgentReportControl, error) {
			if first {
				first = false
				return &agentpb.AgentReportControl{ServerId: "s1"}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		send: func(*agentpb.AgentReport) error { return errors.New("stream send failed") },
	}
	done := make(chan error, 1)
	go func() { done <- handler.Report(stream) }()
	watcher := waitForWatcher(t, hub)
	now := time.Now().UTC()
	watcher.ch <- metricReport(now)
	watcher.ch <- metricReport(now.Add(-time.Second))
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Report to fail after stream.Send failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Report did not return after send failure")
	}
	spooled := hub.takeSpool()
	if len(spooled) != 2 {
		t.Fatalf("spooled %d reports, want 2 (failed report + drained queue)", len(spooled))
	}
	for _, report := range spooled {
		if report.GetMetrics() == nil {
			t.Fatalf("spooled report must carry metrics: %#v", report)
		}
	}
}

func waitForWatcher(t *testing.T, hub *reportHub) reportWatcherSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if snapshots := hub.snapshot(); len(snapshots) > 0 {
			return snapshots[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("report watcher was not registered")
	return reportWatcherSnapshot{}
}

func TestNormalizeReportConfigParsesProbe(t *testing.T) {
	cfg := normalizeReportConfig(&agentpb.AgentReportControl{
		ServerId: "s1",
		Probe: &agentpb.AgentProbeConfig{
			Enabled:            true,
			IntervalSeconds:    60,
			PingCount:          5,
			PingTimeoutSeconds: 4,
			Targets: []*agentpb.AgentProbeTarget{
				{ServerId: "s2", Address: "10.0.0.2", Name: "web"},
				{ServerId: "s3", Address: "  10.0.0.3  "},
				{ServerId: "s4", Address: "  "},
				nil,
			},
		},
	})
	if !cfg.probeSet || !cfg.probe.enabled {
		t.Fatalf("probe config not parsed: %#v", cfg)
	}
	if cfg.probe.interval != time.Minute {
		t.Fatalf("probe interval = %s, want 1m", cfg.probe.interval)
	}
	if cfg.probe.pingCount != 5 || cfg.probe.pingTimeoutSeconds != 4 {
		t.Fatalf("ping options = (%d, %d), want (5, 4)", cfg.probe.pingCount, cfg.probe.pingTimeoutSeconds)
	}
	want := []agentprobe.Target{
		{ServerID: "s2", Address: "10.0.0.2", Name: "web"},
		{ServerID: "s3", Address: "10.0.0.3"},
	}
	if !reflect.DeepEqual(cfg.probe.targets, want) {
		t.Fatalf("probe targets = %#v, want %#v", cfg.probe.targets, want)
	}
	// 显式关闭/清空目标必须整体替换，而不是保留旧目标；旧 Panel 不填 ping
	// 参数（0）时回退默认 3 包/2 秒。
	disabled := normalizeReportConfig(&agentpb.AgentReportControl{Probe: &agentpb.AgentProbeConfig{Enabled: false}})
	if !disabled.probeSet || disabled.probe.enabled || len(disabled.probe.targets) != 0 {
		t.Fatalf("disabled probe config = %#v", disabled)
	}
	if disabled.probe.interval != defaultProbeInterval {
		t.Fatalf("default probe interval = %s, want %s", disabled.probe.interval, defaultProbeInterval)
	}
	if disabled.probe.pingCount != agentprobe.DefaultCount || disabled.probe.pingTimeoutSeconds != agentprobe.DefaultTimeoutSeconds {
		t.Fatalf("default ping options = (%d, %d), want (%d, %d)", disabled.probe.pingCount, disabled.probe.pingTimeoutSeconds, agentprobe.DefaultCount, agentprobe.DefaultTimeoutSeconds)
	}
	// 越界值必须夹紧，不能下发给 ping 放大负载。
	clamped := normalizeReportConfig(&agentpb.AgentReportControl{Probe: &agentpb.AgentProbeConfig{
		Enabled:            true,
		PingCount:          99,
		PingTimeoutSeconds: -3,
	}})
	if clamped.probe.pingCount != agentprobe.MaxCount || clamped.probe.pingTimeoutSeconds != agentprobe.DefaultTimeoutSeconds {
		t.Fatalf("clamped ping options = (%d, %d), want (%d, %d)", clamped.probe.pingCount, clamped.probe.pingTimeoutSeconds, agentprobe.MaxCount, agentprobe.DefaultTimeoutSeconds)
	}
	// 旧 Panel 不携带 probe 字段：保持“未设置”语义，不覆盖现有任务。
	if normalizeReportConfig(&agentpb.AgentReportControl{ServerId: "s1"}).probeSet {
		t.Fatal("a control message without probe must not be treated as probe config")
	}
}

func TestReportHubUpdateReplacesProbeConfig(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	hub.probeRunner = func(context.Context, string) (string, error) { return probeSamplerPingOutput, nil }
	watcher := hub.add(reportConfig{serverID: "s1", probeSet: true, probe: probeConfig{
		enabled:            true,
		interval:           time.Minute,
		targets:            []agentprobe.Target{{ServerID: "s2", Address: "10.0.0.2"}},
		pingCount:          5,
		pingTimeoutSeconds: 4,
	}})
	defer hub.remove(watcher.id)
	hub.update(watcher.id, normalizeReportConfig(&agentpb.AgentReportControl{
		ServerId: "s1",
		Probe:    &agentpb.AgentProbeConfig{Enabled: false},
	}))
	cfg := hub.probeConfigSnapshot()
	if cfg.enabled || len(cfg.targets) != 0 {
		t.Fatalf("probe config must be replaced wholesale: %#v", cfg)
	}
	// 关闭后的控制消息不带 ping 参数：必须回到默认口径，而不是沿用旧值。
	if cfg.pingCount != agentprobe.DefaultCount || cfg.pingTimeoutSeconds != agentprobe.DefaultTimeoutSeconds {
		t.Fatalf("disabled probe ping options = (%d, %d), want defaults", cfg.pingCount, cfg.pingTimeoutSeconds)
	}
	hub.update(watcher.id, normalizeReportConfig(&agentpb.AgentReportControl{
		ServerId: "s1",
		Probe: &agentpb.AgentProbeConfig{
			Enabled:            true,
			IntervalSeconds:    90,
			PingCount:          6,
			PingTimeoutSeconds: 3,
			Targets:            []*agentpb.AgentProbeTarget{{ServerId: "s3", Address: "10.0.0.3"}},
		},
	}))
	cfg = hub.probeConfigSnapshot()
	if !cfg.enabled || cfg.interval != 90*time.Second || len(cfg.targets) != 1 || cfg.targets[0].ServerID != "s3" {
		t.Fatalf("probe config not replaced by the new control message: %#v", cfg)
	}
	if cfg.pingCount != 6 || cfg.pingTimeoutSeconds != 3 {
		t.Fatalf("probe ping options = (%d, %d), want (6, 3)", cfg.pingCount, cfg.pingTimeoutSeconds)
	}
}

const probeSamplerPingOutput = `--- 10.0.0.9 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss
rtt min/avg/max/mdev = 1.0/2.0/3.0/0.1 ms
`

func TestRunProbeSamplerBroadcastsToEnabledWatchersOnly(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	hub.probeRunner = func(context.Context, string) (string, error) { return probeSamplerPingOutput, nil }
	disabled := hub.add(reportConfig{serverID: "s1", probeSet: true, probe: probeConfig{enabled: false}})
	enabled := hub.add(reportConfig{serverID: "s2", probeSet: true, probe: probeConfig{
		enabled:  true,
		interval: time.Hour,
		targets:  []agentprobe.Target{{ServerID: "s9", Address: "10.0.0.9", Name: "target"}},
	}})
	defer hub.remove(disabled.id)
	defer hub.remove(enabled.id)
	select {
	case report := <-enabled.ch:
		if report.GetReason() != "network_probe" {
			t.Fatalf("probe report reason = %q, want network_probe", report.GetReason())
		}
		items := report.GetProbes().GetItems()
		if len(items) != 1 {
			t.Fatalf("probe report items = %#v", items)
		}
		if items[0].GetTargetServerId() != "s9" || items[0].GetTargetAddress() != "10.0.0.9" || items[0].GetLatencyMs() != 2 {
			t.Fatalf("unexpected probe result: %#v", items[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("enabled watcher did not receive a probe report")
	}
	select {
	case report := <-disabled.ch:
		t.Fatalf("disabled watcher received a probe report: %#v", report)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunProbeSamplerRequiresEnabledTargets(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	hub.probeRunner = func(context.Context, string) (string, error) {
		t.Error("probe runner must not run without enabled targets")
		return "", errors.New("unexpected probe")
	}
	watcher := hub.add(reportConfig{serverID: "s1", probeSet: true, probe: probeConfig{enabled: true}})
	defer hub.remove(watcher.id)
	select {
	case report := <-watcher.ch:
		t.Fatalf("watcher without targets received a probe report: %#v", report)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestRunProbeSamplerUsesConfiguredPingOptions(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	deadlines := make(chan time.Duration, 1)
	hub.probeRunner = func(ctx context.Context, _ string) (string, error) {
		if deadline, ok := ctx.Deadline(); ok {
			deadlines <- time.Until(deadline)
		}
		return probeSamplerPingOutput, nil
	}
	watcher := hub.add(reportConfig{serverID: "s1", probeSet: true, probe: probeConfig{
		enabled:            true,
		interval:           time.Hour,
		pingCount:          4,
		pingTimeoutSeconds: 3,
		targets:            []agentprobe.Target{{ServerID: "s9", Address: "10.0.0.9"}},
	}})
	defer hub.remove(watcher.id)
	select {
	case remaining := <-deadlines:
		// 4 包 * 3 秒 + 2 秒余量 = 14 秒。
		if remaining <= 13*time.Second || remaining > 14*time.Second {
			t.Fatalf("per-target timeout = %s, want ~14s", remaining)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("probe runner was not invoked")
	}
}

type fakeReportStream struct {
	grpc.ServerStream
	ctx  context.Context
	recv func() (*agentpb.AgentReportControl, error)
	send func(*agentpb.AgentReport) error
	sent []*agentpb.AgentReport
}

func (f *fakeReportStream) Context() context.Context {
	if f.ctx != nil {
		return f.ctx
	}
	return context.Background()
}

func (f *fakeReportStream) Recv() (*agentpb.AgentReportControl, error) {
	if f.recv == nil {
		return nil, io.EOF
	}
	return f.recv()
}

func (f *fakeReportStream) Send(report *agentpb.AgentReport) error {
	if f.send != nil {
		if err := f.send(report); err != nil {
			return err
		}
	}
	f.sent = append(f.sent, report)
	return nil
}
