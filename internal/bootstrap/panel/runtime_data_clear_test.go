package panel

import (
	"context"
	"database/sql"
	"testing"

	"panel/internal/platform/activitylog"

	_ "modernc.org/sqlite"
)

func TestClearTableBatchedCommitsEachBatch(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE records(id INTEGER PRIMARY KEY); CREATE TRIGGER stop_third_batch BEFORE DELETE ON records WHEN OLD.id=2001 BEGIN SELECT RAISE(ABORT,'stop'); END`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO records(id) VALUES(?)`)
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2505; id++ {
		if _, err = stmt.Exec(id); err != nil {
			t.Fatal(err)
		}
	}
	_ = stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err = clearTableBatched(context.Background(), db, "records", 1000); err == nil {
		t.Fatal("expected the third batch to fail")
	}
	var remaining int
	if err = db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 505 {
		t.Fatalf("completed batches must remain committed: remaining=%d", remaining)
	}
	if _, err = db.Exec(`DROP TRIGGER stop_third_batch`); err != nil {
		t.Fatal(err)
	}
	if err = clearTableBatched(context.Background(), db, "records", 1000); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("records not cleared: remaining=%d err=%v", remaining, err)
	}
}

// DIAG-CLR-002/003: clear derived failures and stale references together with
// their activity history, preserving configuration, revisions and observations.
func TestClearAppCoordinationRemovesPlanningFailurePreservesConfiguration(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE applications(id TEXT PRIMARY KEY,name TEXT,version INTEGER,generation INTEGER,spec_yaml TEXT,job_id TEXT,last_deployment_id TEXT,last_error TEXT,planning_error_json TEXT,updated_at TEXT)`,
		`INSERT INTO applications VALUES('app','saved-name',7,9,'name: saved-name','job','deployment','legacy failure','{"code":"application_invalid","operationId":"old-operation"}','unchanged')`,
		`CREATE TABLE application_instances(id TEXT PRIMARY KEY,desired_state TEXT,observed_state TEXT,last_reconcile_job_id TEXT,last_error_code TEXT,last_error_class TEXT,last_error_message TEXT,last_error_detail TEXT,last_error TEXT,last_error_at TEXT)`,
		`INSERT INTO application_instances VALUES('instance','running','stopped','job','error','runtime','failure','detail','legacy error','2026-09-14T00:00:00Z')`,
		`CREATE TABLE application_revisions(id TEXT PRIMARY KEY)`,
		`INSERT INTO application_revisions VALUES('revision')`,
		`CREATE TABLE jobs(id TEXT PRIMARY KEY)`,
		`INSERT INTO jobs VALUES('job')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := activitylog.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := activitylog.Append(context.Background(), db, []activitylog.EventInput{{EventType: "operation.failed", OperationID: "old-operation", Text: "failure"}}); err != nil {
		t.Fatal(err)
	}
	if err := clearAppCoordination(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var version, generation, revisions, jobs, events int
	var name, spec, updated, planning, jobID string
	if err := db.QueryRow(`SELECT name,version,generation,spec_yaml,updated_at,planning_error_json,job_id FROM applications WHERE id='app'`).Scan(&name, &version, &generation, &spec, &updated, &planning, &jobID); err != nil {
		t.Fatal(err)
	}
	if name != "saved-name" || version != 7 || generation != 9 || spec != "name: saved-name" || updated != "unchanged" || planning != "" || jobID != "" {
		t.Fatalf("configuration changed or diagnostic survived: %s %d %d %s %s %s %s", name, version, generation, spec, updated, planning, jobID)
	}
	var desired, observed, lastJob string
	if err := db.QueryRow(`SELECT desired_state,observed_state,last_reconcile_job_id FROM application_instances`).Scan(&desired, &observed, &lastJob); err != nil {
		t.Fatal(err)
	}
	if desired != "running" || observed != "stopped" || lastJob != "" {
		t.Fatal("runtime state changed or stale job survived")
	}
	var remainingErrors int
	if err := db.QueryRow(`SELECT count(*) FROM application_instances WHERE last_error<>'' OR last_error_at IS NOT NULL`).Scan(&remainingErrors); err != nil || remainingErrors != 0 {
		t.Fatalf("legacy instance diagnostics retained: %d %v", remainingErrors, err)
	}
	for _, item := range []struct {
		q   string
		out *int
	}{{`SELECT COUNT(*) FROM application_revisions`, &revisions}, {`SELECT COUNT(*) FROM jobs`, &jobs}, {`SELECT COUNT(*) FROM activity_events`, &events}} {
		if err := db.QueryRow(item.q).Scan(item.out); err != nil {
			t.Fatal(err)
		}
	}
	if revisions != 1 || jobs != 0 || events != 0 {
		t.Fatalf("unexpected retained data: revisions=%d jobs=%d events=%d", revisions, jobs, events)
	}
}
