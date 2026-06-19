package metrics

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	server "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

type serverListProvider interface {
	List(context.Context) ([]server.Server, error)
}

func (s *Service) RegisterTasks(taskSvc *tasks.Service, collectionInterval func() time.Duration) {
	if taskSvc == nil {
		return
	}
	taskSvc.MustRegister(tasks.Definition{
		Type:              "metrics_collect",
		Summary:           "Collecting scheduled metrics",
		Hidden:            true,
		ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
		Execute:           s.RunCollectTask,
		Periodic: &tasks.Periodic{
			Interval:      time.Second,
			CollectInputs: tasks.NewIntervalCollector(time.Minute, collectionInterval, s.CollectTaskInputs),
		},
	})
}

func (s *Service) CollectTaskInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	lister, ok := s.servers.(serverListProvider)
	if !ok {
		return tasks.CreateBatchInput{}, false, nil
	}
	servers, err := lister.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, srv := range servers {
		if !srv.OS.Supported || !srv.Reachable || !metricsAgentReady(srv) {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         "metrics_collect",
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "scheduler",
			ParamsJSON:   `{"collectedAt":"` + collectedAt.Format(time.RFC3339Nano) + `"}`,
			Summary:      "Collecting metrics for " + srv.Name,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{Type: "metrics_collect", OperationID: operationID, TriggerType: "scheduler", Summary: "Collecting scheduled metrics", ExecutionMode: tasks.ExecutionModeParallel, Inputs: inputs}, true, nil
}

func (s *Service) RunCollectTask(tc tasks.TaskContext) error {
	ctx, task, taskSvc := tc.Context, tc.Task, tc.Service
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return nil
	}
	collectedAt := time.Now().UTC().Truncate(time.Second)
	var params struct {
		CollectedAt string `json:"collectedAt"`
	}
	if strings.TrimSpace(task.ParamsJSON) != "" {
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err == nil && params.CollectedAt != "" {
			if parsed, parseErr := time.Parse(time.RFC3339Nano, params.CollectedAt); parseErr == nil {
				collectedAt = parsed
			}
		}
	}
	if err := s.CollectAt(ctx, serverID, collectedAt); err != nil {
		if taskSvc != nil {
			_ = taskSvc.Fail(ctx, task.ID, err)
		}
		return err
	}
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Complete(ctx, task.ID, "")
}

func metricsAgentReady(srv server.Server) bool {
	switch strings.ToLower(strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled])) {
	case "true", "1", "yes", "on":
		return strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) != "" && srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusCompatible
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
