package applications

import (
	"strings"

	"panel/internal/modules/tasks"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:              TaskTypeTargetBatch,
			Summary:           "Application target coordination",
			ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
			Execute:           func(tasks.TaskContext) error { return nil },
		},
		{
			Type:              TaskTypeTargetApply,
			Hidden:            true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceQueue,
			ConcurrencyKey:    applicationTargetConcurrencyKey,
			Execute:           s.RunDeployTask,
			OnFailure:         s.handleTargetTaskFailure,
		},
		{
			Type:              TaskTypeTargetStop,
			Hidden:            true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceQueue,
			ConcurrencyKey:    applicationTargetConcurrencyKey,
			Execute:           s.RunDeployTask,
			OnFailure:         s.handleTargetTaskFailure,
		},
		{
			Type:              TaskTypeTargetPurge,
			Hidden:            true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceQueue,
			ConcurrencyKey:    applicationTargetConcurrencyKey,
			Execute:           s.RunDeployTask,
			OnFailure:         s.handleTargetTaskFailure,
		},
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

func applicationTargetConcurrencyKey(in tasks.CreateInput) string {
	appID := in.ResourceID
	serverID := in.ServerID
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(serverID) == "" {
		return applicationLifecycleConcurrencyKey(in)
	}
	return "application:target:" + strings.TrimSpace(appID) + ":" + strings.TrimSpace(serverID)
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
