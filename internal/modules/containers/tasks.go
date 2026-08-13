package containerization

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:       TaskImageRefresh,
			AllowRetry: true,
			Execute:    s.RunImageRefreshTask,
			Periodic: &tasks.Periodic{
				Interval:      30 * time.Minute,
				CollectInputs: tasks.NewIntervalCollector(30*time.Minute, nil, s.CollectImageRefreshInputs),
			},
		},
		{
			Type:       TaskVolumeRefresh,
			AllowRetry: true,
			Execute:    s.RunVolumeRefreshTask,
		},
		{Type: TaskNetworkRefresh, AllowRetry: true, Execute: s.RunNetworkRefreshTask},
		{
			Type:              TaskApplicationReconcile,
			ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
			ConcurrencyKey:    applicationReconcileConcurrencyKey,
			Periodic: &tasks.Periodic{
				Interval:      5 * time.Second,
				CollectInputs: s.CollectApplicationReconcileInputs,
			},
		},
		{
			Type:              TaskImageUpgradeMany,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute:           s.RunApplicationImageUpgradeTask,
		},
		{
			Type:              TaskImageUpgradeAll,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
			Execute:           s.RunApplicationImageUpgradeTask,
		},
	} {
		taskSvc.MustRegister(def)
	}
}

type ApplicationReconcileTrigger struct {
	ApplicationIDs []string `json:"applicationIds"`
	ServerIDs      []string `json:"serverIds"`
	Force          bool     `json:"force"`
	Purge          bool     `json:"purge"`
	Reason         string   `json:"reason"`
	StopServers    []string `json:"stopServers"`
}

func (s *Service) RunVolumeRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Volumes refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "volumes", items)
	})
}

func (s *Service) RunNetworkRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	return s.runSimpleRefreshTask(ctx, task, serverID, "Networks refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerNetworks(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "networks", items)
	})
}

func (s *Service) RunApplicationImageUpgradeTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	applicationIDs := strings.Split(task.ResourceID, ",")
	s.runApplicationUpdates(task, applicationIDs)
	return nil
}

func (s *Service) CollectApplicationReconcileInputs(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
	inputs, err := s.CollectApplicationReconcileTasks(ctx, "", trigger)
	if err != nil || len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, err
	}
	triggerType := firstNonEmpty(trigger.Type, "scheduler")
	return tasks.CreateBatchInput{
		Type:          applications.TaskTypeTargetBatch,
		TriggerType:   triggerType,
		Summary:       "Monitoring application containers",
		ExecutionMode: tasks.ExecutionModeSerial,
		ForceParent:   true,
		Inputs:        inputs,
	}, true, nil
}

func applicationReconcileConcurrencyKey(in tasks.CreateInput) string {
	appID := firstNonEmpty(in.ResourceID, in.TriggerResourceID, in.ServerID, in.NodeID)
	if appID == "" {
		return ""
	}
	return "application:lifecycle:" + appID
}

func applicationReconcileTriggerPayload(trigger tasks.PeriodicTrigger) ApplicationReconcileTrigger {
	out := ApplicationReconcileTrigger{}
	switch value := trigger.Payload.(type) {
	case ApplicationReconcileTrigger:
		out = value
	case *ApplicationReconcileTrigger:
		if value != nil {
			out = *value
		}
	case map[string]any:
		if raw, err := json.Marshal(value); err == nil {
			_ = json.Unmarshal(raw, &out)
		}
	}
	if trigger.TriggerResourceType == "application" && strings.TrimSpace(trigger.TriggerResourceID) != "" {
		out.ApplicationIDs = append(out.ApplicationIDs, strings.TrimSpace(trigger.TriggerResourceID))
	}
	out.ApplicationIDs = uniqueStrings(out.ApplicationIDs)
	out.ServerIDs = uniqueStrings(out.ServerIDs)
	out.StopServers = uniqueStrings(out.StopServers)
	return out
}

func (s *Service) CollectImageRefreshInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
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
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         TaskImageRefresh,
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			Summary:      "Refreshing image updates",
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: TaskImageRefresh, OperationID: operationID, TriggerType: "scheduler", Summary: "Refreshing scheduled image checks", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Service) runSimpleRefreshTask(ctx context.Context, task tasks.Task, serverID, completedSummary string, refresh func(context.Context, string) error) error {
	if strings.TrimSpace(serverID) == "" {
		return panelerr.Validation("server_required", "Server is required")
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	s.runSimpleResourceRefresh(s.tasks.ExecutionContext(task.ID), task, serverID, completedSummary, refresh)
	return nil
}
