package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"panel/internal/containerops"
	"panel/internal/docker"
	"panel/internal/metrics"
	"panel/internal/packages"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/tasks"
)

type Scheduler struct {
	settings     *settings.Service
	servers      *server.Service
	metrics      *metrics.Service
	docker       *docker.Service
	packages     *packages.Service
	tasks        *tasks.Service
	containerOps *containerops.Worker
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func New(settings *settings.Service, servers *server.Service, metrics *metrics.Service, dockerSvc *docker.Service, packages *packages.Service, tasks *tasks.Service, containerWorker *containerops.Worker) *Scheduler {
	return &Scheduler{settings: settings, servers: servers, metrics: metrics, docker: dockerSvc, packages: packages, tasks: tasks, containerOps: containerWorker}
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(5)
	go s.metricsLoop(ctx)
	go s.dockerLoop(ctx)
	go s.packageLoop(ctx)
	go s.cleanupLoop(ctx)
	go s.containerServicesLoop(ctx)
}

func (s *Scheduler) dockerLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticks++
			if err := s.docker.RefreshContainersReachable(ctx); err != nil {
				log.Printf("docker containers refresh: %v", err)
			}
			if ticks%10 == 0 {
				if err := s.docker.RefreshReachable(ctx); err != nil {
					log.Printf("docker refresh: %v", err)
				}
				if err := s.servers.RunDueConnectivityTests(ctx); err != nil {
					log.Printf("server connectivity reconcile: %v", err)
				}
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

func (s *Scheduler) RunNow(ctx context.Context, task tasks.Task) error {
	switch task.Type {
	case "server_connectivity_test":
		_, err := s.servers.EnsureConnectivityTask(ctx, task.ServerID, true)
		return err
	case "docker_status_refresh":
		_, err := s.docker.Refresh(ctx, task.ServerID)
		return err
	case "package_refresh":
		_, err := s.packages.Refresh(ctx, task.ServerID)
		return err
	}
	if s.containerOps != nil && task.ResourceType == "container_service" {
		return s.containerOps.RunNow(ctx, task)
	}

	return panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center")
}

func (s *Scheduler) containerServicesLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.containerOps == nil {
				continue
			}
			if err := s.containerOps.RunDue(ctx); err != nil {
				log.Printf("container services worker: %v", err)
			}
		}
	}
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
	if err := s.metrics.Collect(ctx, srv.ID); err != nil {
		return err
	}
	return nil
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
