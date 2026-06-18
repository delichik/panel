package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/certificates/certs"
	"panel/internal/modules/containers"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Scheduler struct {
	settings   *settings.Service
	servers    *server.Service
	metrics    *metrics.Service
	packages   *packages.Service
	tasks      *tasks.Service
	certs      certificateRenewer
	containers *containerization.Service
	periodic   *tasks.PeriodicRunner
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

const StaleQueuedWorkerTaskAfter = 10 * time.Minute
const RunningTaskCheckInterval = 5 * time.Second

var staleQueuedWorkerTaskTypes = []string{
	"server_ufw_install",
}

type certificateRenewer interface {
	RunIssueTask(ctx context.Context, task tasks.Task) error
	RenewTask(ctx context.Context, task tasks.Task) error
}

func New(settings *settings.Service, servers *server.Service, metrics *metrics.Service, packages *packages.Service, tasks *tasks.Service, opts ...Option) *Scheduler {
	s := &Scheduler{settings: settings, servers: servers, metrics: metrics, packages: packages, tasks: tasks}
	for _, opt := range opts {
		opt(s)
	}
	s.registerTaskExecutors()
	s.registerPeriodicTasks()
	return s
}

type Option func(*Scheduler)

func WithCertificateRenewer(renewer certificateRenewer) Option {
	return func(s *Scheduler) { s.certs = renewer }
}

func WithContainerization(service *containerization.Service) Option {
	return func(s *Scheduler) { s.containers = service }
}

func (s *Scheduler) registerTaskExecutors() {
	if s.tasks == nil {
		return
	}
	register := func(taskType string, execute func(tasks.TaskContext) error) {
		def, ok := s.tasks.Registry().Definition(taskType)
		if !ok {
			return
		}
		def.Execute = execute
		s.tasks.Registry().Replace(def)
	}
	register("server_connectivity_test", func(tc tasks.TaskContext) error {
		return s.servers.RunConnectivityTask(tc.Context, tc.Task)
	})
	register("server_info_collect", func(tc tasks.TaskContext) error {
		return s.servers.RunConnectivityTask(tc.Context, tc.Task)
	})
	register("server_agent_deploy", func(tc tasks.TaskContext) error {
		return s.servers.RunAgentDeployTask(tc.Context, tc.Task)
	})
	register("server_agent_check", func(tc tasks.TaskContext) error {
		return s.runAgentCheckTask(tc.Context, tc.Task)
	})
	register("task_queue_drain", func(tc tasks.TaskContext) error {
		return s.runQueueDrainTask(tc.Context, tc.Task)
	})
	register("package_refresh", func(tc tasks.TaskContext) error {
		return s.packages.RunRefreshTask(tc.Context, tc.Task)
	})
	register("metrics_collect", func(tc tasks.TaskContext) error {
		return s.runMetricsCollectTask(tc.Context, tc.Task)
	})
	if s.certs != nil {
		register("certificate_issue", func(tc tasks.TaskContext) error {
			return s.certs.RunIssueTask(tc.Context, tc.Task)
		})
		register("certificate_renew", func(tc tasks.TaskContext) error {
			return s.certs.RenewTask(tc.Context, tc.Task)
		})
	}
	if s.containers != nil {
		register("image_refresh", func(tc tasks.TaskContext) error {
			return s.containers.RunImageRefreshTask(tc.Context, tc.Task)
		})
		register("application_reconcile", func(tc tasks.TaskContext) error {
			return s.containers.RunApplicationReconcileTask(tc.Context, tc.Task)
		})
	}
}

func (s *Scheduler) registerPeriodicTasks() {
	if s.tasks == nil {
		return
	}
	registeredAt := time.Now()
	var lastMetricsRun time.Time
	var lastPackageRun time.Time
	var lastImageRun time.Time
	var lastRenewalRun time.Time
	register := func(def tasks.Definition) {
		existing, _ := s.tasks.Registry().Definition(def.Type)
		if def.Summary == "" {
			def.Summary = existing.Summary
		}
		def.Hidden = existing.Hidden || def.Hidden
		def.AllowRunNow = existing.AllowRunNow
		def.AllowRetry = existing.AllowRetry
		def.DefaultMaxRetries = existing.DefaultMaxRetries
		if def.ConcurrencyPolicy == "" {
			def.ConcurrencyPolicy = existing.ConcurrencyPolicy
		}
		if def.ConcurrencyKey == nil {
			def.ConcurrencyKey = existing.ConcurrencyKey
		}
		if def.Validate == nil {
			def.Validate = existing.Validate
		}
		if def.BeforeStart == nil {
			def.BeforeStart = existing.BeforeStart
		}
		if def.Execute == nil {
			def.Execute = existing.Execute
		}
		if def.OnComplete == nil {
			def.OnComplete = existing.OnComplete
		}
		if def.OnFailure == nil {
			def.OnFailure = existing.OnFailure
		}
		s.tasks.Registry().Replace(def)
	}
	register(tasks.Definition{
		Type:    "server_agent_check",
		Summary: "Checking configured agents",
		Periodic: &tasks.Periodic{
			Interval: 5 * time.Minute,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				return s.collectAgentCheckInputs(ctx)
			},
		},
	})
	register(tasks.Definition{
		Type:    "metrics_collect",
		Summary: "Collecting scheduled metrics",
		Periodic: &tasks.Periodic{
			Interval: time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
				if lastMetricsRun.IsZero() {
					lastMetricsRun = registeredAt
				}
				if time.Since(lastMetricsRun) < interval {
					return tasks.CreateBatchInput{}, false, nil
				}
				batch, shouldRun, err := s.collectMetricsInputs(ctx)
				if err == nil && shouldRun {
					lastMetricsRun = time.Now()
				}
				return batch, shouldRun, err
			},
		},
	})
	register(tasks.Definition{
		Type:    "package_refresh",
		Summary: "Refreshing scheduled packages",
		Periodic: &tasks.Periodic{
			Interval: time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
				if lastPackageRun.IsZero() {
					lastPackageRun = registeredAt
				}
				if time.Since(lastPackageRun) < interval {
					return tasks.CreateBatchInput{}, false, nil
				}
				batch, shouldRun, err := s.collectScheduledPackageRefreshInputs(ctx)
				if err == nil && shouldRun {
					lastPackageRun = time.Now()
				}
				return batch, shouldRun, err
			},
		},
	})
	register(tasks.Definition{
		Type:    "certificate_renew",
		Summary: "Renewing due certificates",
		Periodic: &tasks.Periodic{
			Interval: 5 * time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				if lastRenewalRun.IsZero() {
					lastRenewalRun = registeredAt
				}
				if time.Since(lastRenewalRun) < time.Hour {
					return tasks.CreateBatchInput{}, false, nil
				}
				batch, shouldRun, err := s.collectCertificateRenewInputs(ctx, time.Now())
				if err == nil && shouldRun {
					lastRenewalRun = time.Now()
				}
				return batch, shouldRun, err
			},
		},
	})
	register(tasks.Definition{
		Type:    "image_refresh",
		Summary: "Refreshing scheduled image checks",
		Periodic: &tasks.Periodic{
			Interval: 5 * time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				interval := time.Duration(s.settings.Runtime().MetricsCollectionIntervalSeconds) * time.Second
				if lastImageRun.IsZero() {
					lastImageRun = registeredAt
				}
				if time.Since(lastImageRun) < interval {
					return tasks.CreateBatchInput{}, false, nil
				}
				batch, shouldRun, err := s.collectImageRefreshInputs(ctx)
				if err == nil && shouldRun {
					lastImageRun = time.Now()
				}
				return batch, shouldRun, err
			},
		},
	})
	register(tasks.Definition{
		Type:    "application_reconcile",
		Summary: "Monitoring application containers",
		Periodic: &tasks.Periodic{
			Interval: 5 * time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				return s.collectApplicationReconcileInputs(ctx)
			},
		},
	})
	register(tasks.Definition{
		Type:    "task_queue_drain",
		Summary: "Running due queued tasks",
		Periodic: &tasks.Periodic{
			Interval: 5 * time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				return s.collectQueueDrainInput(ctx)
			},
		},
	})
}

