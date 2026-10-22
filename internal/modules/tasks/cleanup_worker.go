package tasks

import (
	"context"
	"sync"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// defaultTaskRetention is how long terminal task history (tasks, task_steps,
// task_logs) is kept before retention cleanup removes it. Terminal tasks are
// not exposed in the product UI, so a short 24h retention keeps the tables
// small and prevents invisible ghosts from slowing every queue scan and
// orphan check. The tables otherwise grow without bound.
const defaultTaskRetention = 24 * time.Hour

type CleanupWorker struct {
	service *Service
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewCleanupWorker(service *Service) *CleanupWorker {
	return &CleanupWorker{service: service}
}

func (w *CleanupWorker) Start(parent context.Context) {
	if w == nil || w.service == nil || w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.wg.Add(1)
	go w.loop(ctx)
}

func (w *CleanupWorker) Stop() {
	if w == nil {
		return
	}
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.wg.Wait()
}

func (w *CleanupWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	w.run(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.run(ctx)
		}
	}
}

func (w *CleanupWorker) run(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "tasks"})
	deleted, err := w.service.CleanupRetained(ctx, defaultTaskRetention)
	if err != nil {
		logging.From(ctx).Warn("task retention cleanup failed", zap.Error(err))
		return
	}
	if deleted > 0 {
		logging.From(ctx).Info("task retention cleanup deleted old tasks", zap.Int64("deleted", deleted))
	}
}
