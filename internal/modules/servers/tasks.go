package server

import (
	"context"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
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
			Type:              serverInfoTaskType,
			AllowRunNow:       true,
			AllowRetry:        true,
			DefaultMaxRetries: connectivityMaxRetries,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    serverInfoConcurrencyKey,
			Execute:           s.RunServerInfoTask,
			Periodic: &tasks.Periodic{
				Interval:      time.Minute,
				CollectInputs: tasks.NewIntervalCollector(time.Hour, nil, s.CollectServerInfoInputs),
			},
		},
		{Type: ufwInstallTaskType, StaleQueuedAfter: 10 * time.Minute},
		{Type: ufwEnableTaskType},
		{Type: fail2banApplyTaskType},
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

func (s *Service) CollectServerInfoInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	servers, err := s.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	operationID := id.New("op")
	inputs := make([]tasks.CreateInput, 0, len(servers))
	for _, srv := range servers {
		if skipUnavailableAgentScheduledWork(srv) ||
			!traitEnabled(srv.Traits[agentcontract.TraitEnabled]) ||
			srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible ||
			strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) == "" {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         serverInfoTaskType,
			ServerID:     srv.ID,
			ResourceType: connectivityResourceType,
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			ParamsJSON:   `{"bootstrap":false}`,
			Summary:      "Collecting system information for " + srv.Name,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: serverInfoTaskType, OperationID: operationID, TriggerType: "scheduler", Summary: "Collecting scheduled system information", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func serverInfoConcurrencyKey(in tasks.CreateInput) string {
	serverID := firstNonEmpty(in.ResourceID, in.ServerID, in.NodeID)
	if serverID == "" {
		return ""
	}
	return "server_info:" + serverID
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
