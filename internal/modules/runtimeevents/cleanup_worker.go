package runtimeevents

import (
	"context"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

type CleanupSettings struct {
	RetentionDays int
	Schedule      string
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
			tickCtx := logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "runtimeevents"})
			result, err := w.service.Cleanup(tickCtx, settings.RetentionDays)
			if err != nil {
				logging.From(tickCtx).Warn("runtime event cleanup failed", zap.Error(err))
				continue
			}
			lastRun = time.Now()
			if result.EventsDeleted > 0 {
				logging.From(tickCtx).Info("runtime event cleanup deleted events", zap.Int("deleted_events", result.EventsDeleted))
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
