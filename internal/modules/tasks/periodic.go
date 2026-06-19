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

func NewIntervalCollector(defaultInterval time.Duration, intervalProvider func() time.Duration, collect func(context.Context) (CreateBatchInput, bool, error)) func(context.Context) (CreateBatchInput, bool, error) {
	var mu sync.Mutex
	lastRun := time.Now()
	return func(ctx context.Context) (CreateBatchInput, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		interval := defaultInterval
		if intervalProvider != nil {
			if configured := intervalProvider(); configured > 0 {
				interval = configured
			}
		}
		if time.Since(lastRun) < interval {
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
	r.run(ctx, def)
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
	if def.Periodic == nil || def.Periodic.CollectInputs == nil || def.Execute == nil {
		return
	}
	batch, shouldRun, err := def.Periodic.CollectInputs(ctx)
	if err != nil {
		log.Printf("periodic task %s inputs: %v", def.Type, err)
		return
	}
	if !shouldRun {
		return
	}
	if batch.Type == "" {
		batch.Type = def.Type
	}
	if batch.TriggerType == "" {
		batch.TriggerType = "scheduler"
	}
	_, created, err := r.manager.CreateBatchAndRun(ctx, batch, Trigger{Type: batch.TriggerType, Periodic: true})
	if err != nil {
		log.Printf("periodic task %s create: %v", def.Type, err)
		return
	}
	if !created {
		return
	}
}
