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
	if _, err := svc.db.Exec(`UPDATE tasks SET status='running' WHERE id=?`, task.ID); err != nil {
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
	var uncertain, manual int
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id=? AND event_type='uncertainty.detected'`, task.OperationID).Scan(&uncertain); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id=? AND event_type='execution.verified' AND json_extract(data_json,'$.verificationSource')='manual' AND actor_id='admin'`, task.OperationID).Scan(&manual); err != nil {
		t.Fatal(err)
	}
	var gaps, resolvedGaps int
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id=? AND event_type='evidence.gap_detected' AND json_extract(data_json,'$.reason')='source_process_lost'`, task.OperationID).Scan(&gaps); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE operation_id=? AND event_type='evidence.gap_resolved'`, task.OperationID).Scan(&resolvedGaps); err != nil {
		t.Fatal(err)
	}
	if gaps != 1 || resolvedGaps != 0 {
		t.Fatalf("manual result falsely repaired output gap: gaps=%d resolved=%d", gaps, resolvedGaps)
	}
	if uncertain != 1 || manual != 1 {
		t.Fatalf("history changed: uncertainty=%d manual=%d", uncertain, manual)
	}
	if _, err := svc.ResolveUncertain(actorCtx, task.ID, "failed", "changed my mind"); err == nil {
		t.Fatal("verification overwrote a completed fact")
	}
}
