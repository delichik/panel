package tasks

import (
	"context"
	"log"
	"sync"
	"time"
)

type PeriodicRunner struct {
	manager *Manager
	wg      sync.WaitGroup
}

func NewIntervalCollector(defaultInterval time.Duration, intervalProvider func() time.Duration, collect func(context.Context) (CreateBatchInput, bool, error)) func(context.Context, PeriodicTrigger) (CreateBatchInput, bool, error) {
	var mu sync.Mutex
	lastRun := time.Now()
	return func(ctx context.Context, trigger PeriodicTrigger) (CreateBatchInput, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		interval := defaultInterval
		if intervalProvider != nil {
			if configured := intervalProvider(); configured > 0 {
				interval = configured
			}
		}
		if isSchedulerPeriodicTrigger(trigger) && time.Since(lastRun) < interval {
			return CreateBatchInput{}, false, nil
		}
		batch, shouldRun, err := collect(ctx)
		if err == nil && shouldRun {
			lastRun = time.Now()
		}
		return batch, shouldRun, err
	}
}

func NewPeriodicRunner(service *Service) *PeriodicRunner {
	return &PeriodicRunner{manager: NewManager(service)}
}

func (r *PeriodicRunner) Start(ctx context.Context) {
	if r == nil || r.manager == nil || r.manager.service == nil {
		return
	}
	for _, taskType := range r.manager.service.Registry().Types() {
		def, ok := r.manager.service.Registry().Definition(taskType)
		if !ok || def.Periodic == nil || def.Periodic.Interval <= 0 {
			continue
		}
		r.wg.Add(1)
		go func(def Definition) {
			defer r.wg.Done()
			r.loop(ctx, def)
		}(def)
	}
}

func (r *PeriodicRunner) Wait() {
	if r == nil {
		return
	}
	r.wg.Wait()
}

func (r *PeriodicRunner) loop(ctx context.Context, def Definition) {
	ticker := time.NewTicker(def.Periodic.Interval)
	defer ticker.Stop()
	// 首 tick 前不立即执行，避免 Panel 重启瞬间所有周期任务同时触发（惊群）；
	// 各周期类型首个执行统一推迟到第一个 interval 之后。
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.run(ctx, def)
		}
	}
}

func (r *PeriodicRunner) run(ctx context.Context, def Definition) {
	if def.Periodic == nil || def.Periodic.CollectInputs == nil {
		return
	}
	_, _, err := r.manager.TriggerPeriodicNow(ctx, def.Type, PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		log.Printf("periodic task %s trigger: %v", def.Type, err)
		return
	}
}

func isSchedulerPeriodicTrigger(trigger PeriodicTrigger) bool {
	return trigger.Type == "" || trigger.Type == "scheduler"
}
