package orchestrator

import (
	"context"
	"strings"
	"testing"

	"panel/internal/platform/activitylog"
)

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
