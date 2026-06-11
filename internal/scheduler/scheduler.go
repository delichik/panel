package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"panel/internal/id"
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

const StaleQueuedWorkerTaskAfter = 10 * time.Minute
const RunningTaskCheckInterval = 5 * time.Second

var staleQueuedWorkerTaskTypes = []string{
	"nomad_client_join",
	"nomad_server_bootstrap",
	"nomad_node_remove",
	"nomad_cluster_rebuild",
	"nomad_server_switch",
	"server_ufw_install",
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
	s.wg.Add(6)
	go s.connectivityLoop(ctx)
	go s.metricsLoop(ctx)
	go s.packageLoop(ctx)
	go s.cleanupLoop(ctx)
	go s.certificateLoop(ctx)
	go s.runningTaskLoop(ctx)
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
			if err := s.runDuePackageRefreshTasks(ctx); err != nil {
				log.Printf("package refresh task: %v", err)
			}
			interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
			if time.Since(lastRun) < interval {
				continue
			}
			lastRun = time.Now()
			if err := s.runScheduledPackageRefreshes(ctx); err != nil {
				log.Printf("scheduler list servers for packages: %v", err)
			}
		}
	}
}

func (s *Scheduler) runScheduledPackageRefreshes(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	operationID := id.New("op")
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable {
			continue
		}
		if _, err := s.packages.RefreshScheduled(ctx, srv.ID, operationID); err != nil {
			log.Printf("package refresh server %s: %v", srv.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) RunNow(ctx context.Context, task tasks.Task) error {
	switch task.Type {
	case "server_connectivity_test", "server_info_collect":
		return s.servers.RunConnectivityTask(ctx, task)
	case "package_refresh":
		if s.packages == nil {
			return panelerr.Validation("task_run_now_unsupported", "Package refresh runner is not configured")
		}
		return s.packages.RunRefreshTask(ctx, task)
	case "certificate_issue":
		if s.certs == nil {
			return panelerr.Validation("task_run_now_unsupported", "Certificate issuer is not configured")
		}
		return s.certs.RunIssueTask(ctx, task)
	}

	return panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center")
}

func (s *Scheduler) runDuePackageRefreshTasks(ctx context.Context) error {
	if s.packages == nil || s.tasks == nil {
		return nil
	}
	now := time.Now().UTC()
	startedByServer := map[string]struct{}{}
	for _, status := range []string{tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusFailedRetryable} {
		result, err := s.tasks.List(ctx, tasks.ListFilter{Type: "package_refresh", Status: status, Limit: 50})
		if err != nil {
			return err
		}
		for _, task := range result.Items {
			if task.NextRunAt != nil && task.NextRunAt.After(now) {
				continue
			}
			serverID := firstNonEmpty(task.ServerID, task.ResourceID)
			if serverID == "" {
				continue
			}
			if _, ok := startedByServer[serverID]; ok {
				continue
			}
			if err := s.packages.RunRefreshTask(ctx, task); err != nil {
				return err
			}
			startedByServer[serverID] = struct{}{}
		}
	}
	return nil
}

func (s *Scheduler) CanRun(task tasks.Task) bool {
	switch task.Type {
	case "server_connectivity_test", "server_info_collect":
		return s.servers != nil
	case "package_refresh":
		return s.packages != nil
	case "certificate_issue":
		return s.certs != nil
	default:
		return false
	}
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
	return s.collectMetricsAt(ctx, srv, time.Now().UTC().Truncate(time.Second), "")
}

func (s *Scheduler) runDueMetricsCollection(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	operationID := id.New("op")
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable {
			continue
		}
		if err := s.collectMetricsAt(ctx, srv, collectedAt, operationID); err != nil {
			log.Printf("metrics collect server %s: %v", srv.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) collectMetricsAt(ctx context.Context, srv server.Server, collectedAt time.Time, operationID string) error {
	if s.tasks == nil {
		return s.metrics.CollectAt(ctx, srv.ID, collectedAt)
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		OperationID:  operationID,
		Type:         "metrics_collect",
		ServerID:     srv.ID,
		ResourceType: "server",
		ResourceID:   srv.ID,
		TriggerType:  "scheduler",
		Status:       tasks.StatusRunning,
	})
	if err != nil {
		return err
	}
	defer s.tasks.FinishExecution(task.ID)
	_ = s.tasks.Advance(ctx, task.ID, "running", "")
	if err := s.metrics.CollectAt(ctx, srv.ID, collectedAt); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	return s.tasks.Complete(ctx, task.ID, "")
}

func (s *Scheduler) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	s.expireStaleQueuedWorkerTasks(ctx)
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
			s.expireStaleQueuedWorkerTasks(ctx)
			deleted, err := s.metrics.Cleanup(ctx, runtime.MetricsRetentionDays)
			if err != nil {
				log.Printf("metrics cleanup: %v", err)
				continue
			}
			log.Printf("metrics cleanup deleted %d rows", deleted)
		}
	}
}

func (s *Scheduler) expireStaleQueuedWorkerTasks(ctx context.Context) {
	if s.tasks == nil {
		return
	}
	expired, err := s.tasks.ExpireStaleQueued(ctx, time.Now().UTC(), StaleQueuedWorkerTaskAfter, staleQueuedWorkerTaskTypes)
	if err != nil {
		log.Printf("task stale-queued cleanup: %v", err)
		return
	}
	if expired > 0 {
		log.Printf("task stale-queued cleanup marked %d task(s) failed", expired)
	}
}

func (s *Scheduler) runningTaskLoop(ctx context.Context) {
	defer s.wg.Done()
	s.failRunningTasksWithoutExecution(ctx)
	ticker := time.NewTicker(RunningTaskCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.failRunningTasksWithoutExecution(ctx)
		}
	}
}

func (s *Scheduler) failRunningTasksWithoutExecution(ctx context.Context) {
	if s.tasks == nil {
		return
	}
	expired, err := s.tasks.FailRunningWithoutExecution(ctx, time.Now().UTC())
	if err != nil {
		log.Printf("task running execution check: %v", err)
		return
	}
	if expired > 0 {
		log.Printf("task running execution check marked %d orphaned task(s) failed", expired)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
