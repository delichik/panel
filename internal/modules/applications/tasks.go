package applications

import "panel/internal/modules/tasks"

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{Type: TaskTypeDeploy, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeStop, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeRestart, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeRefresh, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeImageCheck, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeImageUpdate, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}
