package networkprobe

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	server "panel/internal/modules/servers"
	panelerr "panel/internal/platform/errors"
)

const testCreateTables = `
CREATE TABLE network_probe_configs (
	id TEXT PRIMARY KEY,
	mode TEXT NOT NULL DEFAULT 'all',
	server_ids_json TEXT NOT NULL DEFAULT '[]',
	version INTEGER NOT NULL DEFAULT 1,
	updated_at TEXT NOT NULL
);
CREATE TABLE network_probe_results (
	source_server_id TEXT NOT NULL,
	target_server_id TEXT NOT NULL,
	target_address TEXT NOT NULL DEFAULT '',
	sampled_at TEXT NOT NULL,
	latency_ms REAL NOT NULL DEFAULT 0,
	packet_loss_percent REAL NOT NULL DEFAULT 0,
	sent INTEGER NOT NULL DEFAULT 0,
	received INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (source_server_id, target_server_id)
);
`

type fakeServerProvider struct {
	items []server.Server
}

func (f *fakeServerProvider) List(context.Context) ([]server.Server, error) {
	return append([]server.Server(nil), f.items...), nil
}

func newTestService(t *testing.T, items ...server.Server) (*Service, *fakeServerProvider, *time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(testCreateTables); err != nil {
		t.Fatal(err)
	}
	provider := &fakeServerProvider{items: items}
	svc := NewService(db, provider)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, provider, &now
}

func testServers() []server.Server {
	return []server.Server{
		{ID: "srv-a", Name: "A", IPv4: "10.0.0.1", IPv6: "fd00::a", Host: "10.0.0.1"},
		{ID: "srv-b", Name: "B", IPv4: "", IPv6: "fd00::b", Host: "fd00::b"},
		{ID: "srv-c", Name: "C", IPv4: "", IPv6: "", Host: "node-c.example.test"},
		{ID: "srv-d", Name: "D"},
	}
}

func requirePanelCode(t *testing.T, err error, want string) {
	t.Helper()
	var domain *panelerr.Error
	if !errors.As(err, &domain) {
		t.Fatalf("expected panel error %q, got %v", want, err)
	}
	if domain.Code != want {
		t.Fatalf("error code = %q, want %q", domain.Code, want)
	}
}

// TestReportConfigAllModeBuildsSortedTargets verifies FAC-PRB: all-mode reports
// target every other server with an address (IPv4 || IPv6 || Host), sorted by
// server ID and never including the probing node itself.
func TestReportConfigAllModeBuildsSortedTargets(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.ReportConfig(ctx, "srv-b")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Enabled || report.IntervalSeconds != ProbeIntervalSeconds {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Targets) != 2 {
		t.Fatalf("targets = %#v, want srv-a and srv-c", report.Targets)
	}
	if report.Targets[0].ServerID != "srv-a" || report.Targets[0].Address != "10.0.0.1" {
		t.Fatalf("first target = %#v, want srv-a with its IPv4", report.Targets[0])
	}
	if report.Targets[1].ServerID != "srv-c" || report.Targets[1].Address != "node-c.example.test" {
		t.Fatalf("second target = %#v, want srv-c with its host fallback", report.Targets[1])
	}

	fromA, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA.Targets) != 2 || fromA.Targets[0].ServerID != "srv-b" || fromA.Targets[0].Address != "fd00::b" {
		t.Fatalf("IPv6 fallback target = %#v", fromA.Targets)
	}
}

// TestReportConfigSelectedModeOnlyEnablesListedNodes covers mode=selected: only
// the configured nodes probe, and they still target every other server.
func TestReportConfigSelectedModeOnlyEnablesListedNodes(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-b", "srv-a", "srv-a"}}); err != nil {
		t.Fatal(err)
	}
	disabled, err := svc.ReportConfig(ctx, "srv-c")
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || len(disabled.Targets) != 0 {
		t.Fatalf("srv-c must not probe in selected mode: %#v", disabled)
	}
	enabled, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled {
		t.Fatal("srv-a is listed and must probe")
	}
	// srv-a still targets srv-b (IPv6) and srv-c (host); srv-d has no address.
	if len(enabled.Targets) != 2 || enabled.Targets[0].ServerID != "srv-b" || enabled.Targets[1].ServerID != "srv-c" {
		t.Fatalf("targets = %#v", enabled.Targets)
	}
}

