package orchestrator

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"panel/internal/platform/activitylog"
)

// ORCH-ACT-003 / ORCH-RETRY-002: a forced startup that fails must keep its
// durable retry budget and deadline when ordinary reports arrive afterward.
func TestAutomaticScanPreservesForcedJob(t *testing.T) {
	for _, state := range []string{JobPending, JobRunning, JobFailedRetryable} {
		t.Run(state, func(t *testing.T) {
			db := newOrchestratorTestDB(t)
			if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','service')`); err != nil {
				t.Fatal(err)
			}
			planner := NewPlanner(NewStore(db))
			ctx := context.Background()
			in := PlanInput{ApplicationID: "app", ServerID: "server", InstanceID: "app-server", IntentID: "forced", Action: ActionApply, DesiredState: DesiredRunning, DesiredGeneration: 1, DesiredSpecHash: "hash", ForceNonce: 42}
			first, err := planner.Plan(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE jobs SET state=?,attempts=3,next_run_at='2099-01-01T00:00:00Z',error_code='container_not_running',error_message='exitCode=1' WHERE id=?`, state, first.Job.ID); err != nil {
				t.Fatal(err)
			}
			in.Automatic, in.ForceNonce, in.IntentID = true, 0, "scan-0"
			var beforeEvents int
			if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&beforeEvents); err != nil {
				t.Fatal(err)
			}
			var original Job
			for i := 0; i < 5; i++ {
				in.IntentID = fmt.Sprintf("scan-%d", i)
				merged, err := planner.Plan(ctx, in)
				if err != nil {
					t.Fatal(err)
				}
				if !merged.Merged || merged.Job.ID != first.Job.ID || merged.Job.IntentID != "forced" || merged.Job.ForceNonce != 42 || merged.Job.Attempts != 3 || merged.Job.ErrorCode != "container_not_running" || merged.Job.NextRunAt == nil || merged.Job.NextRunAt.Year() != 2099 {
					t.Fatalf("automatic scan changed forced retry: %#v", merged)
				}
				if i == 0 {
					original = merged.Job
				} else if !reflect.DeepEqual(original, merged.Job) {
					t.Fatal("repeated scan mutated active Job")
				}
			}
			var afterEvents int
			if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&afterEvents); err != nil {
				t.Fatal(err)
			}
			if afterEvents != beforeEvents {
				t.Fatalf("automatic scans added %d events", afterEvents-beforeEvents)
			}
			// An explicit newer force intent must still be accepted and logged.
			in.ForceNonce, in.IntentID = 43, "new-force"
			forced, err := planner.Plan(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if forced.Job.ForceNonce != 43 {
				t.Fatal("new force request was swallowed")
			}
			if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id='new-force'`).Scan(&afterEvents); err != nil {
				t.Fatal(err)
			}
			if afterEvents == 0 {
				t.Fatal("new force request was not recorded")
			}
		})
	}
}

