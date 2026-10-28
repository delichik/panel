package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"panel/internal/platform/logging"
)

// defaultMaxJobAttempts caps how many total execution attempts a Job may have
// before a retryable failure becomes terminal. Every retry must terminate so a
// permanently failing target (for example an unreachable agent during purge)
// cannot retry forever.
const defaultMaxJobAttempts = 10

// defaultUncertaintyEvidenceWindow is how long a Job may stay running+uncertainty
// while the controller polls Agent evidence. After the window, the Job fails
// terminal with execution_evidence_unreachable so the conflict domain is freed.
// Never auto-succeed without Agent proof.
const defaultUncertaintyEvidenceWindow = 30 * time.Minute

// defaultDispatchWait is how long a worker waits for RuntimeReconcile to finish
// synchronously before releasing into monitor mode. Agent work continues; Panel
// settles from durable execution reports (phase-6 dispatch-only).
const defaultDispatchWait = 15 * time.Second

// controllerDiagnosticInterval prevents a persistent database/runtime fault
// from producing one identical process log on every 250 ms scan.
const controllerDiagnosticInterval = 30 * time.Second

type RuntimeReconciler interface {
	Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error)
}

// ExecutionResultResolver verifies a previously submitted execution without rerunning it.
type ExecutionResultResolver interface {
	ResolveExecution(context.Context, ReconcileRequestRPC) (ReconcileResponse, bool, error)
}

type ControllerConfig struct {
	WorkerCount  int
	ScanInterval time.Duration
	LeaseTTL     time.Duration
	QueueSize    int
	Owner        string
	// MaxAttempts is the maximum number of total execution attempts (including
	// the first) for a Job. Once a retryable failure reaches this threshold the
	// Job transitions to terminal failed with error_code=max_attempts_exceeded.
	// Zero uses defaultMaxJobAttempts.
	MaxAttempts int
	// UncertaintyEvidenceWindow caps how long a Job may remain running with
	// error_class=uncertainty while Agent evidence is unavailable or incomplete.
	// Zero uses defaultUncertaintyEvidenceWindow. Tests may shorten it.
	UncertaintyEvidenceWindow time.Duration
	// DispatchWait caps how long process() blocks on RuntimeReconcile before
	// entering monitor mode so the worker can release. Zero uses
	// defaultDispatchWait. Negative disables dispatch-only (wait until RPC ends).
	DispatchWait time.Duration
	OnSucceeded  func(context.Context, Job, ReconcileResponse)
	OnFailed     func(context.Context, Job, ReconcileResponse)
}

// JobRunner executes one deploy Run ID through the unified Manager.Execute
// path. When set (production BindRunEngineWorker), workerLoop calls this
// instead of RunJob directly so claim goes Manager → Execute → Store.Claim.
type JobRunner func(ctx context.Context, jobID string) error

type Controller struct {
	store        *Store
	planner      *Planner
	observations *ObservationWriter
	runtime      RuntimeReconciler
	config       ControllerConfig
	wake         chan struct{}
	queue        chan queuedJob
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	keys         map[string]struct{}
	diagnostics  map[string]time.Time
	// externalScan means the unified RunEngine worker owns the due/wake ticker
	// (ENG-RUN-001). The process pool still serializes per app:server; when
	// jobRunner is set, each item goes Manager.Run → Execute → RunJob.
	externalScan bool
	externalWake func()
	jobRunner    JobRunner
}

type queuedJob struct {
	id  string
	key string
}

func NewController(store *Store, runtime RuntimeReconciler, cfg ControllerConfig) *Controller {
	if cfg.WorkerCount < 1 {
		cfg.WorkerCount = 8
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 250 * time.Millisecond
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 3 * time.Minute
	}
	if cfg.QueueSize < cfg.WorkerCount*2 {
		cfg.QueueSize = cfg.WorkerCount * 2
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxJobAttempts
	}
	if cfg.UncertaintyEvidenceWindow <= 0 {
		cfg.UncertaintyEvidenceWindow = defaultUncertaintyEvidenceWindow
	}
	if strings.TrimSpace(cfg.Owner) == "" {
		cfg.Owner = "orchestrator-worker"
	}
	var db *sql.DB
	if store != nil {
		db = store.DB()
	}
	return &Controller{store: store, planner: NewPlanner(store), observations: NewObservationWriter(db), runtime: runtime, config: cfg, wake: make(chan struct{}, 1), queue: make(chan queuedJob, cfg.QueueSize), keys: map[string]struct{}{}, diagnostics: map[string]time.Time{}}
}

