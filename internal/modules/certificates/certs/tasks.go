package certs

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
			Type:        TaskTypeIssue,
			AllowRunNow: true,
			AllowRetry:  true,
			Execute:     s.RunIssueTask,
		},
		{
			Type:              TaskTypeRenew,
			Summary:           "Renewing due certificates",
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute:           s.RenewTask,
			Periodic: &tasks.Periodic{
				Interval:      5 * time.Second,
				CollectInputs: tasks.NewIntervalCollector(time.Hour, nil, s.CollectRenewInputs),
			},
		},
		{Type: TaskTypeSelfSignedRenew, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}

func (s *Service) CollectRenewInputs(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
	return s.collectRenewInputsAt(ctx, time.Now())
}

func (s *Service) collectRenewInputsAt(ctx context.Context, now time.Time) (tasks.CreateBatchInput, bool, error) {
	certificates, err := s.List(ctx)
	if err != nil {
		return tasks.CreateBatchInput{}, false, err
	}
	operationID := id.New("op")
	inputs := []tasks.CreateInput{}
	for _, cert := range certificates {
		if !cert.AutoRenew || cert.NextRenewAt.IsZero() || cert.NextRenewAt.After(now) {
			continue
		}
		inputs = append(inputs, tasks.CreateInput{
			OperationID:  operationID,
			Type:         TaskTypeRenew,
			ResourceType: "certificate",
			ResourceID:   cert.ID,
			TriggerType:  "scheduler",
			MetadataJSON: certTaskMetadataJSON(cert),
			Summary:      "Renewing certificate for " + cert.Domain,
		})
	}
	if len(inputs) == 0 {
		return tasks.CreateBatchInput{}, false, nil
	}
	return tasks.CreateBatchInput{
		Type:          TaskTypeRenew,
		OperationID:   operationID,
		TriggerType:   "scheduler",
		Summary:       "Renewing due certificates",
		ExecutionMode: tasks.ExecutionModeParallel,
		Inputs:        inputs,
	}, true, nil
}
