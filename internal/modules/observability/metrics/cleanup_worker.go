package metrics

import (
	"context"
	"sync"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

type CleanupSettings struct {
	RetentionDays int
	Schedule      string
}

type CleanupWorker struct {
	mu       sync.Mutex
	service  *Service
	settings func() CleanupSettings
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewCleanupWorker(service *Service, settings func() CleanupSettings) *CleanupWorker {
	return &CleanupWorker{service: service, settings: settings}
}

func (w *CleanupWorker) Start(parent context.Context) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.service == nil || w.settings == nil || w.cancel != nil {
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.wg.Wait()
}

func (w *CleanupWorker) Running() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cancel != nil
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
			lastRun, _ = w.tick(ctx, lastRun, time.Now())
		}
	}
}

// tick 按 provider 当前下发的清理设置决定本次是否执行清理。provider 由 app.go
// 接到指标设施（metricsfacility.CleanupSettings），因此保留天数与清理计划都来自
// 设施配置；结构本身不感知配置来源，读失败由 provider 自己回退默认值。
// 返回本次 tick 后的 lastRun 以及是否尝试过清理，便于测试直接驱动一轮 tick，
// 不必等待真实的 1h/24h 清理周期。
func (w *CleanupWorker) tick(ctx context.Context, lastRun, now time.Time) (time.Time, bool) {
	if w == nil || w.service == nil || w.settings == nil {
		return lastRun, false
	}
	settings := w.settings()
	if !cleanupDue(lastRun, now, settings.Schedule) {
		return lastRun, false
	}
	tickCtx := logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "metrics"})
	deleted, err := w.service.Cleanup(tickCtx, settings.RetentionDays)
	if err != nil {
		logging.From(tickCtx).Warn("metrics cleanup failed", zap.Error(err))
		return now, true
	}
	logging.From(tickCtx).Info("metrics cleanup deleted rows", zap.Int64("deleted", deleted))
	return now, true
}

// cleanupDue 判断距离上次清理是否已经达到清理计划对应的周期。
func cleanupDue(lastRun, now time.Time, schedule string) bool {
	return now.Sub(lastRun) >= cleanupInterval(schedule)
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