func (c *Controller) Planner() *Planner { return c.planner }

func (c *Controller) Running() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancel != nil
}

func (c *Controller) Wake() {
	if c == nil {
		return
	}
	c.mu.Lock()
	external := c.externalScan
	wakeFn := c.externalWake
	c.mu.Unlock()
	if external {
		if wakeFn != nil {
			wakeFn()
		}
		return
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// SetExternalScan hands due/wake ticking to the unified RunEngine worker while
// this Controller keeps the per-app:server queue. Pass nil wake to clear.
// Safe to call before Start.
func (c *Controller) SetExternalScan(wake func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.externalScan = wake != nil
	c.externalWake = wake
}

// SetJobRunner wires the unified Manager.Execute dispatch for queued deploy
// Runs. Pass nil to fall back to direct RunJob (tests/harnesses).
func (c *Controller) SetJobRunner(run JobRunner) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jobRunner = run
}

func (c *Controller) getJobRunner() JobRunner {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobRunner
}

// JobRunnerConfigured reports whether unified Manager.Execute dispatch is wired.
func (c *Controller) JobRunnerConfigured() bool {
	return c.getJobRunner() != nil
}

// ExternalScanConfigured reports whether due/wake ticking is owned externally.
func (c *Controller) ExternalScanConfigured() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.externalScan
}

// ScanInterval is the deploy due-scan cadence (default 250ms).
func (c *Controller) ScanInterval() time.Duration {
	if c == nil {
		return 250 * time.Millisecond
	}
	if c.config.ScanInterval > 0 {
		return c.config.ScanInterval
	}
	return 250 * time.Millisecond
}

// Poll recovers expired leases and enqueues due/uncertain Jobs. Used by the
// unified worker when externalScan is enabled; also safe for tests.
// workFound is true when at least one candidate was handed to the process pool.
func (c *Controller) Poll(ctx context.Context) (bool, error) {
	if c == nil || c.store == nil {
		return false, ErrStoreUnavailable
	}
	n, err := c.enqueueDue(ctx)
	return n > 0, err
}

func (c *Controller) Start(parent context.Context) error {
	if c == nil || c.store == nil {
		return ErrStoreUnavailable
	}
	if err := c.store.Validate(); err != nil {
		return err
	}
	if err := c.store.RecoverExpiredLeases(parent); err != nil {
		return err
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	external := c.externalScan
	if !external {
		c.wg.Add(1)
		go c.scanLoop(ctx)
	}
	for i := 0; i < c.config.WorkerCount; i++ {
		c.wg.Add(1)
		go c.workerLoop(ctx)
	}
	c.mu.Unlock()
	if external {
		// Immediate drain; subsequent polls come from the unified worker.
		_, _ = c.enqueueDue(ctx)
	} else {
		c.Wake()
	}
	return nil
}

func (c *Controller) Stop() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
	c.wg.Wait()
	return nil
}

func (c *Controller) scanLoop(ctx context.Context) {
	defer c.wg.Done()
	active := c.config.ScanInterval
	if active <= 0 {
		active = 250 * time.Millisecond
	}
	idle := active
	if active >= 250*time.Millisecond {
		idle = 2 * time.Second
		if idle < active {
			idle = active
		}
	}
	interval := active
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		n, err := c.enqueueDue(ctx)
		if err != nil && ctx.Err() == nil {
			c.logControllerError("enqueue_due", "", err)
		}
		next := idle
		if n > 0 {
			next = active
		}
		if next != interval {
			ticker.Reset(next)
			interval = next
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
			if interval != active {
				ticker.Reset(active)
				interval = active
			}
		case <-ticker.C:
		}
	}
}

