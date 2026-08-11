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

type okReportCollector struct{}

func (okReportCollector) MetricsSnapshot(context.Context, string) (linux.MetricsSnapshot, error) {
	return linux.MetricsSnapshot{}, nil
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

func (failingReportCollector) MetricsSnapshot(context.Context, string) (linux.MetricsSnapshot, error) {
	return linux.MetricsSnapshot{}, errors.New("metrics unavailable")
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