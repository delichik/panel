package orchestrator

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newOrchestratorTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	statements := []string{
		`CREATE TABLE applications (id TEXT PRIMARY KEY, deletion_requested INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE application_revisions (id TEXT PRIMARY KEY, application_id TEXT NOT NULL, generation INTEGER NOT NULL, spec_hash TEXT NOT NULL, rendered_runtime_spec TEXT NOT NULL DEFAULT '{}', managed_file_manifest TEXT NOT NULL DEFAULT '[]', image_reference TEXT NOT NULL DEFAULT '', resolved_image_digest TEXT NOT NULL DEFAULT '', spec_yaml TEXT NOT NULL DEFAULT '', job_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`,
		`CREATE TABLE application_instances (
			id TEXT PRIMARY KEY, application_id TEXT NOT NULL, server_id TEXT NOT NULL,
			container_name TEXT NOT NULL DEFAULT '', container_id TEXT NOT NULL DEFAULT '',
			desired_state TEXT NOT NULL DEFAULT 'running', desired_generation INTEGER NOT NULL DEFAULT 0,
			desired_spec_hash TEXT NOT NULL DEFAULT '', desired_revision_id TEXT NOT NULL DEFAULT '',
			desired_spec_json TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL DEFAULT 'pending',
			runtime_spec_json TEXT NOT NULL DEFAULT '{}', last_deployed_generation INTEGER NOT NULL DEFAULT 0,
			observed_state TEXT NOT NULL DEFAULT 'unknown', observed_container_name TEXT NOT NULL DEFAULT '',
			observed_container_id TEXT NOT NULL DEFAULT '', observed_generation INTEGER NOT NULL DEFAULT 0,
			observed_spec_hash TEXT NOT NULL DEFAULT '', observed_image_digest TEXT NOT NULL DEFAULT '',
			observed_at TEXT, observed_sequence INTEGER NOT NULL DEFAULT 0, observed_source TEXT NOT NULL DEFAULT '',
			last_reconcile_job_id TEXT NOT NULL DEFAULT '', last_error_code TEXT NOT NULL DEFAULT '',
			last_error_class TEXT NOT NULL DEFAULT '', last_error_message TEXT NOT NULL DEFAULT '',
			last_error_detail TEXT NOT NULL DEFAULT '', last_error_at TEXT, last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE jobs (
			id TEXT PRIMARY KEY, application_id TEXT NOT NULL, server_id TEXT NOT NULL, instance_id TEXT NOT NULL,
			action TEXT NOT NULL, desired_generation INTEGER NOT NULL DEFAULT 0, desired_spec_hash TEXT NOT NULL DEFAULT '',
			desired_revision_id TEXT NOT NULL DEFAULT '', desired_spec_json TEXT NOT NULL DEFAULT '{}',
			remove_data INTEGER NOT NULL DEFAULT 0, force_nonce INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0, next_run_at TEXT,
			lease_owner TEXT NOT NULL DEFAULT '', lease_token TEXT NOT NULL DEFAULT '', lease_expires_at TEXT,
			execution_id TEXT NOT NULL DEFAULT '', intent_id TEXT NOT NULL DEFAULT '', trigger_type TEXT NOT NULL DEFAULT '',
			trigger_resource_type TEXT NOT NULL DEFAULT '', trigger_resource_id TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '', last_stage TEXT NOT NULL DEFAULT '', last_steps_json TEXT NOT NULL DEFAULT '[]',
			error_code TEXT NOT NULL DEFAULT '', error_class TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '',
			error_detail TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT, updated_at TEXT NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX uq_test_application_instances_app_server ON application_instances(application_id,server_id)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX uq_test_application_revisions_app_generation ON application_revisions(application_id,generation)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertOrchestratorTestRows(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO applications(id,deletion_requested) VALUES('app-1',0)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('inst-1','app-1','srv-1','panel-app','running',1,'hash-1','rev-1','{"image":"one"}',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO jobs(id,application_id,server_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,state,created_at,updated_at)
		VALUES('job-1','app-1','srv-1','inst-1','apply',1,'hash-1','rev-1','{"image":"one"}','pending',?,?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRequeueRefreshesCurrentDesiredSnapshot(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	store := NewStore(db).withNow(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })
	ctx := context.Background()
	job, claimed, err := store.Claim(ctx, "job-1", "worker-a", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: job=%#v claimed=%v err=%v", job, claimed, err)
	}
	if _, err := db.Exec(`UPDATE application_instances SET desired_state='stopped',desired_generation=2,desired_spec_hash='hash-2',desired_revision_id='',desired_spec_json='{}' WHERE id='inst-1'`); err != nil {
		t.Fatal(err)
	}
	changed, err := store.DesiredChanged(ctx, job)
	if err != nil || !changed {
		t.Fatalf("DesiredChanged = %v, %v", changed, err)
	}
	ok, err := store.Requeue(ctx, job, "new stop intent")
	if err != nil || !ok {
		t.Fatalf("Requeue = %v, %v", ok, err)
	}
	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != JobPending || got.Action != ActionStop || got.DesiredGeneration != 2 || got.DesiredSpecHash != "hash-2" || got.LeaseToken != "" {
		t.Fatalf("requeued job did not adopt desired snapshot: %#v", got)
	}
}

func TestObservationWriterUsesSequenceAndLeaseFencing(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	writer := NewObservationWriter(db)
	ctx := context.Background()
	first, err := writer.Write(ctx, Observation{InstanceID: "inst-1", Source: "agent_report", Sequence: 2, ObservedAt: time.Unix(2, 0).UTC(), ObservedState: ObservedRunning, ContainerID: "container-2"})
	if err != nil || !first.Accepted {
		t.Fatalf("first observation = %#v, %v", first, err)
	}
	stale, err := writer.Write(ctx, Observation{InstanceID: "inst-1", Source: "agent_report", Sequence: 1, ObservedAt: time.Unix(3, 0).UTC(), ObservedState: ObservedStopped})
	if err != nil || stale.Accepted {
		t.Fatalf("stale observation = %#v, %v", stale, err)
	}
	store := NewStore(db).withNow(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })
	job, claimed, err := store.Claim(ctx, "job-1", "worker-a", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim: %#v %v %v", job, claimed, err)
	}
	wrong, err := writer.Write(ctx, Observation{InstanceID: "inst-1", Source: "reconcile", ObservedAt: time.Unix(4, 0).UTC(), ObservedState: ObservedRunning, JobID: job.ID, LeaseToken: "stale"})
	if err != nil || wrong.Accepted {
		t.Fatalf("wrong lease observation = %#v, %v", wrong, err)
	}
	owned, err := writer.Write(ctx, Observation{InstanceID: "inst-1", Source: "reconcile", ObservedAt: time.Unix(4, 0).UTC(), ObservedState: ObservedRunning, JobID: job.ID, LeaseToken: job.LeaseToken})
	if err != nil || !owned.Accepted {
		t.Fatalf("owned observation = %#v, %v", owned, err)
	}
}

func TestPlannerUpdatesDesiredAndMergesPendingJob(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	planner := NewPlanner(NewStore(db))
	result, err := planner.Plan(context.Background(), PlanInput{
		ApplicationID: "app-1", ServerID: "srv-1", InstanceID: "inst-1", Action: ActionStop,
		DesiredState: DesiredStopped, DesiredGeneration: 2, DesiredSpecHash: "hash-2",
		DesiredSpecJSON: []byte(`{}`), ContainerName: "panel-app", Priority: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Merged || result.Job.Action != ActionStop || result.Job.State != JobPending {
		t.Fatalf("planner did not merge active pending job: %#v", result)
	}
	var desiredState, action string
	if err := db.QueryRow(`SELECT i.desired_state,j.action FROM application_instances i JOIN jobs j ON j.id=? WHERE i.id=?`, result.Job.ID, "inst-1").Scan(&desiredState, &action); err != nil {
		t.Fatal(err)
	}
	if desiredState != DesiredStopped || action != ActionStop {
		t.Fatalf("desired/job state not updated: %s/%s", desiredState, action)
	}
}

func TestEnsureRevisionAndPlanBatchIsAtomic(t *testing.T) {
	db := newOrchestratorTestDB(t)
	_, err := db.Exec(`INSERT INTO applications(id,deletion_requested) VALUES('app-1',0)`)
	if err != nil {
		t.Fatal(err)
	}
	planner := NewPlanner(NewStore(db))
	revision, results, err := planner.EnsureRevisionAndPlanBatch(context.Background(), RevisionInput{
		ApplicationID:       "app-1",
		Generation:          3,
		SpecHash:            "hash-3",
		RenderedRuntimeSpec: []byte(`{"image":"example/app:3"}`),
	}, []PlanInput{{ApplicationID: "app-1", ServerID: "srv-1", DesiredState: DesiredRunning, DesiredGeneration: 3, DesiredSpecHash: "hash-3", DesiredSpecJSON: []byte(`{"image":"example/app:3"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if revision.ID == "" || len(results) != 1 || results[0].Job.DesiredRevisionID != revision.ID {
		t.Fatalf("revision/job linkage missing: revision=%#v results=%#v", revision, results)
	}
	var revisionCount, jobCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM application_revisions WHERE application_id='app-1'`).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE application_id='app-1'`).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 || jobCount != 1 {
		t.Fatalf("atomic planner rows = revisions:%d jobs:%d", revisionCount, jobCount)
	}
}