// TestReportConfigSnapshotExpires covers the 3s TTL snapshot: the same call
// within the TTL must not observe a new server list, after it must.
func TestReportConfigSnapshotExpires(t *testing.T) {
	svc, provider, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	provider.items = append(provider.items[:0], testServers()...)
	provider.items = append(provider.items, server.Server{ID: "srv-e", Name: "E", IPv4: "10.0.0.5"})
	cached, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Targets) != len(first.Targets) {
		t.Fatalf("snapshot should be reused within TTL, got %d targets want %d", len(cached.Targets), len(first.Targets))
	}
	*now = now.Add(snapshotTTL)
	refreshed, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Targets) != len(first.Targets)+1 {
		t.Fatalf("snapshot should refresh after TTL, got %d targets", len(refreshed.Targets))
	}
}

// TestSaveValidatesModeAndServers covers the stable validation error codes.
func TestSaveValidatesModeAndServers(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	_, err := svc.Save(ctx, SaveInput{Mode: "bogus"})
	requirePanelCode(t, err, "network_probe_mode_invalid")
	_, err = svc.Save(ctx, SaveInput{Mode: ""})
	requirePanelCode(t, err, "network_probe_mode_invalid")
	_, err = svc.Save(ctx, SaveInput{Mode: ModeSelected})
	requirePanelCode(t, err, "network_probe_servers_required")
	_, err = svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"missing"}})
	requirePanelCode(t, err, "network_probe_server_not_found")

	saved, err := svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-b", "srv-a", "srv-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || saved.Mode != ModeSelected || len(saved.ServerIDs) != 2 || saved.ServerIDs[0] != "srv-a" || saved.ServerIDs[1] != "srv-b" {
		t.Fatalf("unexpected saved config: %#v", saved)
	}
	again, err := svc.Save(ctx, SaveInput{Mode: ModeAll, ServerIDs: []string{"srv-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Version != 2 || again.Mode != ModeAll || len(again.ServerIDs) != 0 {
		t.Fatalf("all mode should clear the persisted server list and bump version: %#v", again)
	}
	reloaded, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 2 || reloaded.Mode != ModeAll {
		t.Fatalf("persisted config = %#v", reloaded)
	}
}

// TestSaveReportedReplacesSourceRows keeps one row per (source,target) and stores
// the sample time truncated to the second.
func TestSaveReportedReplacesSourceRows(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	sampleAt := now.Add(750 * time.Millisecond)
	if err := svc.SaveReported(ctx, "srv-a", sampleAt, []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 1.5, PacketLossPercent: 0, Sent: 3, Received: 3},
		{TargetServerID: "srv-a", LatencyMS: 99},
		{TargetServerID: "", LatencyMS: 99},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-c", sampleAt, []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 7, Sent: 3, Received: 2, PacketLossPercent: 33.3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-a", sampleAt.Add(60*time.Second), []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 2.25, Sent: 3, Received: 3},
		{TargetServerID: "srv-c", TargetAddress: "node-c.example.test", LatencyMS: 5, Sent: 3, Received: 3},
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM network_probe_results WHERE source_server_id='srv-a'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("srv-a rows = %d, want 2", count)
	}
	var latency float64
	var sampledAtRaw string
	if err := svc.db.QueryRow(`SELECT latency_ms,sampled_at FROM network_probe_results WHERE source_server_id='srv-a' AND target_server_id='srv-b'`).Scan(&latency, &sampledAtRaw); err != nil {
		t.Fatal(err)
	}
	if latency != 2.25 {
		t.Fatalf("latency = %v, want the replacement sample", latency)
	}
	want := now.Add(60 * time.Second).UTC().Truncate(time.Second)
	if got := parseTime(sampledAtRaw); !got.Equal(want) {
		t.Fatalf("sampled_at = %s, want %s", got, want)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM network_probe_results WHERE source_server_id='srv-c'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("other sources must be untouched, srv-c rows = %d", count)
	}
}