func TestPlannerSupersessionKeepsOriginalRequestSnapshot(t *testing.T) {
	db := newOrchestratorTestDB(t)
	if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','original name')`); err != nil {
		t.Fatal(err)
	}
	ctx := activitylog.WithActor(context.Background(), activitylog.Actor{Kind: "user", ID: "admin", Name: "operator"})
	planner := NewPlanner(NewStore(db))
	first, err := planner.Plan(ctx, PlanInput{ApplicationID: "app", ServerID: "server", IntentID: "intent-1", Action: ActionApply, DesiredGeneration: 1, DesiredRevisionID: "revision-1", Reason: "first deployment"})
	if err != nil {
		t.Fatal(err)
	}
	var original string
	if err := db.QueryRow(`SELECT resources_json FROM activity_events WHERE operation_id='intent-1' AND event_type='operation.requested'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(original, "original name") {
		t.Fatalf("missing name snapshot: %s", original)
	}
	if _, err := db.Exec(`UPDATE applications SET name='renamed' WHERE id='app'`); err != nil {
		t.Fatal(err)
	}
	second, err := planner.Plan(ctx, PlanInput{ApplicationID: "app", ServerID: "server", IntentID: "intent-2", Action: ActionApply, DesiredGeneration: 2, DesiredRevisionID: "revision-2", Reason: "updated deployment"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Job.ID != second.Job.ID || !second.Merged {
		t.Fatal("test must exercise mutable control job merge")
	}
	var retained string
	var relations int
	if err := db.QueryRow(`SELECT resources_json FROM activity_events WHERE operation_id='intent-1' AND event_type='operation.requested'`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if original != retained {
		t.Fatal("historical resource snapshot changed")
	}
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id='intent-1' AND event_type='intent.superseded' AND json_extract(data_json,'$.relatedOperationId')='intent-2'`).Scan(&relations); err != nil {
		t.Fatal(err)
	}
	if relations != 1 {
		t.Fatalf("missing immutable supersession: %d", relations)
	}
	var initiator string
	if err := db.QueryRow(`SELECT actor_id FROM activity_events WHERE operation_id='intent-2' AND event_type='operation.requested'`).Scan(&initiator); err != nil {
		t.Fatal(err)
	}
	if initiator != "admin" {
		t.Fatalf("request lost authenticated actor: %q", initiator)
	}
}

func TestEquivalentIntentReceivesSharedExecutionResult(t *testing.T) {
	db := newOrchestratorTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','service')`); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	planner := NewPlanner(store)
	input := PlanInput{ApplicationID: "app", ServerID: "server", IntentID: "first", Action: ActionStop, DesiredGeneration: 1}
	if _, err := planner.Plan(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.IntentID = "second"
	planned, err := planner.Plan(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	job, claimed, err := store.Claim(ctx, planned.Job.ID, "worker", 0)
	if err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if _, err := store.Succeed(ctx, job, ReconcileResponse{ObservedState: "stopped"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id='first' AND event_type='execution.shared_result' AND json_extract(data_json,'$.result')='succeeded'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("shared intent must reach a durable result, got %d", n)
	}
}

func TestAutomaticEquivalentPlanPreservesRetryBackoffAndDoesNotAppendIntent(t *testing.T) {
	db := newOrchestratorTestDB(t)
	if _, err := db.Exec(`INSERT INTO applications(id,name) VALUES('app','service')`); err != nil {
		t.Fatal(err)
	}
	planner := NewPlanner(NewStore(db))
	in := PlanInput{Automatic: true, ApplicationID: "app", ServerID: "server", InstanceID: "app-server", IntentID: "first", Action: ActionApply, DesiredState: DesiredRunning, DesiredGeneration: 1, DesiredSpecHash: "hash", TriggerType: "agent_report"}
	first, err := planner.Plan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	nextRun := "2026-09-14T03:00:00Z"
	if _, err := db.Exec(`UPDATE jobs SET state='failed_retryable',attempts=9,next_run_at=?,error_code='start_failed' WHERE id=?`, nextRun, first.Job.ID); err != nil {
		t.Fatal(err)
	}
	in.IntentID = "duplicate"
	merged, err := planner.Plan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !merged.Merged || merged.Job.ID != first.Job.ID {
		t.Fatalf("equivalent reconcile did not reuse job: %#v", merged)
	}
	var attempts, duplicateEvents int
	var state, gotNextRun, errorCode string
	if err := db.QueryRow(`SELECT state,attempts,next_run_at,error_code FROM jobs WHERE id=?`, first.Job.ID).Scan(&state, &attempts, &gotNextRun, &errorCode); err != nil {
		t.Fatal(err)
	}
	if state != JobFailedRetryable || attempts != 9 || gotNextRun != nextRun || errorCode != "start_failed" {
		t.Fatalf("equivalent reconcile reset retry state: state=%s attempts=%d next=%s error=%s", state, attempts, gotNextRun, errorCode)
	}
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id='duplicate'`).Scan(&duplicateEvents); err != nil {
		t.Fatal(err)
	}
	if duplicateEvents != 0 {
		t.Fatalf("equivalent automatic reconcile appended %d duplicate events", duplicateEvents)
	}
}
