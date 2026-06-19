package keyassets

import "panel/internal/modules/tasks"

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{Type: TaskTypeTLSReissue, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeSSHRegenerate, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeExport, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeImport, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeSync, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}
