package packages

import (
	"context"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service, collectionInterval func() time.Duration) {
	if taskSvc == nil {
		return
	}
	taskSvc.MustRegister(tasks.Definition{
		Type:        "package_refresh",
		Summary:     "Refreshing scheduled packages",
		AllowRunNow: true,
		AllowRetry:  true,
		Execute:     s.RunRefreshTask,
		Periodic: &tasks.Periodic{
			Interval:      30 * time.Minute,
			CollectInputs: tasks.NewIntervalCollector(30*time.Minute, nil, s.CollectScheduledPackageRefreshInputs),
		},
	})
	taskSvc.MustRegister(tasks.Definition{
		Type:              "package_upgrade_selected",
		Summary:           "Upgrading selected packages",
		AllowRunNow:       true,
		AllowRetry:        true,
		DisallowCancel:    true,
		ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
		Execute:           s.RunUpgradeSelectedTask,
	})
	taskSvc.MustRegister(tasks.Definition{
		Type:              "package_upgrade_all",
		Summary:           "Upgrading all packages",
		AllowRunNow:       true,
		AllowRetry:        true,
		DisallowCancel:    true,
		ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
		Execute:           s.RunUpgradeAllTask,
	})
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
		if !srv.OS.Supported || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible ||
			strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) == "" || s.hasRecentTask(ctx, "package_refresh", srv.ID, 10*time.Minute) {
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