func (c *Controller) enqueueDue(ctx context.Context) (int, error) {
	if err := c.store.RecoverExpiredLeases(ctx); err != nil {
		return 0, err
	}
	uncertain, err := c.store.ListUncertain(ctx, c.config.QueueSize)
	if err != nil {
		return 0, err
	}
	for _, job := range uncertain {
		c.processAsync(ctx, job.ID)
	}
	jobs, err := c.store.ListDue(ctx, c.config.QueueSize)
	if err != nil {
		return len(uncertain), err
	}
	for _, job := range jobs {
		c.processAsync(ctx, job.ID)
	}
	return len(uncertain) + len(jobs), nil
}

func (c *Controller) processAsync(ctx context.Context, jobID string) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	key := jobID
	job, err := c.store.GetJob(ctx, jobID)
	if err == nil {
		key = job.ApplicationID + ":" + job.ServerID
	} else {
		c.logControllerError("enqueue_job_lookup", jobID, err)
		return
	}
	c.mu.Lock()
	if _, exists := c.keys[key]; exists {
		c.mu.Unlock()
		return
	}
	c.keys[key] = struct{}{}
	c.mu.Unlock()
	select {
	case c.queue <- queuedJob{id: jobID, key: key}:
	case <-ctx.Done():
		c.releaseKey(key)
	default:
		c.releaseKey(key)
		c.logControllerError("enqueue_queue_full", jobID, errors.New("orchestrator work queue is full; job deferred to the next scan"))
	}
}

func (c *Controller) workerLoop(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-c.queue:
			var err error
			if runner := c.getJobRunner(); runner != nil {
				err = runner(ctx, item.id)
			} else {
				err = c.RunJob(ctx, item.id)
			}
			if err != nil && ctx.Err() == nil {
				c.logControllerError("process_job", item.id, err)
			}
			c.releaseKey(item.key)
		}
	}
}

func (c *Controller) logControllerError(stage, jobID string, err error) {
	if c == nil || err == nil {
		return
	}
	now := time.Now()
	key := stage + "\x00" + jobID
	c.mu.Lock()
	if c.diagnostics == nil {
		c.diagnostics = map[string]time.Time{}
	}
	if last, ok := c.diagnostics[key]; ok && now.Sub(last) < controllerDiagnosticInterval {
		c.mu.Unlock()
		return
	}
	c.diagnostics[key] = now
	if len(c.diagnostics) > 256 {
		for existingKey, last := range c.diagnostics {
			if now.Sub(last) >= controllerDiagnosticInterval {
				delete(c.diagnostics, existingKey)
			}
		}
		for len(c.diagnostics) > 256 {
			var oldestKey string
			var oldest time.Time
			for existingKey, last := range c.diagnostics {
				if oldestKey == "" || last.Before(oldest) {
					oldestKey, oldest = existingKey, last
				}
			}
			delete(c.diagnostics, oldestKey)
		}
	}
	c.mu.Unlock()
	logging.From(logging.Background("orchestrator")).Error("orchestrator controller operation failed",
		zap.String("component", "orchestrator_controller"),
		zap.String("stage", stage),
		zap.String("owner", c.config.Owner),
		zap.String("job_id", jobID),
		zap.Duration("repeat_suppression", controllerDiagnosticInterval),
		zap.Error(err))
}

func (c *Controller) releaseKey(key string) {
	c.mu.Lock()
	delete(c.keys, key)
	c.mu.Unlock()
}

// RunJob executes one Job through Store.Claim and RuntimeReconcile.
// Production dispatch reaches here via Manager.Run → Definition.Execute
// (ENG-RUN-001). Tests may call RunJob directly.
func (c *Controller) RunJob(ctx context.Context, jobID string) error {
	if c == nil || c.store == nil {
		return ErrStoreUnavailable
	}
	if _, err := c.store.GetJob(ctx, jobID); err != nil {
		return err
	}
	return c.process(ctx, jobID)
}

