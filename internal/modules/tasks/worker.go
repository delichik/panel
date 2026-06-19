package tasks

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	defaultQueuePollInterval   = 5 * time.Second
	defaultCleanupInterval     = time.Second
	defaultOrphanCheckInterval = 5 * time.Second
)

type Worker struct {
	service  *Service
	manager  *Manager
	periodic *PeriodicRunner
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewWorker(service *Service) *Worker {
	return &Worker{
		service:  service,
		manager:  NewManager(service),
		periodic: NewPeriodicRunner(service),
	}
}

func (w *Worker) Start(parent context.Context) {
	if w == nil || w.service == nil || w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
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
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.periodic != nil {
		w.periodic.Wait()
	}
	w.wg.Wait()
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
