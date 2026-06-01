package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"panel/internal/metrics"
	"panel/internal/packages"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/settings"
	"panel/internal/tasks"
)

type Scheduler struct {
	settings *settings.Service
	servers  *server.Service
	metrics  *metrics.Service
	packages *packages.Service
	tasks    *tasks.Service
	certs    certificateRenewer
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type certificateRenewer interface {
	RenewDue(ctx context.Context, now time.Time) (int, error)
	RunDueIssueTasks(ctx context.Context) (int, error)
	RunIssueTask(ctx context.Context, task tasks.Task) error
}

func New(settings *settings.Service, servers *server.Service, metrics *metrics.Service, packages *packages.Service, tasks *tasks.Service) *Scheduler {
	return &Scheduler{settings: settings, servers: servers, metrics: metrics, packages: packages, tasks: tasks}
}

func (s *Scheduler) SetCertificateRenewer(renewer certificateRenewer) {
	s.certs = renewer
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(5)
	go s.connectivityLoop(ctx)
	go s.metricsLoop(ctx)
	go s.packageLoop(ctx)
	go s.cleanupLoop(ctx)
	go s.certificateLoop(ctx)
}

func (s *Scheduler) connectivityLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastRun := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(lastRun) < 5*time.Second {
				continue
			}
			lastRun = time.Now()
			if err := s.runDueConnectivityTests(ctx); err != nil {
				log.Printf("scheduler run connectivity tests: %v", err)
			}
		}
	}
}

func (s *Scheduler) runDueConnectivityTests(ctx context.Context) error {
	return s.servers.RunDueConnectivityTests(ctx)
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
	case "package_refresh":
		_, err := s.packages.Refresh(ctx, task.ServerID)
		return err
	case "certificate_issue":
		if s.certs == nil {
			return panelerr.Validation("task_run_now_unsupported", "Certificate issuer is not configured")
		}
		return s.certs.RunIssueTask(ctx, task)
	}

	return panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center")
}

func (s *Scheduler) certificateLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastRenewal := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if s.certs == nil {
				continue
			}
			if issued, err := s.certs.RunDueIssueTasks(ctx); err != nil {
				log.Printf("certificate issue task: %v", err)
			} else if issued > 0 {
				log.Printf("certificate issue completed for %d certificate(s)", issued)
			}
			if time.Since(lastRenewal) < time.Hour {
				continue
			}
			lastRenewal = now
			if renewed, err := s.certs.RenewDue(ctx, now); err != nil {
				log.Printf("certificate renewal: %v", err)
			} else if renewed > 0 {
				log.Printf("certificate renewal completed for %d certificate(s)", renewed)
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
			if err := s.runDueMetricsCollection(ctx); err != nil {
				log.Printf("scheduler collect metrics: %v", err)
			}
		}
	}
}

func (s *Scheduler) collectMetrics(ctx context.Context, srv server.Server) error {
	return s.collectMetricsAt(ctx, srv, time.Now().UTC().Truncate(time.Second))
}

func (s *Scheduler) runDueMetricsCollection(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable {
			continue
		}
		if err := s.collectMetricsAt(ctx, srv, collectedAt); err != nil {
			log.Printf("metrics collect server %s: %v", srv.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) collectMetricsAt(ctx context.Context, srv server.Server, collectedAt time.Time) error {
	if err := s.metrics.CollectAt(ctx, srv.ID, collectedAt); err != nil {
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
