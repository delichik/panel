package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"panel/internal/docker"
	"panel/internal/metrics"
	"panel/internal/packages"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/tasks"
)

type Scheduler struct {
	settings *settings.Service
	servers  *server.Service
	metrics  *metrics.Service
	docker   *docker.Service
	packages *packages.Service
	tasks    *tasks.Service
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func New(settings *settings.Service, servers *server.Service, metrics *metrics.Service, dockerSvc *docker.Service, packages *packages.Service, tasks *tasks.Service) *Scheduler {
	return &Scheduler{settings: settings, servers: servers, metrics: metrics, docker: dockerSvc, packages: packages, tasks: tasks}
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(4)
	go s.metricsLoop(ctx)
	go s.dockerLoop(ctx)
	go s.packageLoop(ctx)
	go s.cleanupLoop(ctx)
}

func (s *Scheduler) dockerLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Now().Add(-time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
			if interval < 5*time.Minute {
				interval = 5 * time.Minute
			}
			if time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			if err := s.docker.RefreshReachable(ctx); err != nil {
				log.Printf("docker refresh: %v", err)
			}
		}
	}
}

func (s *Scheduler) packageLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
			if time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			servers, err := s.servers.List(ctx)
			if err != nil {
				log.Printf("scheduler list servers for packages: %v", err)
				continue
			}
			for _, srv := range servers {
				if !srv.OS.Supported || !srv.Reachable {
					continue
				}
				if _, err := s.packages.Refresh(ctx, srv.ID); err != nil {
					log.Printf("package refresh server %s: %v", srv.ID, err)
				}
			}
		}
	}
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) metricsLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
			if time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			servers, err := s.servers.List(ctx)
			if err != nil {
				log.Printf("scheduler list servers: %v", err)
				continue
			}
			for _, srv := range servers {
				if !srv.OS.Supported || !srv.Reachable {
					continue
				}
				if err := s.collectMetrics(ctx, srv); err != nil {
					log.Printf("metrics collect server %s: %v", srv.ID, err)
				}
			}
		}
	}
}

func (s *Scheduler) collectMetrics(ctx context.Context, srv server.Server) error {
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:     "metrics_collect",
		ServerID: srv.ID,
		Summary:  "Collecting server metrics",
	})
	if err != nil {
		return err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	if err := s.tasks.Advance(ctx, task.ID, "collecting", "Collecting CPU, memory, disk, and network metrics"); err != nil {
		return err
	}
	if err := s.metrics.Collect(ctx, srv.ID); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	_ = s.tasks.AppendLog(ctx, task.ID, "system", "metrics collection completed")
	return s.tasks.Complete(ctx, task.ID, "Metrics collection completed")
}

func (s *Scheduler) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime := s.settings.Runtime()
			interval := cleanupInterval(runtime.CleanupSchedule)
			if time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			deleted, err := s.metrics.Cleanup(ctx, runtime.MetricsRetentionDays)
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
