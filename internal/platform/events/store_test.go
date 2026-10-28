package events_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/events"
	"panel/internal/platform/logging"
)

func openAppStore(t *testing.T) *database.Store {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestLoggingUpsertDoesNotMutateJobsOrEventRuns(t *testing.T) {
	dbStore := openAppStore(t)
	db := dbStore.AppDB()
	ctx := context.Background()
	store := events.NewStore(db)

	const sharedID = "evt-shared-id"
	ts := "2026-01-01T00:00:00Z"
	if _, err := db.ExecContext(ctx, `INSERT INTO event_runs(id, type, status, stage, summary, created_at, quiet)
		VALUES (?, 'server_restart', 'running', '', 'work', ?, 0)`, sharedID, ts); err != nil {
		t.Fatalf("insert event_runs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO event_runs(
		id, type, status, application_id, server_id, action, attempts, lease_token, execution_id, created_at, updated_at
	) VALUES (?, 'application_deploy', 'running', 'app-1', 'srv-1', 'apply', 1, 'lease-token', 'fence-exec', ?, ?)`,
		"job-"+sharedID, ts, ts); err != nil {
		t.Fatalf("insert deploy run: %v", err)
	}

	if err := store.Upsert(ctx, logging.ContextRecord{
		EventID:     sharedID,
		Module:      "test",
		ExecutionID: "log-correlation-not-fence",
		Summary:     "log line",
		State:       "running",
	}); err != nil {
		t.Fatalf("upsert events: %v", err)
	}

	var runStatus, runStage string
	if err := db.QueryRowContext(ctx, `SELECT status, stage FROM event_runs WHERE id=?`, sharedID).Scan(&runStatus, &runStage); err != nil {
		t.Fatalf("read event_runs: %v", err)
	}
	if runStatus != "running" || runStage != "" {
		t.Fatalf("event_runs mutated by logging upsert: status=%q stage=%q", runStatus, runStage)
	}
	var jobState, lease, fence string
	if err := db.QueryRowContext(ctx, `SELECT status, lease_token, execution_id FROM event_runs WHERE id=?`, "job-"+sharedID).Scan(&jobState, &lease, &fence); err != nil {
		t.Fatalf("read deploy run: %v", err)
	}
	if jobState != "running" || lease != "lease-token" || fence != "fence-exec" {
		t.Fatalf("deploy fence mutated by logging upsert: state=%q lease=%q fence=%q", jobState, lease, fence)
	}
}

func TestFinishedDiagnosticStateNotReopenedByLogUpsert(t *testing.T) {
	dbStore := openAppStore(t)
	store := events.NewStore(dbStore.AppDB())
	ctx := context.Background()
	if err := store.Upsert(ctx, logging.ContextRecord{EventID: "fin-1", Module: "test", Summary: "start"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateFinished(ctx, "fin-1", "done", false); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(ctx, logging.ContextRecord{EventID: "fin-1", Module: "test", Summary: "late log", State: "running"}); err != nil {
		t.Fatal(err)
	}
	rec, err := store.Get(ctx, "fin-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != "done" || rec.FinishedAt == "" {
		t.Fatalf("finished diagnostic reopened: %#v", rec)
	}
	if rec.Summary != "late log" {
		t.Fatalf("summary should still update: %#v", rec)
	}
}

func TestContextStoreUpsertAndTouch(t *testing.T) {
	dbStore := openAppStore(t)
	store := events.NewStore(dbStore.AppDB())
	ctx := context.Background()

	err := store.Upsert(ctx, logging.ContextRecord{
		EventID:     "event-1",
		Module:      "test",
		Origin:      events.OriginProcess,
		OperationID: "op-1",
		LogFile:     "panel-20260101.jsonl",
		Summary:     "first",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.Touch(ctx, "event-1", "info", "second"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	rec, err := store.Get(ctx, "event-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.Module != "test" || rec.OperationID != "op-1" {
		t.Fatalf("got %#v", rec)
	}
	if rec.LastSourceSeq != 0 {
		t.Fatalf("last_source_seq = %d want 0 (ACK watermark untouched by Upsert/Touch)", rec.LastSourceSeq)
	}
	if rec.Summary != "second" {
		t.Fatalf("summary = %q", rec.Summary)
	}
	if err := store.SetLastSourceSeq(ctx, "event-1", 7); err != nil {
		t.Fatalf("set seq: %v", err)
	}
	if err := store.Touch(ctx, "event-1", "error", "boom"); err != nil {
		t.Fatalf("touch error: %v", err)
	}
	rec, err = store.Get(ctx, "event-1")
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	if !rec.HadError {
		t.Fatal("expected had_error")
	}
	if rec.LastSourceSeq != 7 {
		t.Fatalf("last_source_seq = %d want 7 after touch", rec.LastSourceSeq)
	}
	if err := store.UpdateFinished(ctx, "event-1", "done", false); err != nil {
		t.Fatalf("finish: %v", err)
	}
	rec, err = store.Get(ctx, "event-1")
	if err != nil {
		t.Fatalf("get3: %v", err)
	}
	if rec.FinishedAt == "" || rec.State != "done" {
		t.Fatalf("finished %#v", rec)
	}
}

func TestReadLinesFiltersByID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel-20260101.jsonl")
	lines := []map[string]any{
		{"ts": "2026-01-01T00:00:00Z", "level": "info", "module": "a", "eventId": "s1", "msg": "one"},
		{"ts": "2026-01-01T00:00:01Z", "level": "info", "module": "a", "eventId": "s2", "msg": "skip"},
		{"ts": "2026-01-01T00:00:02Z", "level": "warn", "module": "a", "streamId": "s1", "msg": "two"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()

	got, err := events.ReadLines(dir, "s1", events.LineFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d lines: %#v", len(got), got)
	}
	if got[0]["msg"] != "one" || got[1]["msg"] != "two" {
		t.Fatalf("msgs = %#v %#v", got[0]["msg"], got[1]["msg"])
	}
}
