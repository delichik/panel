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

func TestCleanupInterval(t *testing.T) {
	tests := map[string]time.Duration{
		"hourly": time.Hour,
		"daily":  24 * time.Hour,
		"weekly": 7 * 24 * time.Hour,
	}
	for schedule, want := range tests {
		if got := cleanupInterval(schedule); got != want {
			t.Fatalf("cleanupInterval(%q) = %s, want %s", schedule, got, want)
		}
	}
}

// TestCleanupDue 验证清理计划（来自指标设施的 cleanupSchedule）决定清理周期。
func TestCleanupDue(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		lastRun  time.Time
		schedule string
		want     bool
	}{
		{"hourly not due before one hour", now.Add(-59 * time.Minute), "hourly", false},
		{"hourly due at one hour", now.Add(-time.Hour), "hourly", true},
		{"daily due at one day", now.Add(-24 * time.Hour), "daily", true},
		{"weekly not due before a week", now.Add(-6 * 24 * time.Hour), "weekly", false},
		{"weekly due at a week", now.Add(-7 * 24 * time.Hour), "weekly", true},
		{"unknown schedule falls back to daily", now.Add(-23 * time.Hour), "sometimes", false},
		{"unknown schedule due after a day", now.Add(-24 * time.Hour), "sometimes", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanupDue(tc.lastRun, now, tc.schedule); got != tc.want {
				t.Fatalf("cleanupDue(%s, %s, %q) = %v, want %v", tc.lastRun, now, tc.schedule, got, tc.want)
			}
		})
	}
}

// TestCleanupWorkerUsesFacilityProviderSettings 验证清理 worker 的保留天数与清理
// 计划全部来自 provider（装配时接到指标设施 CleanupSettings），且存在周期按设施
// 的 cleanupSchedule 节流——不是每 tick 都清库。
func TestCleanupWorkerUsesFacilityProviderSettings(t *testing.T) {
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
	t.Cleanup(func() { _ = store.Close() })
	svc := NewService(store.MetricsDB(), nil)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, sample := range []struct {
		serverID string
		at       time.Time
	}{
		{"srv-old", now.Add(-5 * 24 * time.Hour)},
		{"srv-new", now.Add(-2 * 24 * time.Hour)},
	} {
		if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: sample.serverID, Time: sample.at, CPUUsagePercent: 1}); err != nil {
			t.Fatal(err)
		}
	}

	providerReads := 0
	worker := NewCleanupWorker(svc, func() CleanupSettings {
		providerReads++
		// 设施配置：保留 3 天、每天清理一次（与 runtime settings 的旧默认值无关）。
		return CleanupSettings{RetentionDays: 3, Schedule: "daily"}
	})
	lastRun := now.Add(-25 * time.Hour)
	next, ran := worker.tick(ctx, lastRun, now)
	if !ran {
		t.Fatal("a daily cleanup must be due after 25 hours")
	}
	if !next.Equal(now) {
		t.Fatalf("lastRun = %s, want the tick time %s", next, now)
	}
	if providerReads != 1 {
		t.Fatalf("provider reads = %d, want 1", providerReads)
	}
	var oldCount, newCount int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-old'`).Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-new'`).Scan(&newCount); err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Fatal("rows older than the facility retention must be deleted")
	}
	if newCount != 1 {
		t.Fatal("rows inside the facility retention must be kept")
	}

	// 第二次 tick 未到清理周期：不得再次清库，但 provider 仍按 tick 读取。
	lastRun, ran = worker.tick(ctx, next, now.Add(time.Hour))
	if ran {
		t.Fatal("cleanup must not run again before the daily interval elapses")
	}
	if providerReads != 2 {
		t.Fatalf("provider reads = %d, want 2", providerReads)
	}
}

// TestCleanupWorkerTickWithoutServiceIsNoop 验证未装配 service/provider 时 tick
// 安全返回，不会 panic（Start 本身也有同样的保护）。
func TestCleanupWorkerTickWithoutServiceIsNoop(t *testing.T) {
	now := time.Now().UTC()
	for name, worker := range map[string]*CleanupWorker{
		"nil worker":   nil,
		"nil service":  NewCleanupWorker(nil, func() CleanupSettings { return CleanupSettings{RetentionDays: 7, Schedule: "daily"} }),
		"nil settings": NewCleanupWorker(&Service{}, nil),
	} {
		t.Run(name, func(t *testing.T) {
			if next, ran := worker.tick(context.Background(), now.Add(-48*time.Hour), now); ran || !next.Equal(now.Add(-48*time.Hour)) {
				t.Fatalf("tick() = (%s, %v), want unchanged lastRun and no run", next, ran)
			}
		})
	}
}
