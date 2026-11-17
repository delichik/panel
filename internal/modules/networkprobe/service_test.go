package networkprobe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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
	interval_seconds INTEGER NOT NULL DEFAULT 60,
	ping_count INTEGER NOT NULL DEFAULT 3,
	ping_timeout_seconds INTEGER NOT NULL DEFAULT 2,
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
	history_json TEXT NOT NULL DEFAULT '[]',
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

// saveProbeSample 用单目标上报写入一条 srv-a → target 的探测结果。
func saveProbeSample(t *testing.T, svc *Service, at time.Time, target string, latency, loss float64, sent, received int, errMsg string) {
	t.Helper()
	if err := svc.SaveReported(context.Background(), "srv-a", at, []ReportedResult{{
		TargetServerID:    target,
		TargetAddress:     "fd00::b",
		LatencyMS:         latency,
		PacketLossPercent: loss,
		Sent:              sent,
		Received:          received,
		Error:             errMsg,
	}}); err != nil {
		t.Fatal(err)
	}
}

// historyFor 返回指定 target 的探测历史；不存在时返回 nil。
func historyFor(results []Result, target string) []HistoryPoint {
	for _, result := range results {
		if result.TargetServerID == target {
			return result.History
		}
	}
	return nil
}

// TestSaveReportedMergesThirtyMinuteHistoryBuckets covers the probe history merge
// contract: samples inside one 30-minute bucket replace each other, later buckets
// append, and the stored history is exposed ascending with RFC3339 times.
func TestSaveReportedMergesThirtyMinuteHistoryBuckets(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	// now 是 12:00 UTC，正好落在 30 分钟桶边界上。
	bucket0 := *now
	saveProbeSample(t, svc, bucket0.Add(2*time.Minute), "srv-b", 4, 1, 3, 3, "")
	saveProbeSample(t, svc, bucket0.Add(17*time.Minute), "srv-b", 6, 2, 3, 3, "")
	saveProbeSample(t, svc, bucket0.Add(31*time.Minute), "srv-b", 8, 0, 3, 3, "")

	got, err := svc.ServerResultsHistory(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	history := historyFor(got.Results, "srv-b")
	if len(history) != 2 {
		t.Fatalf("history = %#v, want 2 buckets (same-bucket replacement, cross-bucket append)", history)
	}
	if !history[0].Time.Equal(bucket0) || history[0].LatencyMS != 6 || history[0].PacketLossPercent != 2 {
		t.Fatalf("first bucket = %#v, want the replacement sample of the 12:00 bucket", history[0])
	}
	if !history[1].Time.Equal(bucket0.Add(30*time.Minute)) || history[1].LatencyMS != 8 || history[1].PacketLossPercent != 0 {
		t.Fatalf("second bucket = %#v", history[1])
	}
	// 持久化格式使用紧凑键名。
	var raw string
	if err := svc.db.QueryRow(`SELECT history_json FROM network_probe_results WHERE source_server_id='srv-a' AND target_server_id='srv-b'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored []storedHistoryPoint
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("history_json = %q, want a JSON array: %v", raw, err)
	}
	if len(stored) != 2 || stored[0].Time != bucket0.Unix() || stored[1].Time != bucket0.Add(30*time.Minute).Unix() {
		t.Fatalf("stored history = %#v (raw %q)", stored, raw)
	}
}

// TestSaveReportedHistoryPrunesBeyond24Hours covers the 24h retention window.
func TestSaveReportedHistoryPrunesBeyond24Hours(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	saveProbeSample(t, svc, now.Add(-24*time.Hour-30*time.Minute), "srv-b", 1, 0, 3, 3, "")
	saveProbeSample(t, svc, now.Add(-24*time.Hour), "srv-b", 2, 0, 3, 3, "")
	saveProbeSample(t, svc, *now, "srv-b", 3, 0, 3, 3, "")

	got, err := svc.ServerResultsHistory(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	history := historyFor(got.Results, "srv-b")
	if len(history) != 2 {
		t.Fatalf("history = %#v, want the oldest bucket pruned", history)
	}
	if !history[0].Time.Equal(now.Add(-24*time.Hour)) || !history[1].Time.Equal(*now) {
		t.Fatalf("history times = %#v", history)
	}
}

// TestSaveReportedHistoryCapsAt48Points covers the 48-point upper bound: the
// bucket at the 24h boundary plus 48 later buckets must drop the oldest point.
func TestSaveReportedHistoryCapsAt48Points(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	start := now.Add(-24 * time.Hour)
	for i := 0; i <= 48; i++ {
		saveProbeSample(t, svc, start.Add(time.Duration(i)*30*time.Minute), "srv-b", float64(i), 0, 3, 3, "")
	}
	got, err := svc.ServerResultsHistory(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	history := historyFor(got.Results, "srv-b")
	if len(history) != 48 {
		t.Fatalf("history length = %d, want 48", len(history))
	}
	if !history[0].Time.Equal(start.Add(30*time.Minute)) || !history[47].Time.Equal(*now) {
		t.Fatalf("capped history span = %s..%s", history[0].Time, history[47].Time)
	}
}

// TestSaveReportedHistoryFailureSamples covers the failure semantics: error or
// received<=0 samples are recorded as 0ms latency and 100% loss, while successful
// samples carry the reported latency and packet-loss values.
func TestSaveReportedHistoryFailureSamples(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	saveProbeSample(t, svc, *now, "srv-b", 123, 0, 3, 0, "request timeout")
	saveProbeSample(t, svc, now.Add(30*time.Minute), "srv-b", 5, 1, 3, 0, "")
	saveProbeSample(t, svc, now.Add(60*time.Minute), "srv-b", 0, 12.5, 3, 3, "")

	got, err := svc.ServerResultsHistory(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	history := historyFor(got.Results, "srv-b")
	if len(history) != 3 {
		t.Fatalf("history = %#v", history)
	}
	for i, point := range history[:2] {
		if point.LatencyMS != 0 || point.PacketLossPercent != 100 {
			t.Fatalf("failure sample %d = %#v, want 0ms/100%%", i, point)
		}
	}
	if history[2].LatencyMS != 0 || history[2].PacketLossPercent != 12.5 {
		t.Fatalf("success sample = %#v, want the reported values", history[2])
	}
}

// TestProbeHistoryOnlyOnExplicitRequest keeps the existing wire contract: only
// ServerResultsHistory materializes history; the plain server-results payload,
// the facility matrix and the overview card must stay history-free.
func TestProbeHistoryOnlyOnExplicitRequest(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	saveProbeSample(t, svc, *now, "srv-b", 7, 1, 3, 3, "")

	plain, err := svc.ServerResults(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Results) != 1 || len(plain.Results[0].History) != 0 {
		t.Fatalf("plain server results must not carry history: %#v", plain.Results)
	}
	matrix, err := svc.Matrix(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Results) != 1 || len(matrix.Results[0].History) != 0 {
		t.Fatalf("matrix must not carry history: %#v", matrix.Results)
	}
	card, err := svc.CardData(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Results) != 1 || len(card.Results[0].History) != 0 {
		t.Fatalf("card data must not carry history: %#v", card.Results)
	}
	for name, payload := range map[string]any{"matrix": matrix, "card": card, "server": plain} {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"history"`) {
			t.Fatalf("%s payload must not serialize history: %s", name, encoded)
		}
	}

	detailed, err := svc.ServerResultsHistory(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	history := historyFor(detailed.Results, "srv-b")
	if len(history) != 1 || history[0].LatencyMS != 7 || history[0].PacketLossPercent != 1 {
		t.Fatalf("detailed history = %#v", history)
	}
	encoded, err := json.Marshal(detailed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"history":[{"time":`) {
		t.Fatalf("detailed payload must serialize history: %s", encoded)
	}
}

// TestServerResultsHistoryMissingServer keeps the not_found contract on the
// history-enabled path.
func TestServerResultsHistoryMissingServer(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	_, err := svc.ServerResultsHistory(context.Background(), "missing")
	requirePanelCode(t, err, "not_found")
}

// TestSaveProbeParameterDefaultsAndBounds covers FAC-PRB: omitted (0) parameter
// values are normalized to the documented defaults, boundary values are
// accepted, and out-of-range values return the stable error codes without writing
// the configuration.
func TestSaveProbeParameterDefaultsAndBounds(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()

	saved, err := svc.Save(ctx, SaveInput{Mode: ModeAll})
	if err != nil {
		t.Fatal(err)
	}
	if saved.IntervalSeconds != DefaultProbeIntervalSeconds || saved.PingCount != DefaultPingCount || saved.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("omitted parameters must default, got %#v", saved)
	}
	reloaded, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IntervalSeconds != DefaultProbeIntervalSeconds || reloaded.PingCount != DefaultPingCount || reloaded.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("persisted defaults = %#v", reloaded)
	}

	// 合法边界值必须原样保存。
	saved, err = svc.Save(ctx, SaveInput{Mode: ModeAll, IntervalSeconds: MinProbeIntervalSeconds, PingCount: MinPingCount, PingTimeoutSeconds: MinPingTimeoutSeconds})
	if err != nil {
		t.Fatal(err)
	}
	if saved.IntervalSeconds != MinProbeIntervalSeconds || saved.PingCount != MinPingCount || saved.PingTimeoutSeconds != MinPingTimeoutSeconds {
		t.Fatalf("minimum bounds = %#v", saved)
	}
	saved, err = svc.Save(ctx, SaveInput{Mode: ModeAll, IntervalSeconds: MaxProbeIntervalSeconds, PingCount: MaxPingCount, PingTimeoutSeconds: MaxPingTimeoutSeconds})
	if err != nil {
		t.Fatal(err)
	}
	if saved.IntervalSeconds != MaxProbeIntervalSeconds || saved.PingCount != MaxPingCount || saved.PingTimeoutSeconds != MaxPingTimeoutSeconds {
		t.Fatalf("maximum bounds = %#v", saved)
	}

	before, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   SaveInput
		code string
	}{
		{"negative interval", SaveInput{Mode: ModeAll, IntervalSeconds: -1}, "network_probe_interval_invalid"},
		{"interval below minimum", SaveInput{Mode: ModeAll, IntervalSeconds: MinProbeIntervalSeconds - 1}, "network_probe_interval_invalid"},
		{"interval above maximum", SaveInput{Mode: ModeAll, IntervalSeconds: MaxProbeIntervalSeconds + 1}, "network_probe_interval_invalid"},
		{"negative ping count", SaveInput{Mode: ModeAll, PingCount: -1}, "network_probe_ping_count_invalid"},
		{"ping count above maximum", SaveInput{Mode: ModeAll, PingCount: MaxPingCount + 1}, "network_probe_ping_count_invalid"},
		{"negative ping timeout", SaveInput{Mode: ModeAll, PingTimeoutSeconds: -1}, "network_probe_ping_timeout_invalid"},
		{"ping timeout above maximum", SaveInput{Mode: ModeAll, PingTimeoutSeconds: MaxPingTimeoutSeconds + 1}, "network_probe_ping_timeout_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Save(ctx, tc.in); err != nil {
				requirePanelCode(t, err, tc.code)
			} else {
				t.Fatalf("expected %s, save succeeded", tc.code)
			}
		})
	}
	after, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.IntervalSeconds != before.IntervalSeconds || after.PingCount != before.PingCount || after.PingTimeoutSeconds != before.PingTimeoutSeconds {
		t.Fatalf("invalid saves must not persist: before=%#v after=%#v", before, after)
	}
}

// TestLoadConfigNormalizesLegacyAndOutOfRangeParameters covers defensive reads:
// legacy alpha rows with 0 and manually corrupted out-of-range values fall back
// to the defaults instead of being handed to agents.
func TestLoadConfigNormalizesLegacyAndOutOfRangeParameters(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	empty, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if empty.IntervalSeconds != DefaultProbeIntervalSeconds || empty.PingCount != DefaultPingCount || empty.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("missing config row must expose defaults: %#v", empty)
	}
	if _, err := svc.db.Exec(`INSERT INTO network_probe_configs(id,mode,server_ids_json,interval_seconds,ping_count,ping_timeout_seconds,version,updated_at) VALUES('network_probe','all','[]',0,0,0,4,'2026-11-16T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 4 || cfg.IntervalSeconds != DefaultProbeIntervalSeconds || cfg.PingCount != DefaultPingCount || cfg.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("legacy zero row normalization = %#v", cfg)
	}
	report, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if report.IntervalSeconds != DefaultProbeIntervalSeconds || report.PingCount != DefaultPingCount || report.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("legacy zero row report = %#v", report)
	}

	if _, err := svc.db.Exec(`UPDATE network_probe_configs SET interval_seconds=99999,ping_count=-3,ping_timeout_seconds=11 WHERE id='network_probe'`); err != nil {
		t.Fatal(err)
	}
	svc.invalidate()
	outOfRange, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outOfRange.IntervalSeconds != DefaultProbeIntervalSeconds || outOfRange.PingCount != DefaultPingCount || outOfRange.PingTimeoutSeconds != DefaultPingTimeoutSeconds {
		t.Fatalf("out-of-range row normalization = %#v", outOfRange)
	}
}

// TestReportConfigCarriesConfiguredParameters covers the downstream contract:
// the saved probe parameters are what the report stream sends to agents.
func TestReportConfigCarriesConfiguredParameters(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, IntervalSeconds: 120, PingCount: 5, PingTimeoutSeconds: 4}); err != nil {
		t.Fatal(err)
	}
	report, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Enabled || report.IntervalSeconds != 120 || report.PingCount != 5 || report.PingTimeoutSeconds != 4 {
		t.Fatalf("report = %#v, want configured parameters", report)
	}
	if len(report.Targets) == 0 {
		t.Fatal("enabled report must carry targets")
	}
}

// TestStaleThresholdScalesWithInterval covers the freshness rule
// max(5min, 3*intervalSeconds): larger intervals must widen the stale window so
// results do not stay stale forever after the user slows probing down.
func TestStaleThresholdScalesWithInterval(t *testing.T) {
	svc, _, now := newTestService(t, testServers()...)
	ctx := context.Background()
	matrixStale := func() bool {
		svc.invalidate()
		matrix, err := svc.Matrix(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range matrix.Results {
			if result.SourceServerID == "srv-a" && result.TargetServerID == "srv-b" {
				return result.Stale
			}
		}
		t.Fatalf("result not found in matrix: %#v", matrix.Results)
		return false
	}

	// 120s 间隔 → 阈值 6 分钟：5.5 分钟旧仍新鲜，6.5 分钟旧已过期。
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, IntervalSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	saveProbeSample(t, svc, now.Add(-5*time.Minute-30*time.Second), "srv-b", 1, 0, 3, 3, "")
	if matrixStale() {
		t.Fatal("5.5-minute-old result must be fresh with a 6-minute threshold")
	}
	saveProbeSample(t, svc, now.Add(-6*time.Minute-30*time.Second), "srv-b", 1, 0, 3, 3, "")
	if !matrixStale() {
		t.Fatal("6.5-minute-old result must be stale with a 6-minute threshold")
	}

	// 默认 60s 间隔 → 仍使用固定 5 分钟下限。
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll}); err != nil {
		t.Fatal(err)
	}
	saveProbeSample(t, svc, now.Add(-5*time.Minute-30*time.Second), "srv-b", 1, 0, 3, 3, "")
	if !matrixStale() {
		t.Fatal("5.5-minute-old result must be stale with the 5-minute default threshold")
	}

	// 3600s 间隔 → 阈值 3 小时：1 小时旧仍新鲜。
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, IntervalSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	saveProbeSample(t, svc, now.Add(-time.Hour), "srv-b", 1, 0, 3, 3, "")
	if matrixStale() {
		t.Fatal("1-hour-old result must be fresh with a 3-hour threshold")
	}
}
