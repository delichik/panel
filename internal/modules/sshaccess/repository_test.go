package sshaccess

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	agentcontract "panel/internal/agent/contract"
)

const testSchema = `
CREATE TABLE ssh_access_sessions (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL,
	user_name TEXT NOT NULL DEFAULT '',
	tty TEXT NOT NULL DEFAULT '',
	pid INTEGER NOT NULL DEFAULT 0,
	source_host TEXT NOT NULL DEFAULT '',
	source_address TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT 'ssh',
	login_at TEXT NOT NULL,
	logout_at TEXT NULL,
	logout_origin TEXT NOT NULL DEFAULT '',
	duration_seconds INTEGER NULL,
	last_seen_at TEXT NULL,
	origin TEXT NOT NULL DEFAULT 'live',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE ssh_access_attempts (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL,
	user_name TEXT NOT NULL DEFAULT '',
	source_host TEXT NOT NULL DEFAULT '',
	source_address TEXT NOT NULL DEFAULT '',
	pid INTEGER NOT NULL DEFAULT 0,
	attempt_at TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

func newTestRepository(t *testing.T) *Repository {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(testSchema); err != nil {
		t.Fatal(err)
	}
	return NewRepository(db)
}

func session(user, tty string, pid int, at time.Time) agentcontract.SSHSession {
	return agentcontract.SSHSession{
		UserName: user, TTY: tty, SourceHost: "203.0.113.9", SourceAddress: "203.0.113.9",
		LoginAt: at, PID: pid, Kind: "ssh",
	}
}

func TestUpsertSessionsAndCloseMissing(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	items := []agentcontract.SSHSession{
		session("alice", "pts/0", 100, base),
		session("bob", "pts/1", 101, base.Add(time.Minute)),
	}
	ids, err := repo.UpsertSessions(ctx, "srv-1", items, base.Add(2*time.Minute), "live")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %d, want 2", len(ids))
	}
	// 重复上报是幂等的，且刷新 last_seen_at。
	if _, err := repo.UpsertSessions(ctx, "srv-1", items, base.Add(5*time.Minute), "live"); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListSessions(ctx, "srv-1", SessionFilter{State: "open", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("open total = %d items = %d, want 2", page.Total, len(page.Items))
	}
	for _, item := range page.Items {
		if item.LastSeenAt == nil || !item.LastSeenAt.Equal(base.Add(5*time.Minute)) {
			t.Fatalf("lastSeenAt = %v", item.LastSeenAt)
		}
	}

	closed, err := repo.CloseMissingSessions(ctx, "srv-1", []string{ids[0]}, base.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("closed = %d, want 1", closed)
	}
	page, err = repo.ListSessions(ctx, "srv-1", SessionFilter{State: "all", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	var bob *Session
	for i := range page.Items {
		if page.Items[i].User == "bob" {
			bob = &page.Items[i]
		}
	}
	if bob == nil || bob.LogoutAt == nil || bob.LogoutOrigin != "live" {
		t.Fatalf("bob = %+v", bob)
	}
	if bob.DurationSeconds == nil || *bob.DurationSeconds != 540 {
		t.Fatalf("bob duration = %v, want 540", bob.DurationSeconds)
	}
}

func TestUpsertHistoryBackfillsOnlyMissingLogout(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	live := session("alice", "pts/0", 200, base)
	if _, err := repo.UpsertSessions(ctx, "srv-2", []agentcontract.SSHSession{live}, base.Add(time.Minute), "live"); err != nil {
		t.Fatal(err)
	}
	history := []agentcontract.SSHSessionHistoryItem{{
		SSHSession: live, LogoutAt: base.Add(2 * time.Hour),
	}}
	if err := repo.UpsertHistory(ctx, "srv-2", history); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListSessions(ctx, "srv-2", SessionFilter{State: "closed", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("closed total = %d, want 1", page.Total)
	}
	item := page.Items[0]
	if item.LogoutOrigin != "wtmp" || item.DurationSeconds == nil || *item.DurationSeconds != 7200 {
		t.Fatalf("history item = %+v", item)
	}
	// 已有 logout 不被更晚的回填覆盖。
	history[0].LogoutAt = base.Add(3 * time.Hour)
	if err := repo.UpsertHistory(ctx, "srv-2", history); err != nil {
		t.Fatal(err)
	}
	page, _ = repo.ListSessions(ctx, "srv-2", SessionFilter{State: "closed", PageSize: 10})
	if *page.Items[0].DurationSeconds != 7200 {
		t.Fatalf("duration overwritten: %v", *page.Items[0].DurationSeconds)
	}

	// wtmp 中已关闭的历史会话可直接插入，origin=backfill。
	carol := session("carol", "pts/2", 201, base.Add(-time.Hour))
	if err := repo.UpsertHistory(ctx, "srv-2", []agentcontract.SSHSessionHistoryItem{{
		SSHSession: carol, LogoutAt: base.Add(-30 * time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if page, _ = repo.ListSessions(ctx, "srv-2", SessionFilter{State: "closed", PageSize: 10}); page.Total != 2 {
		t.Fatalf("closed total = %d, want 2", page.Total)
	}
}

func TestInsertAttemptsDeduplicatesAndLists(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	items := []agentcontract.SSHFailure{
		{UserName: "root", SourceHost: "198.51.100.7", SourceAddress: "198.51.100.7", AttemptAt: at, PID: 1},
		{UserName: "root", SourceHost: "198.51.100.7", SourceAddress: "198.51.100.7", AttemptAt: at, PID: 2},
		{UserName: "admin", SourceHost: "198.51.100.8", SourceAddress: "198.51.100.8", AttemptAt: at.Add(time.Minute), PID: 3},
	}
	inserted, err := repo.InsertAttempts(ctx, "srv-3", items)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 3 {
		t.Fatalf("inserted = %d, want 3 (same second different pid kept)", inserted)
	}
	inserted, err = repo.InsertAttempts(ctx, "srv-3", items)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 0 {
		t.Fatalf("duplicate inserted = %d, want 0", inserted)
	}
	page, err := repo.ListAttempts(ctx, "srv-3", AttemptFilter{Source: "198.51.100.7", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("filtered total = %d", page.Total)
	}
	if page.Items[0].AttemptAt.IsZero() || page.Items[0].PID == 0 {
		t.Fatalf("attempt = %+v", page.Items[0])
	}
	page, err = repo.ListAttempts(ctx, "srv-3", AttemptFilter{From: at.Add(30 * time.Second), PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].User != "admin" {
		t.Fatalf("from filter total = %d", page.Total)
	}
}

func TestLatestSinceAndCleanupAndSweepOrphans(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	closedOld := session("old", "pts/0", 1, old)
	if _, err := repo.UpsertSessions(ctx, "srv-4", []agentcontract.SSHSession{closedOld}, old.Add(time.Hour), "backfill"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CloseMissingSessions(ctx, "srv-4", nil, old.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	openOld := session("open", "pts/1", 2, old)
	if _, err := repo.UpsertSessions(ctx, "srv-4", []agentcontract.SSHSession{openOld}, now, "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InsertAttempts(ctx, "srv-4", []agentcontract.SSHFailure{
		{UserName: "x", AttemptAt: old, PID: 1},
		{UserName: "y", AttemptAt: now.Add(-time.Hour), PID: 2},
	}); err != nil {
		t.Fatal(err)
	}
	since, err := repo.LatestSessionSince(ctx, "srv-4")
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(old.Truncate(time.Second)) && since.Before(old) {
		t.Fatalf("latest since = %v", since)
	}
	sessions, attempts, err := repo.Cleanup(ctx, 30, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || attempts != 1 {
		t.Fatalf("cleanup sessions=%d attempts=%d, want 1/1", sessions, attempts)
	}
	// 在线会话永不因保留期被删。
	page, err := repo.ListSessions(ctx, "srv-4", SessionFilter{State: "open", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].User != "open" {
		t.Fatalf("open after cleanup = %+v", page.Items)
	}

	for i := 0; i < 5; i++ {
		_, _ = repo.InsertAttempts(ctx, "srv-5", []agentcontract.SSHFailure{{
			UserName: "flood", AttemptAt: now.Add(-time.Duration(i) * time.Minute), PID: i + 1,
		}})
	}
	if _, deleted, err := repo.Cleanup(ctx, 30, 3, 2); err != nil || deleted == 0 {
		t.Fatalf("cap cleanup err=%v deleted=%d", err, deleted)
	}
	attemptPage, err := repo.ListAttempts(ctx, "srv-5", AttemptFilter{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if attemptPage.Total != 2 {
		t.Fatalf("attempt cap total = %d, want 2", attemptPage.Total)
	}

	if _, err := repo.UpsertSessions(ctx, "srv-6", []agentcontract.SSHSession{session("gone", "pts/9", 7, now)}, now, "live"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.SweepOrphans(ctx, []string{"srv-4", "srv-5"}); err != nil || deleted == 0 {
		t.Fatalf("sweep deleted=%d err=%v", deleted, err)
	}
	page, _ = repo.ListSessions(ctx, "srv-6", SessionFilter{State: "all", PageSize: 10})
	if page.Total != 0 {
		t.Fatalf("orphan sessions remain = %d", page.Total)
	}
}

func TestCleanupBatchedAndAttemptCapExactOrder(t *testing.T) {
	repo := newTestRepository(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	items := make([]agentcontract.SSHFailure, 0, 1150)
	for i := 0; i < 1150; i++ {
		items = append(items, agentcontract.SSHFailure{
			UserName:  "brute",
			AttemptAt: now.Add(-time.Duration(i) * time.Second),
			PID:       i + 1,
		})
	}
	if _, err := repo.InsertAttempts(ctx, "srv-cap", items); err != nil {
		t.Fatal(err)
	}
	_, deleted, err := repo.Cleanup(ctx, 30, 30, 100)
	if err != nil {
		t.Fatalf("Cleanup cap: %v", err)
	}
	if deleted != 1050 {
		t.Fatalf("deleted = %d, want 1050", deleted)
	}
	page, err := repo.ListAttempts(ctx, "srv-cap", AttemptFilter{PageSize: 200})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 100 {
		t.Fatalf("remaining attempts = %d, want 100", page.Total)
	}
	// Newest item (i=0 -> now) must be retained, oldest retained must be i=99.
	if !page.Items[0].AttemptAt.Equal(now) {
		t.Fatalf("newest attempt = %v, want %v", page.Items[0].AttemptAt, now)
	}
	if !page.Items[len(page.Items)-1].AttemptAt.Equal(now.Add(-99 * time.Second)) {
		t.Fatalf("oldest retained = %v, want %v", page.Items[len(page.Items)-1].AttemptAt, now.Add(-99*time.Second))
	}
}
