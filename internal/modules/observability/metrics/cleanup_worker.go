package metrics

import (
	"context"
	"log"
	"sync"
	"time"
)

type CleanupSettings struct {
	RetentionDays int
	Schedule      string
}

type CleanupWorker struct {
	service  *Service
	settings func() CleanupSettings
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewCleanupWorker(service *Service, settings func() CleanupSettings) *CleanupWorker {
	return &CleanupWorker{service: service, settings: settings}
}

func (w *CleanupWorker) Start(parent context.Context) {
	if w == nil || w.service == nil || w.settings == nil || w.cancel != nil {
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
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings := w.settings()
			if time.Since(lastRun) < cleanupInterval(settings.Schedule) {
				continue
			}
			lastRun = time.Now()
			deleted, err := w.service.Cleanup(ctx, settings.RetentionDays)
			if err != nil {
				log.Printf("metrics cleanup: %v", err)
				continue
			}
			log.Printf("metrics cleanup deleted %d rows", deleted)
		}
	}
}

func cleanupInterval(schedule string) time.Duration {
	switch schedule {
	case "hourly":
		return time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
