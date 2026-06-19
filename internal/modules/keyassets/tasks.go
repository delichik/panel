package keyassets

import "panel/internal/modules/tasks"

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{Type: TaskTypeTLSReissue, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeSSHRegenerate, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeExport, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeImport, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeSync, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}
