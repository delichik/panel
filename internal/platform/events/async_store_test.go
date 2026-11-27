package events

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"panel/internal/platform/logging"
)

func openAsyncTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE events (
		id TEXT PRIMARY KEY,
		module TEXT NOT NULL,
		level TEXT NOT NULL,
		origin TEXT NOT NULL,
		operation_id TEXT NOT NULL DEFAULT '',
		execution_id TEXT NOT NULL DEFAULT '',
		request_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'running',
		had_error INTEGER NOT NULL DEFAULT 0,
		actor_json TEXT NOT NULL DEFAULT '',
		resources_json TEXT NOT NULL DEFAULT '',
		last_source_seq INTEGER NOT NULL DEFAULT 0,
		log_file TEXT NOT NULL DEFAULT '',
		started_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		finished_at TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAsyncStoreFiltersUnknownTouchAndCoalescesFlush(t *testing.T) {
	db := openAsyncTestDB(t)
	store := NewStore(db)
	async := NewAsyncStore(store)
	t.Cleanup(func() { _ = async.Close() })

	ctx := context.Background()

	// Unknown event ID Touch must be skipped without hitting DB.
	if err := async.Touch(ctx, "evt-unknown", "error", "should be skipped"); err != nil {
		t.Fatal(err)
	}
	if err := async.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(ctx, ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after unknown Touch, got %#v", rows)
	}

	// Upsert + multiple Touch calls coalesce and persist on Flush.
	if err := async.Upsert(ctx, logging.ContextRecord{
		EventID: "evt-1",
		Module:  "deploy",
		Title:   "Deploy web",
		Summary: "starting",
		State:   "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := async.Touch(ctx, "evt-1", "info", "step 1"); err != nil {
		t.Fatal(err)
	}
	if err := async.Touch(ctx, "evt-1", "error", "step 2 failed"); err != nil {
		t.Fatal(err)
	}
	if err := async.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, "evt-1")
	if err != nil {
		t.Fatalf("get evt-1: %v", err)
	}
	if got.Summary != "step 2 failed" || got.Level != "error" || !got.HadError {
		t.Fatalf("unexpected coalesced row: %#v", got)
	}

	// ClearBatched + ResetKnownIDs must forget prior event IDs.
	if err := store.ClearBatched(ctx); err != nil {
		t.Fatal(err)
	}
	async.ResetKnownIDs()
	if err := async.Touch(ctx, "evt-1", "warn", "after reset"); err != nil {
		t.Fatal(err)
	}
	if err := async.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "evt-1"); err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows after ResetKnownIDs, got %v", err)
	}

	// Close must drain buffered Upsert before returning.
	if err := async.Upsert(ctx, logging.ContextRecord{
		EventID: "evt-close",
		Module:  "system",
		Title:   "Shutdown",
		State:   "succeeded",
	}); err != nil {
		t.Fatal(err)
	}
	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = closeCtx
	if err := async.Close(); err != nil {
		t.Fatal(err)
	}
	// Idempotent second Close.
	if err := async.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "evt-close"); err != nil {
		t.Fatalf("expected evt-close flushed on Close, got %v", err)
	}
}
