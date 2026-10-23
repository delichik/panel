package tasks

import (
	"context"
	"sync"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// SchedulerLoop owns one goroutine that periodically triggers a registered
// periodic task type via Manager.TriggerPeriodicNow. Interval lives in the
// calling module, not on Definition.Periodic.
type SchedulerLoop struct {
	service  *Service
	taskType string
	interval time.Duration
	module   string
	wg       sync.WaitGroup
}

// StartSchedulerLoop starts a delayed-first-tick loop for taskType.
// The first trigger waits one full interval (thundering-herd guard on restart).
func StartSchedulerLoop(ctx context.Context, service *Service, taskType string, interval time.Duration, module string) *SchedulerLoop {
	if service == nil || interval <= 0 || taskType == "" {
		return nil
	}
	loop := &SchedulerLoop{service: service, taskType: taskType, interval: interval, module: module}
	loop.wg.Add(1)
	go func() {
		defer loop.wg.Done()
		loop.run(ctx)
	}()
	return loop
}

func (l *SchedulerLoop) Wait() {
	if l == nil {
		return
	}
	l.wg.Wait()
}

func (l *SchedulerLoop) run(ctx context.Context) {
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	manager := NewManager(l.service)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx := logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: l.module})
			_, _, err := manager.TriggerPeriodicNow(tickCtx, l.taskType, PeriodicTrigger{Type: "scheduler"})
			if err != nil {
				logging.From(tickCtx).Warn("periodic task trigger failed", zap.String("task_type", l.taskType), zap.Error(err))
			}
		}
	}
}

// TriggerSchedulerOnce runs one scheduler trigger (tests / manual harness).
func TriggerSchedulerOnce(ctx context.Context, service *Service, taskType string) (Task, bool, error) {
	return NewManager(service).TriggerPeriodicNow(ctx, taskType, PeriodicTrigger{Type: "scheduler"})
}
