package containerization

import (
	"context"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service, collectionInterval func() time.Duration) {
	if taskSvc == nil {
		return
	}
	var lastImageRun time.Time
	registeredAt := time.Now()
	for _, def := range []tasks.Definition{
		{
			Type:       TaskContainerRefresh,
			AllowRetry: true,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunContainerRefreshTask(tc.Context, tc.Task)
			},
		},
		{
			Type:       TaskImageRefresh,
			AllowRetry: true,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunImageRefreshTask(tc.Context, tc.Task)
			},
			Periodic: &tasks.Periodic{
				Interval: 5 * time.Second,
				CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
					interval := time.Minute
					if collectionInterval != nil {
						interval = collectionInterval()
					}
					if lastImageRun.IsZero() {
						lastImageRun = registeredAt
					}
					if time.Since(lastImageRun) < interval {
						return tasks.CreateBatchInput{}, false, nil
					}
					batch, shouldRun, err := s.CollectImageRefreshInputs(ctx)
					if err == nil && shouldRun {
						lastImageRun = time.Now()
					}
					return batch, shouldRun, err
				},
			},
		},
		{
			Type:       TaskVolumeRefresh,
			AllowRetry: true,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunVolumeRefreshTask(tc.Context, tc.Task)
			},
		},
		{
			Type:              TaskApplicationReconcile,
			AllowRetry:        true,
			DefaultMaxRetries: 3,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunApplicationReconcileTask(tc.Context, tc.Task)
			},
			Periodic: &tasks.Periodic{
				Interval: 5 * time.Second,
				CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
					operationID := id.New("op")
					inputs, err := s.CollectApplicationReconcileTasks(ctx, operationID)
					if err != nil || len(inputs) == 0 {
						return tasks.CreateBatchInput{}, false, err
					}
					return tasks.CreateBatchInput{Type: TaskApplicationReconcile, OperationID: operationID, TriggerType: "scheduler", Summary: "Monitoring application containers", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
				},
			},
		},
		{
			Type:              TaskImageUpgradeMany,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunApplicationImageUpgradeTask(tc.Context, tc.Task)
			},
		},
		{
			Type:              TaskImageUpgradeAll,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunApplicationImageUpgradeTask(tc.Context, tc.Task)
			},
		},
	} {
		taskSvc.MustRegister(def)
	}
}

func (s *Service) RunContainerRefreshTask(ctx context.Context, task tasks.Task) error {
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Containers refreshed", func(runCtx context.Context, baseURL string) error {
		_, err := s.agent.DockerContainers(runCtx, baseURL)
		return err
	})
}

func (s *Service) RunVolumeRefreshTask(ctx context.Context, task tasks.Task) error {
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Volumes refreshed", func(runCtx context.Context, baseURL string) error {
		_, err := s.agent.DockerVolumes(runCtx, baseURL)
		return err
	})
}

func (s *Service) RunApplicationImageUpgradeTask(ctx context.Context, task tasks.Task) error {
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	applicationIDs := strings.Split(task.ResourceID, ",")
	s.runApplicationUpdates(task, applicationIDs)
	return nil
}

func (s *Service) CollectImageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
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
			Type:         TaskImageRefresh,
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
	return tasks.CreateBatchInput{Type: TaskImageRefresh, OperationID: operationID, TriggerType: "scheduler", Summary: "Refreshing scheduled image checks", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
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
