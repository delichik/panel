package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// failingReconciler always returns a retryable failure so the controller's
// retry loop is exercised until it terminates.
type failingReconciler struct {
	calls      atomic.Int64
	retryAfter time.Duration
}

type structuredErrorReconciler struct{}

func (*structuredErrorReconciler) Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error) {
	return ReconcileResponse{
		ErrorCode:    "agent_health_failed",
		ErrorClass:   "agent_unavailable",
		ErrorMessage: "agent connection refused",
		ErrorDetail:  "health preflight did not start remote execution",
		Retryable:    true,
	}, errors.New("agent connection refused")
}

func TestRunJobMissingStoreReturnsError(t *testing.T) {
	ctrl := NewController(nil, nil, ControllerConfig{Owner: "test-owner"})
	err := ctrl.RunJob(context.Background(), "missing-job")
	if err == nil {
		t.Fatal("expected error for unavailable store")
	}
}

func TestRunJobMissingJobReturnsError(t *testing.T) {
	db := newOrchestratorTestDB(t)
	ctrl := NewController(NewStore(db), nil, ControllerConfig{Owner: "test-owner"})
	err := ctrl.RunJob(context.Background(), "missing-job")
	if err == nil {
		t.Fatal("RunJob(missing job) returned nil, want error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RunJob(missing job) error = %v, want sql.ErrNoRows", err)
	}
}

func TestExternalScanWakeForwardsToUnifiedWorker(t *testing.T) {
	ctrl := NewController(nil, nil, ControllerConfig{Owner: "test-owner"})
	var wakes atomic.Int64
	ctrl.SetExternalScan(func() { wakes.Add(1) })
	ctrl.Wake()
	ctrl.Wake()
	if got := wakes.Load(); got != 2 {
		t.Fatalf("expected external wake callbacks, got %d", got)
	}
	ctrl.SetExternalScan(nil)
	ctrl.Wake() // internal wake chan; must not panic
	if got := wakes.Load(); got != 2 {
		t.Fatalf("cleared external wake must not fire, got %d", got)
	}
}

func TestJobRunnerDispatchesInsteadOfDirectRunJob(t *testing.T) {
	db := newOrchestratorTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO applications(id,deletion_requested) VALUES('app-1',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,desired_state,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('inst-1','app-1','srv-1','panel-app','running',1,'hash-1','','{}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO event_runs(id,operation_id,type,status,application_id,server_id,node_id,resource_type,resource_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('job-1','job-1','application_deploy','queued','app-1','srv-1','srv-1','application','app-1','inst-1','stop',1,'hash-1','','{}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}

	var runnerCalls atomic.Int64
	var reconcileCalls atomic.Int64
	ctrl := NewController(NewStore(db), &countingReconciler{calls: &reconcileCalls}, ControllerConfig{
		Owner:        "test",
		ScanInterval: 10 * time.Millisecond,
		WorkerCount:  1,
		LeaseTTL:     time.Minute,
	})
	ctrl.SetExternalScan(func() {})
	ctrl.SetJobRunner(func(ctx context.Context, jobID string) error {
		runnerCalls.Add(1)
		return ctrl.RunJob(ctx, jobID)
	})
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ctrl.Stop() }()

	deadline := time.Now().Add(5 * time.Second)
	for runnerCalls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("JobRunner was never invoked for due deploy run")
		}
		_, _ = ctrl.Poll(context.Background())
		time.Sleep(10 * time.Millisecond)
	}
	if got := runnerCalls.Load(); got < 1 {
		t.Fatalf("JobRunner calls = %d, want >= 1", got)
	}
	if got := reconcileCalls.Load(); got < 1 {
		t.Fatalf("reconcile via JobRunner→RunJob calls = %d, want >= 1", got)
	}
}

type countingReconciler struct {
	calls *atomic.Int64
}

func (c *countingReconciler) Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error) {
	c.calls.Add(1)
	return ReconcileResponse{ObservedState: "stopped", ObservedGeneration: 1, ObservedSpecHash: "hash-1"}, nil
}

