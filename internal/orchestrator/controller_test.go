package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// failingReconciler always returns a retryable failure so the controller's
// retry loop is exercised until it terminates.
type failingReconciler struct {
	calls      atomic.Int64
	retryAfter time.Duration
}

// ORCH-CTRL-001/002: persistent controller errors are structured and rate limited.
func TestControllerDiagnosticsAreStructuredAndRateLimited(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()

	ctrl := NewController(nil, nil, ControllerConfig{Owner: "test-owner"})
	err := errors.New("database unavailable")
	ctrl.logControllerError("process_job", "job-1", err)
	ctrl.logControllerError("process_job", "job-1", err)
	ctrl.logControllerError("renew_lease", "job-1", err)

	entries := logs.FilterMessage("orchestrator controller operation failed").All()
	if len(entries) != 2 {
		t.Fatalf("diagnostic entries = %d, want one per stage inside suppression window", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["component"] != "orchestrator_controller" || fields["stage"] != "process_job" || fields["owner"] != "test-owner" || fields["job_id"] != "job-1" {
		t.Fatalf("diagnostic fields = %#v", fields)
	}
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

type uncertainReconciler struct {
	calls    int
	finished bool
}

func (r *uncertainReconciler) Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error) {
	r.calls++
	return ReconcileResponse{}, errors.New("response lost after remote side effect")
}
func (r *uncertainReconciler) ResolveExecution(context.Context, ReconcileRequestRPC) (ReconcileResponse, bool, error) {
	return ReconcileResponse{ObservedState: "stopped", ObservedGeneration: 1, ObservedSpecHash: "hash-1"}, r.finished, nil
}

func TestUncertainExecutionIsResolvedWithoutRepeatingSideEffect(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	if _, err := db.Exec(`UPDATE jobs SET action='stop',desired_revision_id='' WHERE id='job-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE application_instances SET desired_state='stopped',desired_revision_id='' WHERE id='inst-1'`); err != nil {
		t.Fatal(err)
	}
	runtime := &uncertainReconciler{}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{})
	ctx := context.Background()
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if before.State != JobRunning || before.ErrorClass != "uncertainty" {
		t.Fatalf("missing uncertainty: %#v", before)
	}
	if _, claimed, err := store.Claim(ctx, "job-1", "other", time.Minute); err != nil || claimed {
		t.Fatalf("unknown work must not be reclaimed: %v %v", claimed, err)
	}
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	runtime.finished = true
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.calls != 1 || after.State != JobSucceeded || after.ExecutionID != before.ExecutionID {
		t.Fatalf("resolution repeated or changed execution: calls=%d job=%#v", runtime.calls, after)
	}
	var facts int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE event_type='uncertainty.detected'`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if facts != 1 {
		t.Fatalf("uncertainty fact lost: %d", facts)
	}
}
