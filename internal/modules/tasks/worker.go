package tasks

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	defaultQueuePollInterval   = time.Second
	defaultCleanupInterval     = time.Second
	defaultOrphanCheckInterval = 5 * time.Second
)

type Worker struct {
	service     *Service
	manager     *Manager
	periodic    *PeriodicRunner
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	running     bool
	wg          sync.WaitGroup
}

type RuntimeStats struct {
	WorkerRunning     bool                     `json:"workerRunning"`
	RegisteredTypes   int                      `json:"registeredTypes"`
	ExecutableTypes   int                      `json:"executableTypes"`
	PeriodicTypes     int                      `json:"periodicTypes"`
	RunningExecutions int                      `json:"runningExecutions"`
	Definitions       []RuntimeDefinitionStats `json:"definitions"`
}

type RuntimeDefinitionStats struct {
	Type                    string `json:"type"`
	Hidden                  bool   `json:"hidden"`
	Executable              bool   `json:"executable"`
	Periodic                bool   `json:"periodic"`
	AllowRunNow             bool   `json:"allowRunNow"`
	AllowRetry              bool   `json:"allowRetry"`
	DefaultMaxRetries       int    `json:"defaultMaxRetries"`
	ConcurrencyPolicy       string `json:"concurrencyPolicy"`
	StaleQueuedAfterSeconds int64  `json:"staleQueuedAfterSeconds"`
	PeriodicIntervalSeconds int64  `json:"periodicIntervalSeconds"`
}

func NewWorker(service *Service) *Worker {
	return &Worker{
		service:  service,
		manager:  NewManager(service),
		periodic: NewPeriodicRunner(service),
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
	w.periodic.Start(ctx)
	w.wg.Add(3)
	go w.queueLoop(ctx)
	go w.staleQueuedLoop(ctx)
	go w.orphanLoop(ctx)
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
	if w.periodic != nil {
		w.periodic.Wait()
	}
	w.wg.Wait()
}

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
		if def.Periodic != nil {
			stats.PeriodicTypes++
		}
		detail := RuntimeDefinitionStats{
			Type:                    def.Type,
			Hidden:                  def.Hidden,
			Executable:              def.Execute != nil,
			Periodic:                def.Periodic != nil,
			AllowRunNow:             def.AllowRunNow && def.Execute != nil,
			AllowRetry:              def.AllowRetry && def.Execute != nil,
			DefaultMaxRetries:       def.DefaultMaxRetries,
			ConcurrencyPolicy:       def.ConcurrencyPolicy,
			StaleQueuedAfterSeconds: int64(def.StaleQueuedAfter.Seconds()),
		}
		if def.Periodic != nil {
			detail.PeriodicIntervalSeconds = int64(def.Periodic.Interval.Seconds())
		}
		stats.Definitions = append(stats.Definitions, detail)
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
	return w.manager.Run(ctx, task)
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
	for _, task := range w.dueTasks(ctx) {
		if task.Status == StatusRunning && w.service.HasRunningExecution(task.ID) {
			continue
		}
		if err := w.RunNow(ctx, task); err != nil {
			log.Printf("task worker run task %s: %v", task.ID, err)
		}
	}
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
		for _, status := range []string{StatusQueued, StatusScheduled, StatusFailedRetryable} {
			result, err := w.service.List(ctx, ListFilter{Type: taskType, Status: status, Limit: 50, IncludeInternal: true})
			if err != nil {
				log.Printf("task worker list %s/%s: %v", taskType, status, err)
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
			log.Printf("task stale-queued cleanup: %v", err)
			continue
		}
		if expired > 0 {
			log.Printf("task stale-queued cleanup marked %d task(s) failed", expired)
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

func (w *Worker) failOrphanedRunning(ctx context.Context) {
	failed, err := w.service.FailRunningWithoutExecution(ctx, time.Now().UTC())
	if err != nil {
		log.Printf("task running execution check: %v", err)
		return
	}
	if failed > 0 {
		log.Printf("task running execution check marked %d orphaned task(s) failed", failed)
	}
}
