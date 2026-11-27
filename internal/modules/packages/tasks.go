package packages

import (
	"context"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

const packageRefreshInterval = 30 * time.Minute

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	taskSvc.MustRegister(tasks.Definition{
		Type:        "package_refresh",
		Summary:     "Refreshing scheduled packages",
		Quiet:       true,
		AllowRunNow: true,
		AllowRetry:  true,
		Execute:     s.RunRefreshTask,
		Periodic: &tasks.Periodic{
			CollectInputs: func(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
				return s.CollectScheduledPackageRefreshInputs(ctx)
			},
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
	// PKG-API-011：仅安全升级任务。与全部/选中升级一样不可取消且可重试，
	// 执行时实时分类，空集合直接完成。
	taskSvc.MustRegister(tasks.Definition{
		Type:              TaskPackageUpgradeSecurity,
		Summary:           "Upgrading security packages",
		AllowRunNow:       true,
		AllowRetry:        true,
		DisallowCancel:    true,
		ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
		Execute:           s.RunUpgradeSecurityTask,
	})
}

// StartPeriodic starts the module-owned package refresh scheduler loop.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, "package_refresh", packageRefreshInterval, "packages")
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
