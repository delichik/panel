package containerization

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	imageRefreshInterval         = 30 * time.Minute
	imageUnusedCleanupHour       = 3
	imageUnusedCleanupMinute     = 0
	applicationReconcileInterval = 5 * time.Second
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:       TaskImageRefresh,
			AllowRetry: true,
			Execute:    s.RunImageRefreshTask,
			Periodic: &tasks.Periodic{
				CollectInputs: func(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
					return s.CollectImageRefreshInputs(ctx)
				},
			},
		},
		{
			Type:       TaskImageUnusedCleanup,
			AllowRetry: true,
			Execute:    s.RunImageUnusedCleanupTask,
			Periodic: &tasks.Periodic{
				CollectInputs: func(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
					return s.CollectImageUnusedCleanupInputs(ctx)
				},
			},
		},
		{
			Type:       TaskVolumeRefresh,
			AllowRetry: true,
			Execute:    s.RunVolumeRefreshTask,
		},
		{Type: TaskNetworkRefresh, AllowRetry: true, Execute: s.RunNetworkRefreshTask},
		{
			// Collector-only hook for StartSchedulerLoop. CollectInputs plans Jobs
			// and returns no event-run inputs. There is no Execute.
			Type: TaskApplicationReconcile,
			Periodic: &tasks.Periodic{
				CollectInputs: s.CollectApplicationReconcileInputs,
			},
		},
		{
			Type:              TaskImageUpgradeMany,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute:           s.RunApplicationImageUpgradeTask,
		},
		{
			Type:              TaskImageUpgradeAll,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute:           s.RunApplicationImageUpgradeTask,
		},
	} {
		taskSvc.MustRegister(def)
	}
}

// StartPeriodic starts module-owned scheduler loops for image refresh, unused
// image cleanup, and application reconcile. Loops cancel with the shared
// periodic context. Interval loops delay the first tick by one full interval;
// unused-image cleanup waits until the next local 03:00 wall clock.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, TaskImageRefresh, imageRefreshInterval, "containers")
	tasks.StartDailyAtLoop(ctx, taskSvc, TaskImageUnusedCleanup, imageUnusedCleanupHour, imageUnusedCleanupMinute, time.Local, "containers")
	tasks.StartSchedulerLoop(ctx, taskSvc, TaskApplicationReconcile, applicationReconcileInterval, "containers")
}

type ApplicationReconcileTrigger struct {
	ApplicationIDs []string `json:"applicationIds"`
	ServerIDs      []string `json:"serverIds"`
	Force          bool     `json:"force"`
	Purge          bool     `json:"purge"`
	Reason         string   `json:"reason"`
	StopServers    []string `json:"stopServers"`
}

func (s *Service) RunVolumeRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Volumes refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "volumes", items)
	})
}

func (s *Service) RunNetworkRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Networks refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerNetworks(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "networks", items)
	})
}

func (s *Service) RunApplicationImageUpgradeTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	applicationIDs := strings.Split(task.ResourceID, ",")
	s.runApplicationUpdates(task, applicationIDs)
	return nil
}

func (s *Service) CollectApplicationReconcileInputs(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
	if _, err := s.CollectApplicationReconcileTasks(ctx, "", trigger); err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	return tasks.CreateBatchInput{}, false, nil
}

func applicationReconcileTriggerPayload(trigger tasks.PeriodicTrigger) ApplicationReconcileTrigger {
	out := ApplicationReconcileTrigger{}
	switch value := trigger.Payload.(type) {
	case ApplicationReconcileTrigger:
		out = value
	case *ApplicationReconcileTrigger:
		if value != nil {
			out = *value
		}
	case map[string]any:
		if raw, err := json.Marshal(value); err == nil {
			_ = json.Unmarshal(raw, &out)
		}
	}
	if trigger.TriggerResourceType == "application" && strings.TrimSpace(trigger.TriggerResourceID) != "" {
		out.ApplicationIDs = append(out.ApplicationIDs, strings.TrimSpace(trigger.TriggerResourceID))
	}
	out.ApplicationIDs = uniqueStrings(out.ApplicationIDs)
	out.ServerIDs = uniqueStrings(out.ServerIDs)
	out.StopServers = uniqueStrings(out.StopServers)
	return out
}

func (s *Service) CollectImageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	return s.collectCompatibleServerBatch(ctx, TaskImageRefresh, "Refreshing image updates", "Refreshing scheduled image checks")
}

func (s *Service) CollectImageUnusedCleanupInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	return s.collectCompatibleServerBatch(ctx, TaskImageUnusedCleanup, "Cleaning unused images older than 7 days", "Cleaning scheduled unused images")
}

func (s *Service) collectCompatibleServerBatch(ctx context.Context, taskType, itemSummary, batchSummary string) (tasks.CreateBatchInput, bool, error) {
	if s == nil || s.servers == nil {
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
			Type:         taskType,
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      itemSummary,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{
		Type:          taskType,
		OperationID:   operationID,
		TriggerType:   "scheduler",
		Summary:       batchSummary,
		ExecutionMode: tasks.ExecutionModeParallel,
		Inputs:        inputs,
	}, true, nil
}

func (s *Service) RunImageUnusedCleanupTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return panelerr.Validation("server_required", "Server is required")
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	s.runImageUnusedCleanup(s.tasks.ExecutionContext(task.ID), task, serverID)
	return nil
}

func (s *Service) runImageUnusedCleanup(ctx context.Context, task tasks.Task, serverID string) {
	defer s.tasks.FinishExecution(task.ID)
	if err := ctx.Err(); err != nil {
		return
	}
	if err := s.deleteUnusedImages(ctx, serverID, unusedImageRetention); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	if _, err := s.RefreshImages(context.WithoutCancel(ctx), serverID, "scheduler", ""); err != nil {
		// Deletes already succeeded; a concurrent refresh must not fail the cleanup.
		logging.From(ctx).Warn("image unused cleanup refresh skipped", zap.String("server_id", serverID), zap.Error(err))
	}
	_ = s.tasks.Complete(ctx, task.ID, "Unused images older than 7 days cleaned")
}

func (s *Service) runSimpleRefreshTask(ctx context.Context, task tasks.Task, serverID, completedSummary string, refresh func(context.Context, string) error) error {
	if strings.TrimSpace(serverID) == "" {
		return panelerr.Validation("server_required", "Server is required")
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	s.runSimpleResourceRefresh(s.tasks.ExecutionContext(task.ID), task, serverID, completedSummary, refresh)
	return nil
}
