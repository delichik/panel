package tasks

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	defaultQueuePollInterval   = 30 * time.Second
	defaultCleanupInterval     = 30 * time.Second
	defaultOrphanCheckInterval = 30 * time.Second
)

// DeployScheduler is the deploy due/wake side of RunEngine (ENG-RUN-001).
// The unified worker owns ticking; when a JobRunner is wired, claim/Execute
// goes through Manager.Run → Definition.Execute → Controller.RunJob.
type DeployScheduler interface {
	Poll(context.Context) error
	ScanInterval() time.Duration
	SetExternalScan(wake func())
}

// Service returns the underlying task service (for BindRunEngineWorker wiring).
func (w *Worker) Service() *Service {
	if w == nil {
		return nil
	}
	return w.service
}

// RunDeployJob loads one application_deploy row and runs it through Manager.Run
// (Execute → Controller.RunJob / Store.Claim). Used when the Controller pool
// dispatches via JobRunner instead of calling RunJob directly.
func (w *Worker) RunDeployJob(ctx context.Context, jobID string) error {
	if w == nil || w.service == nil || w.manager == nil {
		return ErrExecutorUnavailable()
	}
	task, err := w.service.Get(ctx, jobID)
	if err != nil {
		return err
	}
	return w.RunNow(ctx, task)
}

type Worker struct {
	service               *Service
	manager               *Manager
	deploy                DeployScheduler
	deployWake            chan struct{}
	lifecycleMu           sync.Mutex
	cancel                context.CancelFunc
	running               bool
	maintenanceWasRunning bool
	wg                    sync.WaitGroup
}

// RuntimeStats is the event-runner diagnostics snapshot (product: event executor).
type RuntimeStats struct {
	WorkerRunning     bool                     `json:"eventRunnerRunning"`
	RegisteredTypes   int                      `json:"registeredEventTypes"`
	ExecutableTypes   int                      `json:"executableEventTypes"`
	PeriodicTypes     int                      `json:"periodicEventTypes"`
	RunningExecutions int                      `json:"runningEventExecutions"`
	Definitions       []RuntimeDefinitionStats `json:"eventDefinitions"`
}

type RuntimeDefinitionStats struct {
	Type                    string `json:"type"`
	Hidden                  bool   `json:"hidden"`
	Quiet                   bool   `json:"quiet"`
	Executable              bool   `json:"executable"`
	Periodic                bool   `json:"periodic"`
	AllowRunNow             bool   `json:"allowRunNow"`
	AllowRetry              bool   `json:"allowRetry"`
	DefaultMaxRetries       int    `json:"defaultMaxRetries"`
	RecoveryPolicy          string `json:"recoveryPolicy"`
	ConcurrencyPolicy       string `json:"concurrencyPolicy"`
	StaleQueuedAfterSeconds int64  `json:"staleQueuedAfterSeconds"`
}

func NewWorker(service *Service) *Worker {
	return &Worker{
		service:    service,
		manager:    NewManager(service),
		deployWake: make(chan struct{}, 1),
	}
}

// AttachDeploy registers the jobs-side scheduler so this Worker owns deploy
// due/wake scanning. Call before Start (and before the scheduler's Start so
// SetExternalScan disables its internal scanLoop).
func (w *Worker) AttachDeploy(deploy DeployScheduler) {
	if w == nil {
		return
	}
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.deploy != nil {
		w.deploy.SetExternalScan(nil)
	}
	w.deploy = deploy
	if deploy != nil {
		deploy.SetExternalScan(w.signalDeployWake)
	}
}

func (w *Worker) signalDeployWake() {
	if w == nil {
		return
	}
	select {
	case w.deployWake <- struct{}{}:
	default:
	}
}

