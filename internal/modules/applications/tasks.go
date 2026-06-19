package applications

import "panel/internal/modules/tasks"

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:              TaskTypeDeploy,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunDeployTask,
		},
		{
			Type:              TaskTypeStop,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunStopTask,
		},
		{
			Type:              TaskTypeRestart,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunRestartTask,
		},
		{
			Type:              TaskTypeRefresh,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunRefreshTask,
		},
		{
			Type:              TaskTypeImageCheck,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunImageCheckTask,
		},
		{
			Type:              TaskTypeImageUpdate,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunImageUpdateTask,
		},
	} {
		taskSvc.MustRegister(def)
	}
}
