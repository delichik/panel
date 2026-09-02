package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// defaultMaxJobAttempts caps how many total execution attempts a Job may have
// before a retryable failure becomes terminal. Every retry must terminate so a
// permanently failing target (for example an unreachable agent during purge)
// cannot retry forever.
const defaultMaxJobAttempts = 10

type RuntimeReconciler interface {
	Reconcile(context.Context, ReconcileRequestRPC) (ReconcileResponse, error)
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
	OnSucceeded func(context.Context, Job, ReconcileResponse)
	OnFailed    func(context.Context, Job, ReconcileResponse)
}

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
	if strings.TrimSpace(cfg.Owner) == "" {
		cfg.Owner = "orchestrator-worker"
	}
	var db *sql.DB
	if store != nil {
		db = store.DB()
	}
	return &Controller{store: store, planner: NewPlanner(store), observations: NewObservationWriter(db), runtime: runtime, config: cfg, wake: make(chan struct{}, 1), queue: make(chan queuedJob, cfg.QueueSize), keys: map[string]struct{}{}}
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
	if c != nil {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
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
	c.wg.Add(1)
	go c.scanLoop(ctx)
	for i := 0; i < c.config.WorkerCount; i++ {
		c.wg.Add(1)
		go c.workerLoop(ctx)
	}
	c.mu.Unlock()
	c.Wake()
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
	ticker := time.NewTicker(c.config.ScanInterval)
	defer ticker.Stop()
	for {
		if err := c.enqueueDue(ctx); err != nil && ctx.Err() == nil {
			_ = err
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-ticker.C:
		}
	}
}

func (c *Controller) enqueueDue(ctx context.Context) error {
	if err := c.store.RecoverExpiredLeases(ctx); err != nil {
		return err
	}
	jobs, err := c.store.ListDue(ctx, c.config.QueueSize)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		c.processAsync(ctx, job.ID)
	}
	return nil
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
	}
}

func (c *Controller) workerLoop(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-c.queue:
			_ = c.process(ctx, item.id)
			c.releaseKey(item.key)
		}
	}
}

func (c *Controller) releaseKey(key string) {
	c.mu.Lock()
	delete(c.keys, key)
	c.mu.Unlock()
}

func (c *Controller) process(ctx context.Context, jobID string) error {
	job, claimed, err := c.store.Claim(ctx, jobID, c.config.Owner, c.config.LeaseTTL)
	if err != nil || !claimed {
		return err
	}
	traceJobEvent("job_claimed", job)
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
	rpc := ReconcileRequestRPC{JobID: job.ID, ExecutionID: job.ExecutionID, ApplicationID: job.ApplicationID, InstanceID: job.InstanceID, ServerID: job.ServerID, Action: job.Action, DesiredGeneration: job.DesiredGeneration, DesiredSpecHash: job.DesiredSpecHash, DesiredRevisionID: job.DesiredRevisionID, RenderedRuntimeSpec: firstRuntimeSpec(job.DesiredSpecJSON, revision.RenderedRuntimeSpec), RemoveData: job.RemoveData, PreviousContainerName: previousContainerName}
	if c.runtime == nil {
		return c.fail(ctx, job, ReconcileResponse{ErrorCode: "runtime_unavailable", ErrorClass: "agent_unavailable", ErrorMessage: "runtime reconciler is unavailable", Retryable: true})
	}
	leaseCtx, stopLease := context.WithCancel(ctx)
	defer stopLease()
	go c.renewLease(leaseCtx, job)
	traceJobEvent("agent_reconcile_started", job)
	response, runErr := c.runtime.Reconcile(ctx, rpc)
	if runErr != nil && response.ErrorMessage == "" {
		response.ErrorCode = "runtime_reconcile_failed"
		response.ErrorClass = "runtime"
		response.ErrorMessage = runErr.Error()
		response.Retryable = true
	}
	traceJobEvent("agent_reconcile_finished", job,
		zap.String("observed_state", response.ObservedState),
		zap.String("error_code", response.ErrorCode),
		zap.Bool("retryable", response.Retryable))
	if response.ErrorMessage != "" || response.ErrorCode != "" {
		return c.fail(ctx, job, response)
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
			traceJobEvent("observation_discarded_stale", job, zap.String("observed_state", response.ObservedState))
			return ErrOwnershipLost
		}
		traceJobEvent("observation_accepted", job, zap.String("observed_state", response.ObservedState))
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
			traceJobEvent("lease_lost", job, zap.String("reason", "requeue_desired_changed"))
			return ErrOwnershipLost
		}
		return nil
	}
	ok, err := c.store.Succeed(ctx, job, response)
	if err != nil {
		return err
	}
	if !ok {
		traceJobEvent("lease_lost", job, zap.String("reason", "succeed_fencing"))
		return ErrOwnershipLost
	}
	traceJobEvent("job_succeeded", job, zap.String("observed_state", response.ObservedState))
	if c.config.OnSucceeded != nil {
		c.config.OnSucceeded(ctx, job, response)
	}
	return nil
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
			if err != nil || !owned {
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
		response.Retryable = false
		response.ErrorCode = "max_attempts_exceeded"
		response.ErrorClass = "retry_exhausted"
		response.ErrorMessage = fmt.Sprintf("job exceeded %d attempts (%d); giving up", c.config.MaxAttempts, job.Attempts)
	}
	ok, err := c.store.Fail(ctx, job, response)
	if err != nil {
		return err
	}
	if !ok {
		traceJobEvent("lease_lost", job, zap.String("reason", "fail_fencing"))
		return ErrOwnershipLost
	}
	if c.config.OnFailed != nil {
		c.config.OnFailed(ctx, job, response)
	}
	if response.Retryable {
		traceJobEvent("job_retry_scheduled", job,
			zap.String("error_code", response.ErrorCode),
			zap.String("error_class", response.ErrorClass))
	} else {
		traceJobEvent("job_failed", job,
			zap.String("error_code", response.ErrorCode),
			zap.String("error_class", response.ErrorClass))
	}
	return nil
}

func (c *Controller) String() string { return fmt.Sprintf("orchestrator[%s]", c.config.Owner) }