func (c *Controller) process(ctx context.Context, jobID string) error {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "orchestrator"})
	existing, getErr := c.store.GetJob(ctx, jobID)
	if getErr == nil && existing.State == JobRunning && existing.ErrorClass == "uncertainty" {
		return c.resolveUncertain(ctx, existing)
	}
	job, claimed, err := c.store.Claim(ctx, jobID, c.config.Owner, c.config.LeaseTTL)
	if err != nil || !claimed {
		return err
	}
	traceJobEvent(ctx, "job_claimed", job)
	if strings.TrimSpace(job.DesiredRevisionID) == "" && job.Action == ActionApply {
		return c.fail(ctx, job, ReconcileResponse{ErrorCode: "invalid_revision", ErrorClass: "configuration", ErrorMessage: "job has no immutable revision", Retryable: false})
	}
	var revision Revision
	if job.DesiredRevisionID != "" {
		revision, err = c.store.GetRevisionByID(ctx, job.DesiredRevisionID)
		if err != nil {
			return c.fail(ctx, job, ReconcileResponse{ErrorCode: "revision_unavailable", ErrorClass: "configuration", ErrorMessage: err.Error(), Retryable: true})
		}
	}
	previousContainerName, _ := c.store.InstanceContainerName(ctx, job.InstanceID)
	rpc := ReconcileRequestRPC{OperationID: job.IntentID, RunID: job.IntentID, JobID: job.ID, ExecutionID: job.ExecutionID, ApplicationID: job.ApplicationID, InstanceID: job.InstanceID, ServerID: job.ServerID, Action: job.Action, DesiredGeneration: job.DesiredGeneration, DesiredSpecHash: job.DesiredSpecHash, DesiredRevisionID: job.DesiredRevisionID, RenderedRuntimeSpec: firstRuntimeSpec(job.DesiredSpecJSON, revision.RenderedRuntimeSpec), RemoveData: job.RemoveData, PreviousContainerName: previousContainerName}
	if c.runtime == nil {
		return c.fail(ctx, job, ReconcileResponse{ErrorCode: "runtime_unavailable", ErrorClass: "agent_unavailable", ErrorMessage: "runtime reconciler is unavailable", Retryable: true})
	}
	leaseCtx, stopLease := context.WithCancel(ctx)
	defer stopLease()
	go c.renewLease(leaseCtx, job)
	traceJobEvent(ctx, "agent_reconcile_started", job)
	response, runErr, handedToMonitor := c.dispatchReconcile(ctx, job, rpc)
	if handedToMonitor {
		return nil
	}
	if runErr != nil {
		// A structured business failure is a known outcome and follows the normal
		// retry policy. Monitor mode (uncertainty fence) is reserved for calls with
		// no trustworthy terminal result, such as transport loss after a possible
		// remote side effect — progress continues via Agent step reports.
		if response.ErrorCode != "" || response.ErrorMessage != "" {
			return c.fail(ctx, job, response)
		}
		if len(response.Steps) > 0 {
			_, _ = c.store.UpdateProgress(context.WithoutCancel(ctx), job, lastStep(response.Steps), response.Steps)
		}
		// Keep the conflict row owned and resolve the same execution identity
		// before allowing new work (ENG-RUN-004 remote-fence).
		return c.store.MarkUncertain(context.WithoutCancel(ctx), job, runErr.Error())
	}
	return c.acceptResponse(ctx, job, response)
}

type reconcileOutcome struct {
	response ReconcileResponse
	err      error
}

// dispatchReconcile runs RuntimeReconcile with an optional dispatch window.
// If the RPC does not finish in time, the Job enters monitor mode and the
// worker returns; late RPC results are discarded because Agent durable
// evidence is settled by resolveUncertain.
func (c *Controller) dispatchReconcile(ctx context.Context, job Job, rpc ReconcileRequestRPC) (ReconcileResponse, error, bool) {
	wait := c.config.DispatchWait
	if wait == 0 {
		wait = defaultDispatchWait
	}
	if wait < 0 {
		response, err := c.runtime.Reconcile(ctx, rpc)
		return response, err, false
	}
	outcomes := make(chan reconcileOutcome, 1)
	go func() {
		// Detach from the worker's wait so Agent-side work can finish and
		// persist evidence after monitor mode takes over.
		resp, err := c.runtime.Reconcile(context.WithoutCancel(ctx), rpc)
		outcomes <- reconcileOutcome{response: resp, err: err}
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		go func() { <-outcomes }()
		return ReconcileResponse{}, ctx.Err(), false
	case outcome := <-outcomes:
		return outcome.response, outcome.err, false
	case <-timer.C:
		// Drain the in-flight RPC without settling here; monitor owns completion.
		go func() { <-outcomes }()
		traceJobEvent(ctx, "agent_reconcile_dispatched", job, zap.Duration("dispatch_wait", wait))
		if err := c.store.MarkUncertain(context.WithoutCancel(ctx), job, "remote execution dispatched; awaiting Agent progress reports"); err != nil {
			return ReconcileResponse{}, err, false
		}
		return ReconcileResponse{}, nil, true
	}
}

