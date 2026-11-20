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

// DailyAtLoop owns one goroutine that triggers a periodic task type once per
// calendar day at a fixed local wall-clock hour:minute.
type DailyAtLoop struct {
	service  *Service
	taskType string
	hour     int
	minute   int
	loc      *time.Location
	module   string
	wg       sync.WaitGroup
}

// StartDailyAtLoop starts a wall-clock daily loop for taskType.
// The first trigger waits until the next occurrence of hour:minute in loc
// (nil loc uses time.Local); restart never fires immediately. After each run
// the next occurrence is recomputed so DST shifts do not accumulate drift.
func StartDailyAtLoop(ctx context.Context, service *Service, taskType string, hour, minute int, loc *time.Location, module string) *DailyAtLoop {
	if service == nil || taskType == "" || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	loop := &DailyAtLoop{service: service, taskType: taskType, hour: hour, minute: minute, loc: loc, module: module}
	loop.wg.Add(1)
	go func() {
		defer loop.wg.Done()
		loop.run(ctx)
	}()
	return loop
}

func (l *DailyAtLoop) Wait() {
	if l == nil {
		return
	}
	l.wg.Wait()
}

func (l *DailyAtLoop) run(ctx context.Context) {
	manager := NewManager(l.service)
	for {
		delay := time.Until(nextDailyAt(time.Now(), l.hour, l.minute, l.loc))
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			tickCtx := logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: l.module})
			_, _, err := manager.TriggerPeriodicNow(tickCtx, l.taskType, PeriodicTrigger{Type: "scheduler"})
			if err != nil {
				logging.From(tickCtx).Warn("daily-at task trigger failed", zap.String("task_type", l.taskType), zap.Error(err))
			}
		}
	}
}

// nextDailyAt returns the next wall-clock occurrence of hour:minute in loc that
// is strictly after now. If now is exactly the target time, the result is tomorrow.
func nextDailyAt(now time.Time, hour, minute int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