func (s *Scheduler) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.periodic = tasks.NewPeriodicRunner(s.tasks)
	s.periodic.Start(ctx)
	s.wg.Add(2)
	go s.cleanupLoop(ctx)
	go s.runningTaskLoop(ctx)
}

func (s *Scheduler) collectAgentCheckInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.servers == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	inputs := []tasks.CreateInput{}
	operationID := id.New("op")
	for _, srv := range servers {
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "server_agent_check",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      "Checking agent for " + srv.Name,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "server_agent_check", OperationID: operationID, TriggerType: "scheduler", Summary: "Checking configured agents", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Scheduler) collectMetricsInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.metrics == nil || s.servers == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable || !schedulerAgentReady(srv) {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "metrics_collect",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			ParamsJSON:   `{"collectedAt":"` + collectedAt.Format(time.RFC3339Nano) + `"}`,
			Summary:      "Collecting metrics for " + srv.Name,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "metrics_collect", OperationID: operationID, TriggerType: "scheduler", Summary: "Collecting scheduled metrics", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Scheduler) collectScheduledPackageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.packages == nil || s.servers == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable || s.hasRecentTask(ctx, "package_refresh", srv.ID, 10*time.Minute) {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "package_refresh",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      "Refreshing package updates",
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "package_refresh", OperationID: operationID, TriggerType: "scheduler", Summary: "Refreshing scheduled packages", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Scheduler) collectCertificateRenewInputs(ctx context.Context, now time.Time) (tasks.CreateBatchInput, bool, error) {
	if s.certs == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	lister, ok := s.certs.(interface {
		List(context.Context) ([]certs.Certificate, error)
	})
	if !ok {
		return tasks.CreateBatchInput{}, false, nil
	}
	certificates, err := lister.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, cert := range certificates {
		if !cert.AutoRenew || cert.NextRenewAt.IsZero() || cert.NextRenewAt.After(now) {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "certificate_renew",
			ResourceType: "certificate",
			ResourceID:   cert.ID,
			TriggerType:  "scheduler",
			MetadataJSON: certMetadataJSON(cert),
			Summary:      "Renewing certificate for " + cert.Domain,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "certificate_renew", OperationID: operationID, TriggerType: "scheduler", Summary: "Renewing due certificates", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func certMetadataJSON(cert certs.Certificate) string {
	data, err := json.Marshal(map[string]any{
		"certificateId": cert.ID,
		"domain":        cert.Domain,
		"domains":       cert.Domains,
		"issuer":        cert.Issuer,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *Scheduler) collectImageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.containers == nil || s.servers == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, srv := range servers {
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "image_refresh",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      "Refreshing image updates",
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "image_refresh", OperationID: operationID, TriggerType: "scheduler", Summary: "Refreshing scheduled image checks", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Scheduler) collectApplicationReconcileInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.containers == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	collector, ok := any(s.containers).(interface {
		CollectApplicationReconcileTasks(context.Context, string) ([]tasks.CreateInput, error)
	})
	if !ok {
		return tasks.CreateBatchInput{}, false, nil
	}
	operationID := id.New("op")
	inputs, err := collector.CollectApplicationReconcileTasks(ctx, operationID)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "application_reconcile", OperationID: operationID, TriggerType: "scheduler", Summary: "Monitoring application containers", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

type queueDrainParams struct {
	TaskIDs []string `json:"taskIds"`
}

func (s *Scheduler) collectQueueDrainInput(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	if s.tasks == nil {
		return tasks.CreateBatchInput{}, false, nil
	}
	taskIDs, err := s.dueRunnableTaskIDs(ctx, "server_info_collect", "server_connectivity_test", "certificate_issue", "package_refresh")
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	if len(taskIDs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	data, err := json.Marshal(queueDrainParams{TaskIDs: taskIDs})
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	return tasks.CreateBatchInput{
		Type:          "task_queue_drain",
		OperationID:   id.New("op"),
		TriggerType:   "scheduler",
		Summary:       "Running due queued tasks",
		ExecutionMode: tasks.ExecutionModeSerial,
		Inputs: []tasks.CreateInput{{
			Type:        "task_queue_drain",
			TriggerType: "scheduler",
			ParamsJSON:  string(data),
			Summary:     "Running due queued tasks",
		}},
	}, true, nil
}

func (s *Scheduler) dueRunnableTaskIDs(ctx context.Context, taskTypes ...string) ([]string, error) {
	now := time.Now().UTC()
	taskIDs := []string{}
	seen := map[string]struct{}{}
	for _, taskType := range taskTypes {
		for _, status := range []string{tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusFailedRetryable} {
			result, err := s.tasks.List(ctx, tasks.ListFilter{Type: taskType, Status: status, Limit: 50, IncludeInternal: true})
			if err != nil {
				return nil, err
			}
			for _, task := range result.Items {
				if task.NextRunAt != nil && task.NextRunAt.After(now) {
					continue
				}
				key := task.ID
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				taskIDs = append(taskIDs, task.ID)
			}
		}
	}
	return taskIDs, nil
}

func (s *Scheduler) hasRecentTask(ctx context.Context, taskType, serverID string, window time.Duration) bool {
	if s.tasks == nil {
		return false
	}
	result, err := s.tasks.List(ctx, tasks.ListFilter{Type: taskType, ServerID: serverID, Limit: 1, IncludeInternal: true})
	if err != nil || len(result.Items) == 0 {
		return false
	}
	return time.Since(result.Items[0].CreatedAt) <= window
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.periodic != nil {
		s.periodic.Wait()
	}
	s.wg.Wait()
}

func (s *Scheduler) RunNow(ctx context.Context, task tasks.Task) error {
	def, ok := s.tasks.Registry().Definition(task.Type)
	if !ok || def.Execute == nil {
		return tasks.ErrExecutorUnavailable()
	}
	return def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: s.tasks})
}

func (s *Scheduler) runAgentCheckTask(ctx context.Context, task tasks.Task) error {
	if s.servers == nil {
		return nil
	}
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	srv, err := s.servers.Get(checkCtx, serverID)
	if err != nil {
		return err
	}
	s.servers.CheckConfiguredAgent(checkCtx, srv)
	return nil
}

func (s *Scheduler) runMetricsCollectTask(ctx context.Context, task tasks.Task) error {
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return nil
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	var params struct {
		CollectedAt string `json:"collectedAt"`
	}
	if strings.TrimSpace(task.ParamsJSON) != "" {
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err == nil && params.CollectedAt != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, params.CollectedAt); parseErr == nil {
				collectedAt = parsed
			}
		}
	}
	if err := s.metrics.CollectAt(ctx, serverID, collectedAt); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	return s.tasks.Complete(ctx, task.ID, "")
}

func (s *Scheduler) runQueueDrainTask(ctx context.Context, task tasks.Task) error {
	if s.tasks == nil {
		return nil
	}
	var params queueDrainParams
	if strings.TrimSpace(task.ParamsJSON) != "" {
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
			return err
		}
	}
	if len(params.TaskIDs) == 0 {
		return nil
	}
	for _, taskID := range params.TaskIDs {
		dueTask, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			var pe *panelerr.Error
			if errors.As(err, &pe) && pe.Code == "not_found" {
				continue
			}
			return err
		}
		if dueTask.Status == tasks.StatusRunning && s.tasks.HasRunningExecution(dueTask.ID) {
			continue
		}
		if err := s.RunNow(ctx, dueTask); err != nil {
			log.Printf("scheduler queue drain task %s: %v", dueTask.ID, err)
		}
	}
	return nil
}

func schedulerAgentReady(srv server.Server) bool {
	switch strings.ToLower(strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled])) {
	case "true", "1", "yes", "on":
		return strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) != "" && srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusCompatible
	default:
		return false
	}
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
