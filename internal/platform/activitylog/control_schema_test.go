package activitylog

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
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
		`CREATE TABLE tasks(
			id TEXT PRIMARY KEY,operation_id TEXT NOT NULL DEFAULT '',type TEXT NOT NULL,parent_task_id TEXT NOT NULL DEFAULT '',child_index INTEGER NOT NULL DEFAULT 0,child_count INTEGER NOT NULL DEFAULT 0,
			resource_type TEXT NOT NULL DEFAULT '',resource_id TEXT NOT NULL DEFAULT '',server_id TEXT NOT NULL DEFAULT '',node_id TEXT NOT NULL DEFAULT '',
			trigger_type TEXT NOT NULL DEFAULT '',trigger_resource_type TEXT NOT NULL DEFAULT '',trigger_resource_id TEXT NOT NULL DEFAULT '',trigger_task_id TEXT NOT NULL DEFAULT '',triggered_by TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',stage TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '',error TEXT NOT NULL DEFAULT '',retry_count INTEGER NOT NULL DEFAULT 0,quiet INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE task_steps(
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

func TestInstallControlTriggersReplacesLegacyDefinitions(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER activity_job_shared_result AFTER UPDATE ON jobs BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER activity_observation AFTER UPDATE ON application_instances BEGIN SELECT 1; END`); err != nil {
		t.Fatal(err)
	}
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name, contains string
	}{
		{"activity_job_shared_result", "NEW.state IN ('succeeded','failed','cancelled')"},
		{"activity_observation", "OLD.observed_state<>NEW.observed_state"},
	} {
		var definition string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, check.name).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(definition, check.contains) || strings.Contains(definition, "BEGIN SELECT 1") {
			t.Fatalf("trigger %s was not upgraded: %s", check.name, definition)
		}
	}
}

// TestTaskControlFactsUseQuietLevelForInternalRoutine 固定 TASK-REG-003 的
// Activity 控制触发器语义：quiet 任务（内部例行巡检）的创建、流转、步骤和输出
// 为 debug，失败与 stderr 仍为 error；非 quiet 任务保持 info。
func TestTaskControlFactsUseQuietLevelForInternalRoutine(t *testing.T) {
	db := newControlSchemaTestDB(t)
	ctx := context.Background()
	if err := InstallControlTriggers(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct {
		id    string
		quiet int
		want  string
	}{{"task_quiet", 1, "debug"}, {"task_loud", 0, "info"}} {
		if _, err := db.Exec(`INSERT INTO tasks(id,operation_id,type,status,summary,quiet,created_at) VALUES(?,?,?,?,?,?,?)`,
			task.id, "op_"+task.id, "server_agent_check", "queued", "Checking agent", task.quiet, "now"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE tasks SET status='running' WHERE id=?`, task.id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO task_steps(id,task_id,step,status) VALUES(?,?,?,?)`, "step_"+task.id, task.id, "check", "running"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO task_logs(task_id,time,stream,line) VALUES(?,?,?,?)`, task.id, "now", "system", "line"); err != nil {
			t.Fatal(err)
		}
		for eventType, want := range map[string]string{
			"operation.requested": task.want,
			"execution.started":   task.want,
			"step.started":        task.want,
			"output.chunk":        task.want,
		} {
			var level string
			if err := db.QueryRow(`SELECT level FROM activity_events WHERE run_id=? AND event_type=?`, task.id, eventType).Scan(&level); err != nil {
				t.Fatalf("%s %s: %v", task.id, eventType, err)
			}
			if level != want {
				t.Fatalf("%s %s level = %s, want %s", task.id, eventType, level, want)
			}
		}
		if _, err := db.Exec(`INSERT INTO task_logs(task_id,time,stream,line) VALUES(?,?,?,?)`, task.id, "now", "stderr", "boom"); err != nil {
			t.Fatal(err)
		}
		var stderrLevel string
		if err := db.QueryRow(`SELECT level FROM activity_events WHERE run_id=? AND stream='stderr'`, task.id).Scan(&stderrLevel); err != nil {
			t.Fatal(err)
		}
		if stderrLevel != "error" {
			t.Fatalf("%s stderr output level = %s, want error", task.id, stderrLevel)
		}
		if _, err := db.Exec(`UPDATE tasks SET status='failed',error='unreachable' WHERE id=?`, task.id); err != nil {
			t.Fatal(err)
		}
		var failureLevel string
		if err := db.QueryRow(`SELECT level FROM activity_events WHERE run_id=? AND event_type='execution.finished'`, task.id).Scan(&failureLevel); err != nil {
			t.Fatal(err)
		}
		if failureLevel != "error" {
			t.Fatalf("%s failed execution level = %s, want error", task.id, failureLevel)
		}
	}
}

func TestSharedResultDoesNotFanOutIntermediateTransitions(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','app'); INSERT INTO servers(id,name) VALUES('srv','srv')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,application_id,server_id,instance_id,state,intent_id,created_at,updated_at) VALUES('job','app','srv','inst','pending','current','now','now')`); err != nil {
		t.Fatal(err)
	}
	linked := make([]EventInput, 500)
	for i := range linked {
		linked[i] = EventInput{EventType: "execution.linked", OperationID: "old-" + strconv.Itoa(i), Data: map[string]any{"jobId": "job"}}
	}
	if _, err := Append(context.Background(), db, linked); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE jobs SET state='running',execution_id='exec',attempts=1 WHERE id='job'`); err != nil {
		t.Fatal(err)
	}
	var after, shared int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE event_type='execution.shared_result'`).Scan(&shared); err != nil {
		t.Fatal(err)
	}
	if shared != 0 || after-before != 1 {
		t.Fatalf("intermediate transition appended %d events (%d shared), want O(1) single lifecycle event", after-before, shared)
	}
}

func TestIdenticalReconcileObservationDoesNotAppendEvent(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if err := InstallControlTriggers(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','app'); INSERT INTO servers(id,name) VALUES('srv','srv'); INSERT INTO application_instances(id,application_id,server_id,observed_state,observed_source,observed_generation,observed_spec_hash,observed_container_name,observed_container_id,observed_image_digest) VALUES('inst','app','srv','running','reconcile',1,'hash','container','cid','digest')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE application_instances SET observed_source='reconcile',observed_at='later',observed_sequence=observed_sequence+1 WHERE id='inst'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE event_type='observation.accepted'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("identical reconcile observation appended %d events", count)
	}
	if _, err := db.Exec(`UPDATE application_instances SET observed_container_id='cid-2' WHERE id='inst'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE event_type='observation.accepted'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("changed reconcile observation appended %d events, want 1", count)
	}
}
