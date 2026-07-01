package rpc

import (
	"testing"
	"time"

	agentsystem "panel/internal/agent/system"
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
