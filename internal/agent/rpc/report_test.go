package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	agentsystem "panel/internal/agent/system"
	"panel/internal/platform/linux"
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
	c.setCPU(12)
	c.setMemory(2048, 1024)
	c.setDisk(8192, 4096)
	c.setNetwork(1.5, 0.5)
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
func (okReportCollector) NetworkRates(context.Context) (float64, float64, error) {
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
func (failingReportCollector) NetworkRates(context.Context) (float64, float64, error) {
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
func TestMetricCacheRequiresAllParts(t *testing.T) {
	c := &metricCache{}
	if _, ok := c.snapshot(); ok {
		t.Fatal("cache without values must not be ready")
	}
	c.setCPU(12)
	c.setMemory(2048, 1024)
	c.setDisk(8192, 4096)
	c.setNetwork(100, 200)
	c.setStatus(linux.SystemStatus{LoadAverage: "1.00 0.50 0.25"})
	snap, ok := c.snapshot()
	if !ok {
		t.Fatal("cache with all parts must be ready")
	}
	if snap.CPUUsagePercent != 12 || snap.MemoryUsedBytes != 1024 || snap.NetworkRxBytesRate != 100 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}

func TestCollectAndBroadcastUsesCachedMetrics(t *testing.T) {
	hub := newReportHub(failingReportCollector{}, nil)
	hub.metrics.setCPU(42)
	hub.metrics.setMemory(2048, 1024)
	hub.metrics.setDisk(8192, 4096)
	hub.metrics.setNetwork(100, 200)
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