func (w *Worker) Start(parent context.Context) {
	if w == nil || w.service == nil {
		return
	}
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.running {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.running = true
	loops := 3
	if w.deploy != nil {
		loops++
	}
	w.wg.Add(loops)
	go w.queueLoop(ctx)
	go w.staleQueuedLoop(ctx)
	go w.orphanLoop(ctx)
	if w.deploy != nil {
		go w.deployLoop(ctx)
	}
}

func (w *Worker) Stop() {
	if w == nil {
		return
	}
	w.lifecycleMu.Lock()
	if !w.running {
		w.lifecycleMu.Unlock()
		return
	}
	cancel := w.cancel
	w.cancel = nil
	w.running = false
	w.lifecycleMu.Unlock()
	cancel()
	w.wg.Wait()
}

// EventRuntime returns event-executor registration and run counts for diagnostics.
func (w *Worker) EventRuntime() RuntimeStats {
	return w.TaskRuntime()
}

// TaskRuntime is kept as a compatibility alias for EventRuntime.
func (w *Worker) TaskRuntime() RuntimeStats {
	if w == nil || w.service == nil {
		return RuntimeStats{}
	}
	w.lifecycleMu.Lock()
	running := w.running
	w.lifecycleMu.Unlock()
	stats := RuntimeStats{
		WorkerRunning:     running,
		RunningExecutions: w.service.RunningExecutionCount(),
		Definitions:       []RuntimeDefinitionStats{},
	}
	taskTypes := w.service.Registry().Types()
	sort.Strings(taskTypes)
	for _, taskType := range taskTypes {
		stats.RegisteredTypes++
		def, ok := w.service.Registry().Definition(taskType)
		if !ok {
			continue
		}
		if def.Execute != nil {
			stats.ExecutableTypes++
		}
		if def.Periodic != nil && def.Periodic.CollectInputs != nil {
			stats.PeriodicTypes++
		}
		stats.Definitions = append(stats.Definitions, RuntimeDefinitionStats{
			Type:                    def.Type,
			Hidden:                  def.Hidden,
			Quiet:                   def.Quiet,
			Executable:              def.Execute != nil,
			Periodic:                def.Periodic != nil && def.Periodic.CollectInputs != nil,
			AllowRunNow:             def.AllowRunNow && def.Execute != nil,
			AllowRetry:              def.AllowRetry && def.Execute != nil,
			DefaultMaxRetries:       def.DefaultMaxRetries,
			RecoveryPolicy:          normalizeRecoveryPolicy(def.RecoveryPolicy),
			ConcurrencyPolicy:       def.ConcurrencyPolicy,
			StaleQueuedAfterSeconds: int64(def.StaleQueuedAfter.Seconds()),
		})
	}
	return stats
}

func (w *Worker) RunNow(ctx context.Context, task Task) error {
	if w == nil || w.manager == nil || w.service == nil {
		return ErrExecutorUnavailable()
	}
	if w.service.HasRunningExecution(task.ID) {
		return nil
	}
	defer w.service.FinishExecution(task.ID)
	ctx = logging.With(ctx, logging.Context{
		EventID:     eventRunLogID(task.ID),
		Module:      "event-runner",
		OperationID: task.OperationID,
		ExecutionID: task.ID,
	})
	return w.manager.runRecover(ctx, task)
}

func (w *Worker) queueLoop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(defaultQueuePollInterval)
	defer ticker.Stop()
	w.runDueTasks(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runDueTasks(ctx)
		}
	}
}

func (w *Worker) runDueTasks(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{EventID: logging.NewEventID(), Module: "event-runner"})
	// 单次轮询内发生 panic 时只记录日志并退出本轮，避免整个事件执行器 goroutine
	// 崩溃后事件队列无人驱动。
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.From(ctx).Error("event runner due-run loop recovered from panic", zap.Any("panic", recovered))
		}
	}()
	for _, task := range w.dueTasks(ctx) {
		if task.Status == StatusRunning && w.service.HasRunningExecution(task.ID) {
			continue
		}
		if !w.isAtFrontOfQueue(ctx, task) {
			continue
		}
		if err := w.RunNow(ctx, task); err != nil {
			logging.From(ctx).Warn("event runner run failed", zap.String("eventId", task.ID), zap.Error(err))
		}
	}
}

// isAtFrontOfQueue 返回任务是否位于其并发队列队首。worker 只运行队首任务，
// 非队首任务留到后续轮次再处理，避免某个并发键被阻塞时拖死整个 worker 循环。
func (w *Worker) isAtFrontOfQueue(ctx context.Context, task Task) bool {
	if w == nil || w.service == nil {
		return true
	}
	def, ok := w.service.Registry().Definition(task.Type)
	if !ok {
		return true
	}
	switch def.ConcurrencyPolicy {
	case ConcurrencyResourceQueue, ConcurrencyResourceExclusive:
	default:
		return true
	}
	if strings.TrimSpace(task.ConcurrencyKey) == "" {
		return true
	}
	first, ok, err := w.service.FirstActiveByConcurrencyKey(ctx, task.ConcurrencyKey)
	if err != nil {
		return false
	}
	if !ok {
		return true
	}
	return first.ID == task.ID
}

