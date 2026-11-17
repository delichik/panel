package metricsfacility

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

// testCreateTables 复刻 spec 2.1 的 metrics_facility_configs 与旧 runtime_settings
// 表结构（含 CHECK 约束），保证服务在真实约束下读写。
const testCreateTables = `
CREATE TABLE metrics_facility_configs (
	id TEXT PRIMARY KEY,
	mode TEXT NOT NULL DEFAULT 'all' CHECK(mode IN ('all','selected')),
	server_ids_json TEXT NOT NULL DEFAULT '[]',
	retention_days INTEGER NOT NULL DEFAULT 7,
	collection_interval_seconds INTEGER NOT NULL DEFAULT 60,
	cleanup_schedule TEXT NOT NULL DEFAULT 'daily' CHECK(cleanup_schedule IN ('hourly','daily','weekly')),
	version INTEGER NOT NULL DEFAULT 1,
	updated_at TEXT NOT NULL
);
CREATE TABLE runtime_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

type fakeServerProvider struct {
	items []server.Server
}

func (f *fakeServerProvider) List(context.Context) ([]server.Server, error) {
	return append([]server.Server(nil), f.items...), nil
}

func testServers() []server.Server {
	return []server.Server{
		{ID: "srv-a", Name: "A"},
		{ID: "srv-b", Name: "B"},
		{ID: "srv-c", Name: "C"},
	}
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

// insertLegacySetting 写入旧 runtime_settings 行，模拟旧 alpha 版本设置页数据。
func insertLegacySetting(t *testing.T, svc *Service, key, value string) {
	t.Helper()
	if _, err := svc.db.Exec(`INSERT INTO runtime_settings(key,value,updated_at) VALUES(?,?,?)`, key, value, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("insert legacy setting %s: %v", key, err)
	}
}

func requireConfig(t *testing.T, got Config, want Config) {
	t.Helper()
	if got.Mode != want.Mode {
		t.Fatalf("mode = %q, want %q", got.Mode, want.Mode)
	}
	if len(got.ServerIDs) != len(want.ServerIDs) {
		t.Fatalf("serverIds = %v, want %v", got.ServerIDs, want.ServerIDs)
	}
	for i := range got.ServerIDs {
		if got.ServerIDs[i] != want.ServerIDs[i] {
			t.Fatalf("serverIds = %v, want %v", got.ServerIDs, want.ServerIDs)
		}
	}
	if got.RetentionDays != want.RetentionDays {
		t.Fatalf("retentionDays = %d, want %d", got.RetentionDays, want.RetentionDays)
	}
	if got.CollectionIntervalSeconds != want.CollectionIntervalSeconds {
		t.Fatalf("collectionIntervalSeconds = %d, want %d", got.CollectionIntervalSeconds, want.CollectionIntervalSeconds)
	}
	if got.CleanupSchedule != want.CleanupSchedule {
		t.Fatalf("cleanupSchedule = %q, want %q", got.CleanupSchedule, want.CleanupSchedule)
	}
	if got.Version != want.Version {
		t.Fatalf("version = %d, want %d", got.Version, want.Version)
	}
}

// TestGetDefaultsWhenNeverSaved covers the default DTO before any save: mode=all,
// empty serverIds, 7/60/daily, version=0 and a zero updatedAt.
func TestGetDefaultsWhenNeverSaved(t *testing.T) {
	svc, _, _ := newTestService(t)
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	requireConfig(t, cfg, Config{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily", Version: 0})
	if !cfg.UpdatedAt.IsZero() {
		t.Fatalf("updatedAt = %v, want zero", cfg.UpdatedAt)
	}
}

// TestEnsureMigratedSeedsLegacySettings covers a fresh table seeded from the old
// runtime settings: valid values are adopted once, version starts at 1.
func TestEnsureMigratedSeedsLegacySettings(t *testing.T) {
	svc, _, now := newTestService(t)
	insertLegacySetting(t, svc, "metricsRetentionDays", "14")
	insertLegacySetting(t, svc, "metricsCollectionIntervalSeconds", "120")
	insertLegacySetting(t, svc, "cleanupSchedule", "weekly")
	if err := svc.EnsureMigrated(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	requireConfig(t, cfg, Config{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 14, CollectionIntervalSeconds: 120, CleanupSchedule: "weekly", Version: 1})
	if !cfg.UpdatedAt.Equal(*now) {
		t.Fatalf("updatedAt = %v, want %v", cfg.UpdatedAt, *now)
	}

	// 播种是首次写入，随后的用户保存从 2 开始单调递增。
	saved, err := svc.Save(context.Background(), SaveInput{Mode: ModeAll, RetentionDays: 14, CollectionIntervalSeconds: 120, CleanupSchedule: "weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 2 {
		t.Fatalf("version after first save = %d, want 2", saved.Version)
	}
}

// TestEnsureMigratedFallsBackForInvalidLegacySettings covers invalid or missing
// legacy values: each field independently falls back to its default.
func TestEnsureMigratedFallsBackForInvalidLegacySettings(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
	}{
		{name: "invalid", values: map[string]string{
			"metricsRetentionDays":             "0",
			"metricsCollectionIntervalSeconds": "-5",
			"cleanupSchedule":                  "sometimes",
		}},
		{name: "garbage", values: map[string]string{
			"metricsRetentionDays":             "seven",
			"metricsCollectionIntervalSeconds": "",
		}},
		{name: "missing", values: map[string]string{}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService(t)
			for key, value := range tt.values {
				insertLegacySetting(t, svc, key, value)
			}
			if err := svc.EnsureMigrated(context.Background()); err != nil {
				t.Fatal(err)
			}
			cfg, err := svc.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			requireConfig(t, cfg, Config{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily", Version: 1})
		})
	}
}

// TestEnsureMigratedIsOneTime covers the once-only contract: after the row is
// written, later legacy changes are ignored.
func TestEnsureMigratedIsOneTime(t *testing.T) {
	svc, _, _ := newTestService(t)
	insertLegacySetting(t, svc, "metricsRetentionDays", "14")
	if err := svc.EnsureMigrated(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE runtime_settings SET value='30' WHERE key='metricsRetentionDays'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureMigrated(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RetentionDays != 14 || cfg.Version != 1 {
		t.Fatalf("re-migration must not overwrite the seeded row: %#v", cfg)
	}
}

// TestSaveAllModeClearsServerIDsAndBumpsVersion covers mode=all normalization
// (ignored + cleared serverIds) and the monotonic version.
func TestSaveAllModeClearsServerIDsAndBumpsVersion(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	first, err := svc.Save(ctx, SaveInput{Mode: ModeAll, ServerIDs: []string{"srv-b", " srv-a ", "srv-a", ""}, RetentionDays: 5, CollectionIntervalSeconds: 30, CleanupSchedule: "hourly"})
	if err != nil {
		t.Fatal(err)
	}
	requireConfig(t, first, Config{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 5, CollectionIntervalSeconds: 30, CleanupSchedule: "hourly", Version: 1})
	second, err := svc.Save(ctx, SaveInput{Mode: ModeAll, ServerIDs: []string{"srv-c"}, RetentionDays: 5, CollectionIntervalSeconds: 30, CleanupSchedule: "hourly"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 {
		t.Fatalf("version = %d, want 2", second.Version)
	}
	stored, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ServerIDs) != 0 {
		t.Fatalf("all mode must persist an empty serverIds, got %v", stored.ServerIDs)
	}
}

// TestSaveSelectedModeNormalizesServerIDs covers selected mode: trim, dedupe and
// ascending serverID order.
func TestSaveSelectedModeNormalizesServerIDs(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	cfg, err := svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-b", " srv-a ", "srv-b", "srv-c", ""}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"})
	if err != nil {
		t.Fatal(err)
	}
	requireConfig(t, cfg, Config{Mode: ModeSelected, ServerIDs: []string{"srv-a", "srv-b", "srv-c"}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily", Version: 1})
	stored, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	requireConfig(t, stored, cfg)
}

// TestSaveValidationErrors covers every stable validation code and proves a
// failed save writes nothing (no row, no version bump, no partial update).
func TestSaveValidationErrors(t *testing.T) {
	valid := SaveInput{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"}
	cases := []struct {
		name  string
		input SaveInput
		code  string
	}{
		{
			name:  "mode invalid",
			input: SaveInput{Mode: "none", RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"},
			code:  "metrics_facility_mode_invalid",
		},
		{
			name:  "retention zero",
			input: SaveInput{Mode: ModeAll, RetentionDays: 0, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"},
			code:  "metrics_facility_retention_invalid",
		},
		{
			name:  "retention negative",
			input: SaveInput{Mode: ModeAll, RetentionDays: -1, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"},
			code:  "metrics_facility_retention_invalid",
		},
		{
			name:  "interval zero",
			input: SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: 0, CleanupSchedule: "daily"},
			code:  "metrics_facility_interval_invalid",
		},
		{
			name:  "interval negative",
			input: SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: -30, CleanupSchedule: "daily"},
			code:  "metrics_facility_interval_invalid",
		},
		{
			name:  "schedule invalid",
			input: SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "monthly"},
			code:  "metrics_facility_cleanup_schedule_invalid",
		},
		{
			name:  "schedule empty",
			input: SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: ""},
			code:  "metrics_facility_cleanup_schedule_invalid",
		},
		{
			name:  "selected empty",
			input: SaveInput{Mode: ModeSelected, ServerIDs: []string{"", "  "}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"},
			code:  "metrics_facility_servers_required",
		},
		{
			name:  "selected unknown server",
			input: SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-z"}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"},
			code:  "metrics_facility_server_not_found",
		},
	}
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Save(ctx, tt.input); err == nil {
				t.Fatalf("expected error %q", tt.code)
			} else {
				requirePanelCode(t, err, tt.code)
			}
			cfg, err := svc.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			requireConfig(t, cfg, Config{Mode: ModeAll, ServerIDs: []string{}, RetentionDays: 7, CollectionIntervalSeconds: 60, CleanupSchedule: "daily", Version: 0})
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM metrics_facility_configs`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("failed save must not insert a row, count = %d", count)
			}
		})
	}

	// 已存在配置时，失败保存同样不得 bump version 或部分覆盖。
	if _, err := svc.Save(ctx, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, RetentionDays: 0, CollectionIntervalSeconds: 60, CleanupSchedule: "daily"}); err == nil {
		t.Fatal("expected retention validation failure")
	}
	cfg, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || cfg.RetentionDays != 7 {
		t.Fatalf("failed save must keep the previous config: %#v", cfg)
	}
}

