package activitylog

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newControlSchemaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE applications(id TEXT PRIMARY KEY,name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE servers(id TEXT PRIMARY KEY,name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE event_runs(
			id TEXT PRIMARY KEY,operation_id TEXT NOT NULL DEFAULT '',type TEXT NOT NULL,parent_task_id TEXT NOT NULL DEFAULT '',child_index INTEGER NOT NULL DEFAULT 0,child_count INTEGER NOT NULL DEFAULT 0,
			resource_type TEXT NOT NULL DEFAULT '',resource_id TEXT NOT NULL DEFAULT '',server_id TEXT NOT NULL DEFAULT '',node_id TEXT NOT NULL DEFAULT '',
			trigger_type TEXT NOT NULL DEFAULT '',trigger_resource_type TEXT NOT NULL DEFAULT '',trigger_resource_id TEXT NOT NULL DEFAULT '',trigger_task_id TEXT NOT NULL DEFAULT '',triggered_by TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',stage TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',retry_count INTEGER NOT NULL DEFAULT 0,quiet INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE event_run_steps(
			id TEXT PRIMARY KEY,task_id TEXT NOT NULL,step TEXT NOT NULL,status TEXT NOT NULL,percentage REAL NOT NULL DEFAULT 0,metadata_json TEXT NOT NULL DEFAULT '{}',started_at TEXT,finished_at TEXT,error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE task_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,task_id TEXT NOT NULL,time TEXT NOT NULL,stream TEXT NOT NULL,line TEXT NOT NULL)`,
		`CREATE TABLE jobs(
			id TEXT PRIMARY KEY,application_id TEXT NOT NULL,server_id TEXT NOT NULL,instance_id TEXT NOT NULL,
			action TEXT NOT NULL DEFAULT 'apply',desired_generation INTEGER NOT NULL DEFAULT 0,desired_spec_hash TEXT NOT NULL DEFAULT '',desired_revision_id TEXT NOT NULL DEFAULT '',desired_spec_json TEXT NOT NULL DEFAULT '{}',remove_data INTEGER NOT NULL DEFAULT 0,force_nonce INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'pending',priority INTEGER NOT NULL DEFAULT 0,attempts INTEGER NOT NULL DEFAULT 0,next_run_at TEXT,lease_owner TEXT NOT NULL DEFAULT '',lease_token TEXT NOT NULL DEFAULT '',lease_expires_at TEXT,execution_id TEXT NOT NULL DEFAULT '',intent_id TEXT NOT NULL DEFAULT '',trigger_type TEXT NOT NULL DEFAULT '',trigger_resource_type TEXT NOT NULL DEFAULT '',trigger_resource_id TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',idempotency_key TEXT NOT NULL DEFAULT '',last_stage TEXT NOT NULL DEFAULT '',last_steps_json TEXT NOT NULL DEFAULT '[]',error_code TEXT NOT NULL DEFAULT '',error_class TEXT NOT NULL DEFAULT '',error_message TEXT NOT NULL DEFAULT '',error_detail TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL DEFAULT '',started_at TEXT,finished_at TEXT,updated_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE application_instances(
			id TEXT PRIMARY KEY,application_id TEXT NOT NULL,server_id TEXT NOT NULL,desired_state TEXT NOT NULL DEFAULT 'running',desired_generation INTEGER NOT NULL DEFAULT 0,desired_spec_hash TEXT NOT NULL DEFAULT '',desired_revision_id TEXT NOT NULL DEFAULT '',desired_spec_json TEXT NOT NULL DEFAULT '{}',
			observed_state TEXT NOT NULL DEFAULT 'unknown',observed_container_name TEXT NOT NULL DEFAULT '',observed_container_id TEXT NOT NULL DEFAULT '',observed_generation INTEGER NOT NULL DEFAULT 0,observed_spec_hash TEXT NOT NULL DEFAULT '',observed_image_digest TEXT NOT NULL DEFAULT '',observed_at TEXT,observed_sequence INTEGER NOT NULL DEFAULT 0,observed_source TEXT NOT NULL DEFAULT '',last_reconcile_job_id TEXT NOT NULL DEFAULT '',last_error_code TEXT NOT NULL DEFAULT '',last_error_message TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInstallControlTriggersDropsLegacyDefinitions(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER activity_job_shared_result AFTER UPDATE ON jobs BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER activity_observation AFTER UPDATE ON application_instances BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER activity_task_requested AFTER INSERT ON event_runs BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"activity_job_shared_result", "activity_observation", "activity_task_requested"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name=?`, name).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("trigger %s still present after InstallControlTriggers", name)
		}
	}
}

func TestInstallControlTriggersIdempotentWhenAbsent(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
}

func TestControlTriggersNoLongerWriteActivityEvents(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_runs(id,operation_id,type,status,summary,quiet,created_at) VALUES(?,?,?,?,?,?,?)`,
		"task1", "op1", "server_agent_check", "queued", "Checking agent", 0, "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_runs SET status='running' WHERE id=?`, "task1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("control path wrote %d activity_events rows", count)
	}
}
