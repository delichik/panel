package runtimeevents

import (
	"context"
	"log"
	"time"
)

type CleanupSettings struct {
	RetentionDays       int
	DetailRetentionDays int
	Schedule            string
}

type CleanupWorker struct {
	service  *Service
	settings func() CleanupSettings
	cancel   context.CancelFunc
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
	go w.loop(ctx)
}

func (w *CleanupWorker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.cancel = nil
}

func (w *CleanupWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastRun time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings := w.settings()
			if time.Since(lastRun) < cleanupInterval(settings.Schedule) {
				continue
			}
			result, err := w.service.Cleanup(ctx, settings.RetentionDays, settings.DetailRetentionDays)
			if err != nil {
				log.Printf("runtime event cleanup failed: %v", err)
				continue
			}
			lastRun = time.Now()
			if result.DetailsPruned > 0 || result.EventsDeleted > 0 {
				log.Printf("runtime event cleanup pruned_details=%d deleted_events=%d", result.DetailsPruned, result.EventsDeleted)
			}
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
