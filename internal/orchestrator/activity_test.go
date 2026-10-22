package orchestrator

import (
	"context"
	"fmt"
	"reflect"
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
			in.ForceNonce, in.IntentID = 43, "new-force"
			forced, err := planner.Plan(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if forced.Job.ForceNonce != 43 {
				t.Fatal("new force request was swallowed")
			}
			var events int
			if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 0 {
				t.Fatalf("planner must not write activity_events, got %d", events)
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
	if second.Job.IntentID != "intent-2" {
		t.Fatalf("merged job intent = %s, want intent-2", second.Job.IntentID)
	}
	var events int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("planner must not write activity_events, got %d", events)
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
	got, err := store.GetJob(ctx, planned.Job.ID)
	if err != nil || got.State != JobSucceeded {
		t.Fatalf("shared execution result: %#v %v", got, err)
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
