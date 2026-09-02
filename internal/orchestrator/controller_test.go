package orchestrator

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"
)

// failingReconciler always returns a retryable failure so the controller's
// retry loop is exercised until it terminates.
type failingReconciler struct {
	calls      atomic.Int64
	retryAfter time.Duration
}

func (f *failingReconciler) Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error) {
	f.calls.Add(1)
	return ReconcileResponse{
		ErrorCode:    "boom",
		ErrorClass:   "runtime",
		ErrorMessage: "boom",
		Retryable:    true,
		RetryAfter:   f.retryAfter,
	}, nil
}

func TestControllerRetryTerminatesAfterMaxAttempts(t *testing.T) {
	db := newOrchestratorTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO applications(id,deletion_requested) VALUES('app-1',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('inst-1','app-1','srv-1','panel-app','running',1,'hash-1','','{"image":"one"}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	// A stop job needs no revision lookup, so the fake reconciler is reached.
	if _, err := db.Exec(`INSERT INTO jobs(id,application_id,server_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,state,created_at,updated_at)
		VALUES('job-1','app-1','srv-1','inst-1','stop',1,'hash-1','','{}','pending',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	rec := &failingReconciler{retryAfter: 20 * time.Millisecond}
	var failedCalls atomic.Int64
	ctrl := NewController(NewStore(db), rec, ControllerConfig{
		Owner:        "test",
		MaxAttempts:  3,
		ScanInterval: 10 * time.Millisecond,
		LeaseTTL:     10 * time.Second,
		WorkerCount:  1,
		OnFailed: func(context.Context, Job, ReconcileResponse) {
			failedCalls.Add(1)
		},
	})
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ctrl.Stop() }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		var state, code string
		if err := db.QueryRow(`SELECT state,error_code FROM jobs WHERE id='job-1'`).Scan(&state, &code); err != nil {
			t.Fatal(err)
		}
		if state == JobFailed {
			if code != "max_attempts_exceeded" {
				t.Fatalf("terminal job error_code = %q, want max_attempts_exceeded", code)
			}
			break
		}
		if state == JobSucceeded {
			t.Fatal("job unexpectedly succeeded")
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not terminate after max attempts, state=%s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := rec.calls.Load(); got != 3 {
		t.Fatalf("reconciler calls = %d, want 3", got)
	}
	if got := failedCalls.Load(); got != 3 {
		t.Fatalf("OnFailed calls = %d, want 3 (two retryable + one terminal)", got)
	}
	// A terminal job must never be re-claimed.
	time.Sleep(100 * time.Millisecond)
	if got := rec.calls.Load(); got != 3 {
		t.Fatalf("reconciler calls after termination = %d, want 3", got)
	}
}

func TestControllerRetryableFailureStaysRetryableBelowMaxAttempts(t *testing.T) {
	db := newOrchestratorTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO applications(id,deletion_requested) VALUES('app-1',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('inst-1','app-1','srv-1','panel-app','running',1,'hash-1','','{"image":"one"}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO jobs(id,application_id,server_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,state,created_at,updated_at)
		VALUES('job-1','app-1','srv-1','inst-1','stop',1,'hash-1','','{}','pending',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	rec := &failingReconciler{retryAfter: time.Second}
	ctrl := NewController(NewStore(db), rec, ControllerConfig{
		Owner:        "test",
		MaxAttempts:  3,
		ScanInterval: 10 * time.Millisecond,
		LeaseTTL:     10 * time.Second,
		WorkerCount:  1,
	})
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ctrl.Stop() }()

	// First failure must stay failed_retryable (attempt 1 < max 3) and schedule
	// a future retry instead of going terminal.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		var nextRun sql.NullString
		if err := db.QueryRow(`SELECT state,next_run_at FROM jobs WHERE id='job-1'`).Scan(&state, &nextRun); err != nil {
			t.Fatal(err)
		}
		if state == JobFailedRetryable {
			if !nextRun.Valid || nextRun.String == "" {
				t.Fatalf("retryable job should persist future next_run_at, got %v", nextRun)
			}
			break
		}
		if state == JobFailed {
			t.Fatal("job terminated before reaching max attempts")
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never entered failed_retryable, state=%s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = ctrl.Stop()
	if got := rec.calls.Load(); got != 1 {
		t.Fatalf("reconciler calls before max attempts = %d, want 1", got)
	}
}