func (c *Controller) acceptResponse(ctx context.Context, job Job, response ReconcileResponse) error {

	traceJobEvent(ctx, "agent_reconcile_finished", job,
		zap.String("observed_state", response.ObservedState),
		zap.String("error_code", response.ErrorCode),
		zap.Bool("retryable", response.Retryable))
	if response.ErrorMessage != "" || response.ErrorCode != "" {
		return c.fail(ctx, job, response)
	}
	if !ApplyReadyToSucceed(job.Action, response.Steps) {
		// ORCH-CTRL-007: do not succeed apply before verify_running evidence.
		// Keep fencing and wait for monitor/resolve to obtain a complete trail.
		if len(response.Steps) > 0 {
			_, _ = c.store.UpdateProgress(ctx, job, lastStep(response.Steps), response.Steps)
		}
		return c.store.MarkUncertain(ctx, job, "apply is awaiting verify_running evidence")
	}
	if response.ObservedAt.IsZero() {
		response.ObservedAt = time.Now().UTC()
	}
	if c.observations != nil {
		written, err := c.observations.Write(ctx, Observation{InstanceID: job.InstanceID, Source: "reconcile", ObservedAt: response.ObservedAt, ObservedState: response.ObservedState, ContainerName: response.ContainerName, ContainerID: response.ContainerID, ObservedGeneration: response.ObservedGeneration, ObservedSpecHash: response.ObservedSpecHash, ObservedImageDigest: response.ObservedImageDigest, JobID: job.ID, LeaseToken: job.LeaseToken, DesiredSpecJSON: job.DesiredSpecJSON})
		if err != nil {
			return c.fail(ctx, job, ReconcileResponse{ErrorCode: "observation_write_failed", ErrorClass: "storage", ErrorMessage: err.Error(), Retryable: true})
		}
		if !written.Accepted {
			traceJobEvent(ctx, "observation_discarded_stale", job, zap.String("observed_state", response.ObservedState))
			return ErrOwnershipLost
		}
		traceJobEvent(ctx, "observation_accepted", job, zap.String("observed_state", response.ObservedState))
	}
	changed, err := c.store.DesiredChanged(ctx, job)
	if err != nil {
		return err
	}
	if changed {
		ok, err := c.store.Requeue(ctx, job, "desired state changed while runtime call was in flight")
		if err != nil {
			return err
		}
		if !ok {
			traceJobEvent(ctx, "lease_lost", job, zap.String("reason", "requeue_desired_changed"))
			return ErrOwnershipLost
		}
		return nil
	}
	ok, err := c.store.Succeed(ctx, job, response)
	if err != nil {
		return err
	}
	if !ok {
		traceJobEvent(ctx, "lease_lost", job, zap.String("reason", "succeed_fencing"))
		return ErrOwnershipLost
	}
	traceJobEvent(ctx, "job_succeeded", job, zap.String("observed_state", response.ObservedState))
	if c.config.OnSucceeded != nil {
		c.config.OnSucceeded(ctx, job, response)
	}
	return nil
}

