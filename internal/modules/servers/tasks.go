package server

import (
	"context"
	"time"

	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:              "server_agent_check",
			Hidden:            true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunAgentCheckTask,
			Periodic: &tasks.Periodic{
				Interval:      5 * time.Minute,
				CollectInputs: s.CollectAgentCheckInputs,
			},
		},
		{
			Type:              connectivityTaskType,
			Hidden:            true,
			AllowRunNow:       true,
			AllowRetry:        true,
			DefaultMaxRetries: connectivityMaxRetries,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    serverConnectivityConcurrencyKey,
			Execute:           s.RunConnectivityTask,
		},
		{
			Type:              serverInfoTaskType,
			AllowRunNow:       true,
			AllowRetry:        true,
			DefaultMaxRetries: connectivityMaxRetries,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    serverConnectivityConcurrencyKey,
			Execute:           s.RunConnectivityTask,
		},
		{Type: ufwInstallTaskType, StaleQueuedAfter: 10 * time.Minute},
		{Type: ufwEnableTaskType},
		{Type: restartTaskType},
		{
			Type:        agentDeployTaskType,
			AllowRunNow: true,
			AllowRetry:  true,
			Execute:     s.RunAgentDeployTask,
		},
		{Type: agentCertificateResetTaskType},
	} {
		if _, exists := taskSvc.Registry().Definition(def.Type); exists {
			taskSvc.Registry().Replace(def)
			continue
		}
		taskSvc.MustRegister(def)
	}
}

func serverConnectivityConcurrencyKey(in tasks.CreateInput) string {
	serverID := firstNonEmpty(in.ResourceID, in.ServerID, in.NodeID)
	if serverID == "" {
		return ""
	}
	return "server_connectivity:" + serverID
}

func (s *Service) CollectAgentCheckInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	servers, err := s.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	inputs := []tasks.CreateInput{}
	operationID := id.New("op")
	for _, srv := range servers {
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "server_agent_check",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      "Checking agent for " + srv.Name,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{
		Type:          "server_agent_check",
		OperationID:   operationID,
		TriggerType:   "scheduler",
		Summary:       "Checking configured agents",
		ExecutionMode: tasks.ExecutionModeParallel,
		Inputs:        inputs,
	}, true, nil
}

func (s *Service) RunAgentCheckTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	srv, err := s.Get(checkCtx, serverID)
	if err != nil {
		return err
	}
	s.CheckConfiguredAgent(checkCtx, srv)
	return nil
}
