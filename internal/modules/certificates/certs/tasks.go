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
	var lastRenewalRun time.Time
	registeredAt := time.Now()
	for _, def := range []tasks.Definition{
		{
			Type:        TaskTypeIssue,
			AllowRunNow: true,
			AllowRetry:  true,
			Execute: func(tc tasks.TaskContext) error {
				return s.RunIssueTask(tc.Context, tc.Task)
			},
		},
		{
			Type:              TaskTypeRenew,
			Summary:           "Renewing due certificates",
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed,
			Execute: func(tc tasks.TaskContext) error {
				return s.RenewTask(tc.Context, tc.Task)
			},
			Periodic: &tasks.Periodic{
				Interval: 5 * time.Second,
				CollectInputs: func(ctx context.Context) (tasks.CreateBatchInput, bool, error) {
					if lastRenewalRun.IsZero() {
						lastRenewalRun = registeredAt
					}
					if time.Since(lastRenewalRun) < time.Hour {
						return tasks.CreateBatchInput{}, false, nil
					}
					batch, shouldRun, err := s.CollectRenewInputs(ctx, time.Now())
					if err == nil && shouldRun {
						lastRenewalRun = time.Now()
					}
					return batch, shouldRun, err
				},
			},
		},
		{Type: TaskTypeSelfSignedRenew, AllowRetry: true, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}

func (s *Service) CollectRenewInputs(ctx context.Context, now time.Time) (tasks.CreateBatchInput, bool, error) {
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
