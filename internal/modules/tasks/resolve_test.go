package tasks

import (
	"context"
	"panel/internal/platform/activitylog"
	"testing"
	"time"
)

func TestManualVerificationPreservesUncertaintyAndReleasesQueue(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "package_refresh", ResourceType: "server", ResourceID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE event_runs SET status='running' WHERE id=?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FailRunningWithoutExecution(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.FailRunningWithoutExecution(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("repeated orphan scan changed evidence: %d %v", n, err)
	}
	if err := svc.Start(ctx, task.ID); err == nil {
		t.Fatal("unknown execution was restarted")
	}
	if err := svc.Cancel(ctx, task.ID, "cancel"); err == nil {
		t.Fatal("cancel released unresolved conflict")
	}
	if _, err := svc.Retry(ctx, task.ID); err == nil {
		t.Fatal("unknown execution was retried")
	}
	if _, err := svc.ResolveUncertain(ctx, task.ID, "succeeded", "verified on server"); err == nil {
		t.Fatal("unauthenticated verification accepted")
	}
	actorCtx := activitylog.WithActor(ctx, activitylog.Actor{Kind: "user", ID: "admin", Name: "operator"})
	resolved, err := svc.ResolveUncertain(actorCtx, task.ID, "succeeded", "verified process and server state")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusCompleted {
		t.Fatalf("unexpected resolved status: %s", resolved.Status)
	}
	// Product facts go to unified logging; activity_events must not receive verification rows.
	var manual int
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id=? AND event_type='execution.verified'`, task.OperationID).Scan(&manual); err != nil {
		t.Fatal(err)
	}
	if manual != 0 {
		t.Fatalf("execution.verified still written to activity_events: %d", manual)
	}
	if _, err := svc.ResolveUncertain(actorCtx, task.ID, "failed", "changed my mind"); err == nil {
		t.Fatal("verification overwrote a completed fact")
	}
}