// TestMatrixFiltersNodesAndTargets covers node filtering, missing targets and the
// Stale flag.
func TestMatrixFiltersNodesAndTargets(t *testing.T) {
	svc, provider, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-a", "srv-b"}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-a", now.Add(-10*time.Minute), []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 3, Sent: 3, Received: 3},
		{TargetServerID: "srv-c", TargetAddress: "node-c.example.test", LatencyMS: 4, Sent: 3, Received: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-c", now.Add(-time.Minute), []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 9, Sent: 3, Received: 1},
	}); err != nil {
		t.Fatal(err)
	}

	matrix, err := svc.Matrix(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Mode != ModeSelected || len(matrix.Nodes) != 2 || matrix.Nodes[0].ServerID != "srv-a" || matrix.Nodes[1].ServerID != "srv-b" {
		t.Fatalf("nodes = %#v", matrix.Nodes)
	}
	if len(matrix.Results) != 2 {
		t.Fatalf("results = %#v, want only sources that are probe nodes", matrix.Results)
	}
	for _, result := range matrix.Results {
		if result.SourceServerID == "srv-c" {
			t.Fatal("srv-c is not a probe node in selected mode and must not contribute results")
		}
		if result.TargetServerID == "srv-b" && !result.Stale {
			t.Fatal("10-minute-old result must be marked stale")
		}
	}
	if matrix.UpdatedAt == nil || !matrix.UpdatedAt.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("updatedAt = %v, want newest sample", matrix.UpdatedAt)
	}

	filtered, err := svc.Matrix(ctx, []string{"srv-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Nodes) != 1 || len(filtered.Results) != 2 {
		t.Fatalf("filtered matrix nodes=%#v results=%#v", filtered.Nodes, filtered.Results)
	}

	// A target that no longer exists must be dropped from the matrix.
	provider.items = []server.Server{testServers()[0], testServers()[1]}
	*now = now.Add(2 * snapshotTTL)
	targetGone, err := svc.Matrix(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range targetGone.Results {
		if result.TargetServerID == "srv-c" {
			t.Fatalf("results for a removed target must be filtered: %#v", result)
		}
	}
}

// TestServerResults covers the per-server tab payload.
func TestServerResults(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-a", now.Add(-time.Minute), []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 2, Sent: 3, Received: 3},
		{TargetServerID: "srv-c", TargetAddress: "node-c.example.test", LatencyMS: 3, Sent: 3, Received: 2},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ServerResults(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.ServerID != "srv-a" || len(got.Nodes) != 2 || len(got.Results) != 2 {
		t.Fatalf("unexpected server results: %#v", got)
	}
	if got.Nodes[0].ServerID != "srv-b" || got.Nodes[1].ServerID != "srv-c" {
		t.Fatalf("target nodes = %#v", got.Nodes)
	}
	if got.UpdatedAt == nil || !got.UpdatedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("updatedAt = %v", got.UpdatedAt)
	}
	_, err = svc.ServerResults(ctx, "missing")
	requirePanelCode(t, err, "not_found")
}

// TestCardDataFiltersProbeSources covers the overview card provider contract.
func TestCardDataFiltersProbeSources(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-a", *now, []ReportedResult{
		{TargetServerID: "srv-b", TargetAddress: "fd00::b", LatencyMS: 2, Sent: 3, Received: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveReported(ctx, "srv-b", *now, []ReportedResult{
		{TargetServerID: "srv-a", TargetAddress: "10.0.0.1", LatencyMS: 4, Sent: 3, Received: 3},
	}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.CardData(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Nodes) != 4 || len(all.Results) != 2 {
		t.Fatalf("all card data nodes=%d results=%d", len(all.Nodes), len(all.Results))
	}
	onlyA, err := svc.CardData(ctx, []string{"srv-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyA.Nodes) != 1 || onlyA.Nodes[0].ServerID != "srv-a" {
		t.Fatalf("filtered nodes = %#v", onlyA.Nodes)
	}
	if len(onlyA.Results) != 1 || onlyA.Results[0].SourceServerID != "srv-a" {
		t.Fatalf("filtered results = %#v", onlyA.Results)
	}
}
