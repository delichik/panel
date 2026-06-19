package packages

import (
	"context"
	"time"

	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service, collectionInterval func() time.Duration) {
	if taskSvc == nil {
		return
	}
	var lastPackageRun time.Time
	registeredAt := time.Now()
	taskSvc.MustRegister(tasks.Definition{
		Type:        "package_refresh",
		Summary:     "Refreshing scheduled packages",
		AllowRunNow: true,
		AllowRetry:  true,
		Execute: func(tc tasks.TaskContext) error {
			return s.RunRefreshTask(tc.Context, tc.Task)
		},
		Periodic: &tasks.Periodic{
			Interval: time.Second,
			CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
				interval := time.Minute
				if collectionInterval != nil {
					interval = collectionInterval()
				}
				if lastPackageRun.IsZero() {
					lastPackageRun = registeredAt
				}
				if time.Since(lastPackageRun) < interval {
					return tasks.CreateBatchInput{}, false, nil
				}
				batch, shouldRun, err := s.CollectScheduledPackageRefreshInputs(ctx)
				if err == nil && shouldRun {
					lastPackageRun = time.Now()
				}
				return batch, shouldRun, err
			},
		},
	})
	taskSvc.MustRegister(tasks.Definition{Type: "package_upgrade_selected", AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed})
	taskSvc.MustRegister(tasks.Definition{Type: "package_upgrade_all", AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed})
}

func (s *Service) CollectScheduledPackageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
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

func (s *Service) hasRecentTask(ctx context.Context, taskType, serverID string, window time.Duration) bool {
	if s == nil || s.tasks == nil {
		return false
	}
	result, err := s.tasks.List(ctx, tasks.ListFilter{Type: taskType, ServerID: serverID, Limit: 1, IncludeInternal: true})
	if err != nil || len(result.Items) == 0 {
		return false
	}
	return time.Since(result.Items[0].CreatedAt) <= window
}