// TestReportConfigAllMode covers mode=all: every existing server reports with the
// configured interval; unknown/empty server IDs are disabled.
func TestReportConfigAllMode(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: 90, CleanupSchedule: "daily"}); err != nil {
		t.Fatal(err)
	}
	for _, serverID := range []string{"srv-a", "srv-b", "srv-c"} {
		report, err := svc.ReportConfig(ctx, serverID)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Enabled || report.IntervalSeconds != 90 {
			t.Fatalf("%s report = %#v, want enabled with 90s", serverID, report)
		}
	}
	for _, serverID := range []string{"srv-missing", ""} {
		report, err := svc.ReportConfig(ctx, serverID)
		if err != nil {
			t.Fatal(err)
		}
		if report.Enabled || report.IntervalSeconds != 0 {
			t.Fatalf("%q must be disabled: %#v", serverID, report)
		}
	}
}

// TestReportConfigSelectedMode covers mode=selected: only the listed existing
// servers report; unlisted and deleted servers are disabled with interval 0.
func TestReportConfigSelectedMode(t *testing.T) {
	svc, provider, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeSelected, ServerIDs: []string{"srv-a", "srv-b"}, RetentionDays: 7, CollectionIntervalSeconds: 120, CleanupSchedule: "daily"}); err != nil {
		t.Fatal(err)
	}
	enabled, err := svc.ReportConfig(ctx, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled || enabled.IntervalSeconds != 120 {
		t.Fatalf("srv-a report = %#v", enabled)
	}
	disabled, err := svc.ReportConfig(ctx, "srv-c")
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.IntervalSeconds != 0 {
		t.Fatalf("srv-c report = %#v, want disabled with 0 interval", disabled)
	}

	// 删除已选节点后，即使仍留在配置里也必须禁用。
	provider.items = []server.Server{{ID: "srv-a", Name: "A"}, {ID: "srv-c", Name: "C"}}
	*now = now.Add(snapshotTTL)
	deleted, err := svc.ReportConfig(ctx, "srv-b")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Enabled || deleted.IntervalSeconds != 0 {
		t.Fatalf("deleted server report = %#v, want disabled", deleted)
	}
}

