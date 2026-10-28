package applications

import (
	"panel/internal/modules/tasks"
	controlplane "panel/internal/orchestrator"
)

// RegisterDeployRunDefinition registers the hidden remote-fence deploy Run
// before any orphan loop can see application_deploy rows. Execute may be nil
// at this early call; RegisterTasks / BindRunEngineWorker Replace it once the
// applications service can route to Controller.RunJob (ENG-RUN-001).
func RegisterDeployRunDefinition(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	if _, ok := taskSvc.Registry().Definition("application_deploy"); ok {
		return
	}
	taskSvc.MustRegister(tasks.Definition{
		Type:           "application_deploy",
		Hidden:         true,
		Summary:        "Application deploy/stop/purge Run (event_runs; remote-fence)",
		RecoveryPolicy: tasks.RecoveryRemoteFence,
	})
}

// bindDeployExecute wires Manager.Run → Execute → Controller.RunJob so deploy
// claim/lease shares the unified Execute path (closes GAP-030 claim dual-pool).
func (s *Service) bindDeployExecute(taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	taskSvc.Registry().Replace(tasks.Definition{
		Type:           "application_deploy",
		Hidden:         true,
		Summary:        "Application deploy/stop/purge Run (event_runs; remote-fence)",
		RecoveryPolicy: tasks.RecoveryRemoteFence,
		Execute:        s.RunApplicationDeployTask,
	})
}

// RunApplicationDeployTask is the Definition.Execute for application_deploy.
// Lease claim, renew, RuntimeReconcile, and status settlement stay in RunJob.
func (s *Service) RunApplicationDeployTask(tc tasks.TaskContext) error {
	if s == nil || s.orchestrator == nil {
		return controlplane.ErrStoreUnavailable
	}
	return s.orchestrator.RunJob(tc.Context, tc.Task.ID)
}

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	RegisterDeployRunDefinition(taskSvc)
	s.bindDeployExecute(taskSvc)
	for _, def := range []tasks.Definition{
		{
			Type:              TaskTypeStop,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    applicationLifecycleConcurrencyKey,
			Execute:           s.RunStopTask,
		},
		{
			Type:              TaskTypeRestart,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    applicationLifecycleConcurrencyKey,
			Execute:           s.RunRestartTask,
		},
		{
			Type:              TaskTypeRefresh,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    applicationLifecycleConcurrencyKey,
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
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    applicationLifecycleConcurrencyKey,
			Execute:           s.RunImageUpdateTask,
		},
	} {
		taskSvc.MustRegister(def)
	}
}

func applicationLifecycleConcurrencyKey(in tasks.CreateInput) string {
	appID := in.ResourceID
	if appID == "" {
		appID = in.ServerID
	}
	if appID == "" {
		appID = in.NodeID
	}
	if appID == "" {
		return ""
	}
	return "application:lifecycle:" + appID
}
