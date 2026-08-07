package tasks

import (
	"context"
	"log"
	"sync"
	"time"
)

// defaultTaskRetention is how long terminal task history (tasks, task_steps,
// task_logs) is kept before retention cleanup removes it. The tables otherwise
// grow without bound, which makes every queue scan and orphan check slower.
const defaultTaskRetention = 30 * 24 * time.Hour

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
	deleted, err := w.service.CleanupRetained(ctx, defaultTaskRetention)
	if err != nil {
		log.Printf("task retention cleanup: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("task retention cleanup deleted %d old task(s)", deleted)
	}
}