// ORCH-CTRL-001/002: persistent controller errors are structured and rate limited.
func TestControllerDiagnosticsAreStructuredAndRateLimited(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	restore := logging.SwapStdoutForTest(zap.New(core))
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
	if _, err := db.Exec(`INSERT INTO event_runs(id,operation_id,type,status,application_id,server_id,node_id,resource_type,resource_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('job-1','job-1','application_deploy','queued','app-1','srv-1','srv-1','application','app-1','inst-1','stop',1,'hash-1','','{}',?,?)`, now, now); err != nil {
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
		var state, code, message, detail string
		if err := db.QueryRow(`SELECT `+jobStateSQL+`,error_code,error_message,error_detail FROM event_runs WHERE type='application_deploy' AND id='job-1'`).Scan(&state, &code, &message, &detail); err != nil {
			t.Fatal(err)
		}
		if state == JobFailed {
			if code != "max_attempts_exceeded" {
				t.Fatalf("terminal job error_code = %q, want max_attempts_exceeded", code)
			}
			if message != "boom" || !strings.Contains(detail, "last error code=boom") {
				t.Fatalf("terminal job lost root failure: message=%q detail=%q", message, detail)
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

func TestStructuredReconcileErrorUsesRetryPolicyInsteadOfUncertainty(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	if _, err := db.Exec(`UPDATE event_runs SET action='stop',desired_revision_id='' WHERE id='job-1'`); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	ctrl := NewController(store, &structuredErrorReconciler{}, ControllerConfig{Owner: "test", MaxAttempts: 10})
	if err := ctrl.process(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != JobFailedRetryable || job.ErrorClass != "agent_unavailable" || job.ErrorMessage != "agent connection refused" || job.NextRunAt == nil {
		t.Fatalf("structured failure was not scheduled normally: %#v", job)
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
	if _, err := db.Exec(`INSERT INTO event_runs(id,operation_id,type,status,application_id,server_id,node_id,resource_type,resource_id,instance_id,action,desired_generation,desired_spec_hash,desired_revision_id,desired_spec_json,created_at,updated_at)
		VALUES('job-1','job-1','application_deploy','queued','app-1','srv-1','srv-1','application','app-1','inst-1','stop',1,'hash-1','','{}',?,?)`, now, now); err != nil {
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
		if err := db.QueryRow(`SELECT `+jobStateSQL+`,next_run_at FROM event_runs WHERE type='application_deploy' AND id='job-1'`).Scan(&state, &nextRun); err != nil {
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
	if _, err := db.Exec(`UPDATE event_runs SET action='stop',desired_revision_id='' WHERE id='job-1'`); err != nil {
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
	if after.ErrorClass != "uncertainty" && after.State != JobSucceeded {
		t.Fatalf("unexpected post-resolution job: %#v", after)
	}
}

type slowReconciler struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (r *slowReconciler) Reconcile(ctx context.Context, _ ReconcileRequestRPC) (ReconcileResponse, error) {
	r.calls++
	close(r.started)
	select {
	case <-r.release:
		return ReconcileResponse{ObservedState: "stopped", ObservedGeneration: 1, ObservedSpecHash: "hash-1"}, nil
	case <-ctx.Done():
		// WithoutCancel parent ignores cancel; still honor if ever cancelled.
		return ReconcileResponse{}, ctx.Err()
	}
}

func (r *slowReconciler) ResolveExecution(context.Context, ReconcileRequestRPC) (ReconcileResponse, bool, error) {
	return ReconcileResponse{ObservedState: "stopped", ObservedGeneration: 1, ObservedSpecHash: "hash-1"}, true, nil
}

func TestDispatchWaitReleasesWorkerIntoMonitorWithoutSecondKick(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	if _, err := db.Exec(`UPDATE event_runs SET action='stop',desired_revision_id='' WHERE id='job-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE application_instances SET desired_state='stopped',desired_revision_id='' WHERE id='inst-1'`); err != nil {
		t.Fatal(err)
	}
	runtime := &slowReconciler{started: make(chan struct{}), release: make(chan struct{})}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{DispatchWait: 30 * time.Millisecond})
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- ctrl.process(ctx, "job-1") }()
	select {
	case <-runtime.started:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile was not started")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not release after dispatch wait")
	}
	job, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.State != JobRunning || job.ErrorClass != "uncertainty" || job.ErrorCode != "execution_monitoring" {
		t.Fatalf("expected monitor after dispatch: %#v", job)
	}
	if _, claimed, err := store.Claim(ctx, "job-1", "other", time.Minute); err != nil || claimed {
		t.Fatalf("dispatch monitor must keep fence: %v %v", claimed, err)
	}
	close(runtime.release)
	// Allow background RPC to finish; settlement stays on ResolveExecution.
	time.Sleep(20 * time.Millisecond)
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobSucceeded || runtime.calls != 1 {
		t.Fatalf("monitor settle failed or re-kicked: job=%#v calls=%d", after, runtime.calls)
	}
}

// ORCH-LEASE-005: uncertainty without Agent evidence fails terminal after the evidence window.
func TestUncertainExecutionFailsWhenEvidenceWindowExpires(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	if _, err := db.Exec(`UPDATE event_runs SET action='stop',desired_revision_id='' WHERE id='job-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE application_instances SET desired_state='stopped',desired_revision_id='' WHERE id='inst-1'`); err != nil {
		t.Fatal(err)
	}
	runtime := &uncertainReconciler{}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{UncertaintyEvidenceWindow: time.Minute})
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
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	still, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if still.State != JobRunning || still.ErrorClass != "uncertainty" {
		t.Fatalf("evidence window must keep uncertainty: %#v", still)
	}
	past := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE event_runs SET updated_at=? WHERE id='job-1'`, past); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.process(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobFailed || after.ErrorCode != "execution_evidence_unreachable" || after.ErrorClass != "evidence" {
		t.Fatalf("expected terminal evidence failure, got %#v", after)
	}
	if runtime.calls != 1 {
		t.Fatalf("evidence timeout must not repeat reconcile side effects: calls=%d", runtime.calls)
	}
}

func seedUncertainApplyJob(t *testing.T, db *sql.DB, updatedAt time.Time, lastStage string) {
	t.Helper()
	insertOrchestratorTestRows(t, db)
	now := updatedAt.UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE application_instances SET observed_state='running',observed_generation=1,observed_spec_hash='hash-1',observed_at=? WHERE id='inst-1'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE event_runs SET status='running',attempts=1,lease_owner='worker-a',lease_token='lease-a',execution_id='exec-a',intent_id='job-1',error_code='execution_monitoring',error_class='uncertainty',error_message='awaiting',error='awaiting',last_stage=?,lease_expires_at=NULL,updated_at=? WHERE id='job-1'`, lastStage, now); err != nil {
		t.Fatal(err)
	}
}

// ORCH-LEASE-004(b): converged AppDB observation + incomplete evidence past grace → bounded retry.
func TestUncertainConvergedObservationRetriesAfterGrace(t *testing.T) {
	db := newOrchestratorTestDB(t)
	past := time.Now().UTC().Add(-3 * time.Minute)
	seedUncertainApplyJob(t, db, past, "")
	runtime := &uncertainReconciler{}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{UncertaintyConvergedRetryGrace: time.Minute})
	if err := ctrl.process(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobFailedRetryable || after.ErrorCode != "execution_evidence_stale" || after.ErrorClass != "evidence" {
		t.Fatalf("expected stale bounded retry, got %#v", after)
	}
	if runtime.calls != 0 {
		t.Fatalf("converged retry must not re-kick reconcile: calls=%d", runtime.calls)
	}
}

func TestUncertainConvergedObservationKeepsFenceWithinGrace(t *testing.T) {
	db := newOrchestratorTestDB(t)
	seedUncertainApplyJob(t, db, time.Now().UTC(), "")
	runtime := &uncertainReconciler{}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{UncertaintyConvergedRetryGrace: 2 * time.Minute})
	if err := ctrl.process(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobRunning || after.ErrorClass != "uncertainty" {
		t.Fatalf("within grace must keep uncertainty: %#v", after)
	}
}

func TestUncertainProgressRefreshDefersConvergedRetry(t *testing.T) {
	db := newOrchestratorTestDB(t)
	// Entered uncertainty long ago, but a fresh ensure_image projection refreshed updated_at.
	seedUncertainApplyJob(t, db, time.Now().UTC().Add(-5*time.Minute), "ensure_image")
	store := NewStore(db)
	job, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProgress(context.Background(), job, "ensure_image", []Step{{Name: "ensure_image", Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	runtime := &uncertainReconciler{}
	ctrl := NewController(store, runtime, ControllerConfig{UncertaintyConvergedRetryGrace: 2 * time.Minute})
	if err := ctrl.process(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobRunning || after.ErrorClass != "uncertainty" || after.LastStage != "ensure_image" {
		t.Fatalf("fresh progress must defer converged retry: %#v", after)
	}
}

func TestUncertainUnconvergedKeepsEvidenceWindow(t *testing.T) {
	db := newOrchestratorTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Minute)
	seedUncertainApplyJob(t, db, past, "")
	if _, err := db.Exec(`UPDATE application_instances SET observed_generation=0,observed_spec_hash='' WHERE id='inst-1'`); err != nil {
		t.Fatal(err)
	}
	runtime := &uncertainReconciler{}
	store := NewStore(db)
	ctrl := NewController(store, runtime, ControllerConfig{
		UncertaintyConvergedRetryGrace: time.Minute,
		UncertaintyEvidenceWindow:      10 * time.Minute,
	})
	if err := ctrl.process(context.Background(), "job-1"); err != nil {
		t.Fatal(err)
	}
	still, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if still.State != JobRunning || still.ErrorClass != "uncertainty" {
		t.Fatalf("unconverged must keep uncertainty inside evidence window: %#v", still)
	}
}

type uncertainErrorReconciler struct{}

func (*uncertainErrorReconciler) Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error) {
	return ReconcileResponse{}, errors.New("unused")
}
func (*uncertainErrorReconciler) ResolveExecution(context.Context, ReconcileRequestRPC) (ReconcileResponse, bool, error) {
	return ReconcileResponse{}, false, errors.New("agent unreachable")
}

func TestUncertainAgentErrorKeepsFenceDespiteConverged(t *testing.T) {
	db := newOrchestratorTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Minute)
	seedUncertainApplyJob(t, db, past, "")
	store := NewStore(db)
	ctrl := NewController(store, &uncertainErrorReconciler{}, ControllerConfig{
		UncertaintyConvergedRetryGrace: time.Minute,
		UncertaintyEvidenceWindow:      30 * time.Minute,
	})
	if err := ctrl.process(context.Background(), "job-1"); err == nil {
		// ResolveExecution error is returned to caller while fence stays.
	}
	after, err := store.GetJob(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if after.State != JobRunning || after.ErrorClass != "uncertainty" {
		t.Fatalf("unreachable agent must keep uncertainty despite converged observation: %#v", after)
	}
}

func TestControllerStartEnqueuesUncertainJobs(t *testing.T) {
	db := newOrchestratorTestDB(t)
	past := time.Now().UTC().Add(-5 * time.Minute)
	seedUncertainApplyJob(t, db, past, "")
	store := NewStore(db)
	ctrl := NewController(store, &uncertainReconciler{}, ControllerConfig{
		Owner:                          "test-start",
		WorkerCount:                    1,
		ScanInterval:                   50 * time.Millisecond,
		UncertaintyConvergedRetryGrace: time.Minute,
	})
	ctx := context.Background()
	if err := ctrl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ctrl.Stop() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		job, err := store.GetJob(ctx, "job-1")
		if err != nil {
			t.Fatal(err)
		}
		if job.State == JobFailedRetryable && job.ErrorCode == "execution_evidence_stale" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("start did not settle uncertain job, got %#v", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