func (c *Controller) resolveUncertain(ctx context.Context, job Job) error {
	resolver, ok := c.runtime.(ExecutionResultResolver)
	if ok {
		rpc := ReconcileRequestRPC{OperationID: job.IntentID, RunID: job.IntentID, JobID: job.ID, ExecutionID: job.ExecutionID, ApplicationID: job.ApplicationID, InstanceID: job.InstanceID, ServerID: job.ServerID, Action: job.Action, DesiredGeneration: job.DesiredGeneration, DesiredSpecHash: job.DesiredSpecHash, DesiredRevisionID: job.DesiredRevisionID}
		response, finished, err := resolver.ResolveExecution(ctx, rpc)
		if finished {
			return c.acceptResponse(ctx, job, response)
		}
		if !c.uncertaintyEvidenceExpired(job) {
			return err
		}
		// Evidence window elapsed with no trustworthy result: terminal fail, never succeed.
		return c.fail(ctx, job, ReconcileResponse{
			ErrorCode:    "execution_evidence_unreachable",
			ErrorClass:   "evidence",
			ErrorMessage: "execution evidence could not be obtained within the verification window",
			ErrorDetail:  "agent result stayed unavailable or incomplete; remote side effects were not auto-confirmed",
			Retryable:    false,
		})
	}
	if !c.uncertaintyEvidenceExpired(job) {
		return nil
	}
	return c.fail(ctx, job, ReconcileResponse{
		ErrorCode:    "execution_evidence_unreachable",
		ErrorClass:   "evidence",
		ErrorMessage: "execution evidence could not be obtained within the verification window",
		ErrorDetail:  "no execution result resolver is available; remote side effects were not auto-confirmed",
		Retryable:    false,
	})
}

func (c *Controller) uncertaintyEvidenceExpired(job Job) bool {
	if job.UpdatedAt.IsZero() {
		return false
	}
	window := c.config.UncertaintyEvidenceWindow
	if window <= 0 {
		window = defaultUncertaintyEvidenceWindow
	}
	return c.store.now().UTC().Sub(job.UpdatedAt.UTC()) >= window
}

func (c *Controller) renewLease(ctx context.Context, job Job) {
	interval := c.config.LeaseTTL / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			owned, err := c.store.Renew(ctx, job, c.config.LeaseTTL)
			if err != nil {
				c.logControllerError("renew_lease", job.ID, err)
				return
			}
			if !owned {
				c.logControllerError("renew_lease", job.ID, ErrOwnershipLost)
				return
			}
		}
	}
}

func firstRuntimeSpec(candidate, fallback []byte) []byte {
	value := strings.TrimSpace(string(candidate))
	if value != "" && value != "{}" && value != "null" {
		return candidate
	}
	return fallback
}

func (c *Controller) fail(ctx context.Context, job Job, response ReconcileResponse) error {
	if response.Retryable && c.config.MaxAttempts > 0 && job.Attempts >= c.config.MaxAttempts {
		rootCode, rootClass := response.ErrorCode, response.ErrorClass
		rootDetail := strings.TrimSpace(response.ErrorDetail)
		if rootDetail == "" {
			rootDetail = strings.TrimSpace(response.ErrorMessage)
		}
		response.Retryable = false
		response.ErrorCode = "max_attempts_exceeded"
		response.ErrorClass = "retry_exhausted"
		if strings.TrimSpace(response.ErrorMessage) == "" {
			response.ErrorMessage = fmt.Sprintf("job exceeded %d attempts (%d); giving up", c.config.MaxAttempts, job.Attempts)
		}
		response.ErrorDetail = fmt.Sprintf("retry limit reached after %d attempts; last error code=%s class=%s: %s", job.Attempts, rootCode, rootClass, rootDetail)
	}
	ok, err := c.store.Fail(ctx, job, response)
	if err != nil {
		return err
	}
	if !ok {
		traceJobEvent(ctx, "lease_lost", job, zap.String("reason", "fail_fencing"))
		return ErrOwnershipLost
	}
	if c.config.OnFailed != nil {
		c.config.OnFailed(ctx, job, response)
	}
	if response.Retryable {
		traceJobEvent(ctx, "job_retry_scheduled", job,
			zap.String("error_code", response.ErrorCode),
			zap.String("error_class", response.ErrorClass))
	} else {
		traceJobEvent(ctx, "job_failed", job,
			zap.String("error_code", response.ErrorCode),
			zap.String("error_class", response.ErrorClass))
	}
	return nil
}

func (c *Controller) String() string { return fmt.Sprintf("orchestrator[%s]", c.config.Owner) }