func (w *Worker) dueTasks(ctx context.Context) []Task {
	now := time.Now().UTC()
	out := []Task{}
	seen := map[string]struct{}{}
	for _, taskType := range w.service.Registry().Types() {
		def, ok := w.service.Registry().Definition(taskType)
		if !ok || def.Execute == nil {
			continue
		}
		// Remote-fence deploy Runs keep the dedicated ~250ms deployLoop
		// (lease recover + uncertain resolve). Generic 30s due must not
		// double-dispatch them (ENG-RUN-001).
		if normalizeRecoveryPolicy(def.RecoveryPolicy) == RecoveryRemoteFence {
			continue
		}
		for _, status := range []string{StatusQueued, StatusScheduled, StatusFailedRetryable} {
			result, err := w.service.List(ctx, ListFilter{Type: taskType, Status: status, Limit: 50, IncludeInternal: true, SortOldestFirst: true})
			if err != nil {
				logging.From(ctx).Warn("task worker list failed", zap.String("task_type", taskType), zap.String("status", status), zap.Error(err))
				continue
			}
			for _, task := range result.Items {
				if task.NextRunAt != nil && task.NextRunAt.After(now) {
					continue
				}
				if _, exists := seen[task.ID]; exists {
					continue
				}
				seen[task.ID] = struct{}{}
				out = append(out, task)
			}
		}
	}
	// 按创建时间从旧到新处理，保证并发队列队首任务先被运行；
	// 否则 worker 一直取最新任务，会永远排在队首任务之后无法推进。
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (w *Worker) staleQueuedLoop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(defaultCleanupInterval)
	defer ticker.Stop()
	w.expireStaleQueued(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.expireStaleQueued(ctx)
		}
	}
}

func (w *Worker) expireStaleQueued(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{EventID: logging.NewEventID(), Module: "event-runner"})
	byTimeout := map[time.Duration][]string{}
	for _, taskType := range w.service.Registry().Types() {
		def, ok := w.service.Registry().Definition(taskType)
		if !ok || def.StaleQueuedAfter <= 0 {
			continue
		}
		byTimeout[def.StaleQueuedAfter] = append(byTimeout[def.StaleQueuedAfter], taskType)
	}
	for timeout, taskTypes := range byTimeout {
		expired, err := w.service.ExpireStaleQueued(ctx, time.Now().UTC(), timeout, taskTypes)
		if err != nil {
			logging.From(ctx).Warn("task stale-queued cleanup failed", zap.Error(err))
			continue
		}
		if expired > 0 {
			logging.From(ctx).Info("task stale-queued cleanup marked tasks failed", zap.Int("expired", expired))
		}
	}
}

func (w *Worker) orphanLoop(ctx context.Context) {
	defer w.wg.Done()
	w.failOrphanedRunning(ctx)
	ticker := time.NewTicker(defaultOrphanCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.failOrphanedRunning(ctx)
		}
	}
}

func (w *Worker) deployLoop(ctx context.Context) {
	defer w.wg.Done()
	w.lifecycleMu.Lock()
	deploy := w.deploy
	w.lifecycleMu.Unlock()
	if deploy == nil {
		return
	}
	interval := deploy.ScanInterval()
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := deploy.Poll(ctx); err != nil && ctx.Err() == nil {
			logging.From(ctx).Warn("deploy run due scan failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-w.deployWake:
		case <-ticker.C:
		}
	}
}

func (w *Worker) failOrphanedRunning(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{EventID: logging.NewEventID(), Module: "event-runner"})
	now := time.Now().UTC()
	failed, err := w.service.FailRunningWithoutExecution(ctx, now)
	if err != nil {
		logging.From(ctx).Warn("task running execution check failed", zap.Error(err))
		return
	}
	if failed > 0 {
		logging.From(ctx).Info("task running execution check marked orphaned tasks for automatic verification", zap.Int("failed", failed))
	}
	expired, err := w.service.FailUncertainPastEvidenceWindow(ctx, now)
	if err != nil {
		logging.From(ctx).Warn("task uncertain evidence window check failed", zap.Error(err))
		return
	}
	if expired > 0 {
		logging.From(ctx).Info("task uncertain evidence window marked orphaned tasks failed", zap.Int("failed", expired))
	}
}