// TestSnapshotsRespectTTLAndInvalidation covers the 3s TTL snapshots used by the
// agent report stream and cleanup worker, plus invalidation after Save.
func TestSnapshotsRespectTTLAndInvalidation(t *testing.T) {
	svc, provider, now := newTestService(t, testServers()...)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, RetentionDays: 7, CollectionIntervalSeconds: 45, CleanupSchedule: "daily"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.IntervalSeconds(); got != 45 {
		t.Fatalf("interval = %d, want 45", got)
	}
	if got := svc.CleanupSettings(); got.RetentionDays != 7 || got.Schedule != "daily" {
		t.Fatalf("cleanup settings = %#v", got)
	}

	// 数据源在 TTL 内变化时复用快照。
	if _, err := svc.db.Exec(`UPDATE metrics_facility_configs SET retention_days=30, collection_interval_seconds=90 WHERE id='metrics'`); err != nil {
		t.Fatal(err)
	}
	if got := svc.IntervalSeconds(); got != 45 {
		t.Fatalf("interval within TTL = %d, want cached 45", got)
	}
	if got := svc.CleanupSettings(); got.RetentionDays != 7 {
		t.Fatalf("retention within TTL = %d, want cached 7", got.RetentionDays)
	}
	// 先预热服务器列表快照，再模拟节点被删除。
	if report, err := svc.ReportConfig(ctx, "srv-b"); err != nil || !report.Enabled || report.IntervalSeconds != 45 {
		t.Fatalf("server report before deletion = %#v %v", report, err)
	}
	provider.items = []server.Server{{ID: "srv-a", Name: "A"}}
	if report, err := svc.ReportConfig(ctx, "srv-b"); err != nil || !report.Enabled {
		t.Fatalf("server snapshot within TTL must keep srv-b enabled: %#v %v", report, err)
	}

	// 超过 TTL 后重新读取。
	*now = now.Add(snapshotTTL)
	if got := svc.IntervalSeconds(); got != 90 {
		t.Fatalf("interval after TTL = %d, want 90", got)
	}
	if got := svc.CleanupSettings(); got.RetentionDays != 30 {
		t.Fatalf("retention after TTL = %d, want 30", got.RetentionDays)
	}
	if report, err := svc.ReportConfig(ctx, "srv-b"); err != nil || report.Enabled {
		t.Fatalf("deleted server must be disabled after TTL: %#v %v", report, err)
	}

	// Save 立即失效快照。
	if _, err := svc.Save(ctx, SaveInput{Mode: ModeAll, RetentionDays: 3, CollectionIntervalSeconds: 30, CleanupSchedule: "weekly"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.IntervalSeconds(); got != 30 {
		t.Fatalf("interval after save = %d, want 30", got)
	}
	settings := svc.CleanupSettings()
	if settings.RetentionDays != 3 || settings.Schedule != "weekly" {
		t.Fatalf("cleanup settings after save = %#v", settings)
	}
}

// TestSnapshotReadFailureFallsBackToDefaults covers the degraded path: snapshot
// reads fail, the report stream and cleanup worker fall back to defaults instead
// of crashing.
func TestSnapshotReadFailureFallsBackToDefaults(t *testing.T) {
	svc, _, _ := newTestService(t, testServers()...)
	if err := svc.db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := svc.IntervalSeconds(); got != DefaultCollectionIntervalSeconds {
		t.Fatalf("interval fallback = %d, want %d", got, DefaultCollectionIntervalSeconds)
	}
	settings := svc.CleanupSettings()
	if settings.RetentionDays != DefaultRetentionDays || settings.Schedule != DefaultCleanupSchedule {
		t.Fatalf("cleanup fallback = %#v", settings)
	}
	if _, err := svc.ReportConfig(context.Background(), "srv-a"); err == nil {
		t.Fatal("ReportConfig must propagate read errors so the caller can degrade explicitly")
	}
	if _, err := svc.Get(context.Background()); err == nil {
		t.Fatal("Get must propagate read errors")
	}
}

// TestTableConstraintsRejectInvalidRows proves the persisted schema enforces the
// mode/cleanup_schedule CHECK constraints from the spec.
func TestTableConstraintsRejectInvalidRows(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.db.Exec(`INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at) VALUES('metrics','everything','[]',7,60,'daily',1,'2026-11-16T12:00:00Z')`); err == nil {
		t.Fatal("mode CHECK constraint must reject unknown modes")
	}
	if _, err := svc.db.Exec(`INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at) VALUES('metrics','all','[]',7,60,'monthly',1,'2026-11-16T12:00:00Z')`); err == nil {
		t.Fatal("cleanup_schedule CHECK constraint must reject unknown schedules")
	}
}
