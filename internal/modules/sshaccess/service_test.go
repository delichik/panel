package sshaccess

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	agentcontract "panel/internal/agent/contract"
	server "panel/internal/modules/servers"
	panelerr "panel/internal/platform/errors"
)

type fakeServers struct {
	items []server.Server
}

func (f *fakeServers) Get(_ context.Context, serverID string) (server.Server, error) {
	for _, item := range f.items {
		if item.ID == serverID {
			return item, nil
		}
	}
	return server.Server{}, panelerr.NotFound("server")
}

func (f *fakeServers) List(context.Context) ([]server.Server, error) {
	return append([]server.Server(nil), f.items...), nil
}

type fakeRetention struct {
	success int
	failure int
}

func (f fakeRetention) SSHAccessRetention() (int, int) { return f.success, f.failure }

func newTestService(t *testing.T) (*Service, *fakeServers) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(testSchema); err != nil {
		t.Fatal(err)
	}
	servers := &fakeServers{items: []server.Server{{ID: "srv-1"}, {ID: "srv-2"}}}
	svc := NewService(db, servers, fakeRetention{success: 30, failure: 3})
	return svc, servers
}

func TestServiceApplyAndList(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	live := agentcontract.SSHSnapshot{
		SampledAt: base.Add(time.Minute),
		Items: []agentcontract.SSHSession{
			{UserName: "alice", TTY: "pts/0", SourceHost: "h1", SourceAddress: "203.0.113.5", LoginAt: base, PID: 10, Kind: "ssh"},
			{UserName: "bob", TTY: "pts/1", SourceHost: "h2", SourceAddress: "203.0.113.6", LoginAt: base, PID: 11, Kind: "ssh"},
		},
	}
	if err := svc.ApplyLive(ctx, "srv-1", live); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyHistory(ctx, "srv-1", agentcontract.SSHSessionHistory{
		Items: []agentcontract.SSHSessionHistoryItem{{SSHSession: live.Items[0], LogoutAt: base.Add(time.Hour)}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyFailures(ctx, "srv-1", agentcontract.SSHFailures{
		Items: []agentcontract.SSHFailure{{UserName: "root", SourceAddress: "198.51.100.1", AttemptAt: base, PID: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	closed, err := svc.ListSessions(ctx, "srv-1", SessionFilter{State: "closed"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Total != 1 || closed.Items[0].LogoutOrigin != "wtmp" {
		t.Fatalf("closed = %+v", closed)
	}
	if closed.Coverage.State != "live" || closed.Coverage.LastLiveAt == nil {
		t.Fatalf("coverage = %+v", closed.Coverage)
	}
	open, err := svc.ListSessions(ctx, "srv-1", SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if open.Total != 2 || open.Page != 1 || open.PageSize != 50 {
		t.Fatalf("open = %+v", open)
	}
	attempts, err := svc.ListAttempts(ctx, "srv-1", AttemptFilter{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Total != 1 || attempts.Items[0].User != "root" {
		t.Fatalf("attempts = %+v", attempts)
	}

	if _, err := svc.ListSessions(ctx, "missing", SessionFilter{}); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := svc.ListSessions(ctx, "srv-1", SessionFilter{PageSize: 201}); err == nil {
		t.Fatal("expected page size validation")
	}
	if _, err := svc.ListSessions(ctx, "srv-1", SessionFilter{State: "bogus"}); err == nil {
		t.Fatal("expected state validation")
	}
}

func TestServiceBackfillSinceAndCoverage(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	since, err := svc.BackfillSince(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(since); d < 6*24*time.Hour || d > 8*24*time.Hour {
		t.Fatalf("first backfill since = %v (%v ago)", since, d)
	}
	login := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	if err := svc.ApplyLive(ctx, "srv-1", agentcontract.SSHSnapshot{
		SampledAt: time.Now().UTC(),
		Items:     []agentcontract.SSHSession{{UserName: "a", TTY: "pts/0", LoginAt: login, PID: 1, Kind: "ssh"}},
	}); err != nil {
		t.Fatal(err)
	}
	since, err = svc.BackfillSince(ctx, "srv-1")
	if err != nil {
		t.Fatal(err)
	}
	if !since.Equal(login.Add(-time.Hour)) {
		t.Fatalf("since = %v, want %v", since, login.Add(-time.Hour))
	}
	svc.coverage["srv-2"] = time.Now().UTC().Add(-5 * time.Minute)
	list, err := svc.ListSessions(ctx, "srv-2", SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Coverage.State != "stale" {
		t.Fatalf("coverage = %+v", list.Coverage)
	}
	if list.Total != 0 || list.Items == nil {
		t.Fatalf("empty list = %+v", list)
	}
}

func TestServiceCleanupUsesConfiguredRetention(t *testing.T) {
	svc, _ := newTestService(t)
	svc.retention = fakeRetention{success: 1, failure: 1}
	ctx := context.Background()
	old := time.Now().UTC().Add(-3 * 24 * time.Hour)
	item := agentcontract.SSHSession{UserName: "old", TTY: "pts/0", LoginAt: old, PID: 1, Kind: "ssh"}
	if _, err := svc.repo.UpsertSessions(ctx, "srv-1", []agentcontract.SSHSession{item}, old.Add(time.Hour), "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.CloseMissingSessions(ctx, "srv-1", nil, old.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.InsertAttempts(ctx, "srv-1", []agentcontract.SSHFailure{{UserName: "x", AttemptAt: old, PID: 1}}); err != nil {
		t.Fatal(err)
	}
	svc.cleanup(ctx)
	list, err := svc.ListSessions(ctx, "srv-1", SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 0 {
		t.Fatalf("sessions not cleaned: %+v", list)
	}
	attempts, err := svc.ListAttempts(ctx, "srv-1", AttemptFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Total != 0 {
		t.Fatalf("attempts not cleaned: %+v", attempts)
	}
}
