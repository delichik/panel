package server

import (
	"context"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const (
	serverAgentCheckInterval  = 5 * time.Minute
	serverInfoCollectInterval = time.Hour
)

// dispatchRegisteredTask runs a queued/started task through Manager.Run so the
// registered Execute path owns panic recovery and completion. Callers that must
// return StatusRunning (AGT-DEP-001) should awaitTaskRunning after dispatch.
func (s *Service) dispatchRegisteredTask(task tasks.Task) {
	if s == nil || s.tasks == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	go func() {
		defer s.tasks.FinishExecution(task.ID)
		_ = tasks.NewManager(s.tasks).Run(context.Background(), task)
	}()
}

func (s *Service) awaitTaskRunning(ctx context.Context, taskID string, timeout time.Duration) (tasks.Task, error) {
	if s == nil || s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	deadline := time.Now().Add(timeout)
	for {
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return task, err
		}
		switch task.Status {
		case tasks.StatusRunning, tasks.StatusCompleted, tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked, tasks.StatusCancelled:
			return task, nil
		}
		if time.Now().After(deadline) {
			return task, nil
		}
		select {
		case <-ctx.Done():
			return task, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:   "server_agent_check",
			Hidden: true,
			// 周期健康检查按服务器每 5 分钟产生一次执行事实；它们是后台巡检
			// 而不是用户操作，成功流转只进 debug，失败仍进 error，避免活动
			// 日志被“检查 Agent”刷屏。
			Quiet:             true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RunAgentCheckTask,
			Periodic: &tasks.Periodic{
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
				CollectInputs: func(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
					return s.CollectServerInfoInputs(ctx)
				},
			},
		},
		{
			Type:    fail2banApplyTaskType,
			Execute: s.RunFail2BanApplyTask,
		},
		{
			Type:    fail2banReleaseTaskType,
			Execute: s.RunFail2BanReleaseTask,
		},
		{
			Type:    restartTaskType,
			Execute: s.RunRestartTask,
		},
		{
			Type:        agentDeployTaskType,
			AllowRunNow: true,
			AllowRetry:  true,
			Execute:     s.RunAgentDeployTask,
		},
		{
			Type:        tailscaleApplyTaskType,
			AllowRunNow: true,
			AllowRetry:  true,
			Execute:     s.RunTailscaleApplyTask,
		},
		{
			Type:    agentCertificateResetTaskType,
			Execute: s.RunSystemCertificateResetTask,
		},
	} {
		if _, exists := taskSvc.Registry().Definition(def.Type); exists {
			taskSvc.Registry().Replace(def)
			continue
		}
		taskSvc.MustRegister(def)
	}
}

// StartPeriodic starts module-owned scheduler loops for agent check and info collect.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, "server_agent_check", serverAgentCheckInterval, "servers")
	tasks.StartSchedulerLoop(ctx, taskSvc, serverInfoTaskType, serverInfoCollectInterval, "servers")
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

func (s *Service) CollectAgentCheckInputs(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
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
