package certs

import (
	"context"
	"strings"
	"time"

	"panel/internal/modules/tasks"
	id "panel/internal/platform/identity"
)

const certificateRenewInterval = 30 * time.Minute

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	certificateLifecycleKey := func(in tasks.CreateInput) string {
		certID := strings.TrimSpace(in.ResourceID)
		if certID == "" {
			return ""
		}
		// Share one exclusive domain across issue/reissue/renew so concurrent
		// ACME flows cannot overwrite the same certificate/asset (CRT-ACME-010).
		return "certificate:" + certID
	}
	for _, def := range []tasks.Definition{
		{
			Type:              TaskTypeIssue,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
			ConcurrencyKey:    certificateLifecycleKey,
			Execute:           s.RunIssueTask,
		},
		{
			Type:              TaskTypeReissue,
			AllowRunNow:       true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
			ConcurrencyKey:    certificateLifecycleKey,
			Execute:           s.RunIssueTask,
		},
		{
			Type:              TaskTypeRenew,
			Summary:           "Renewing due certificates",
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
			ConcurrencyKey:    certificateLifecycleKey,
			Execute:           s.RenewTask,
			Periodic: &tasks.Periodic{
				CollectInputs: func(ctx context.Context, _ tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
					return s.CollectRenewInputs(ctx)
				},
			},
		},
	} {
		taskSvc.MustRegister(def)
	}
}

// StartPeriodic starts the module-owned certificate renew scheduler loop.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, TaskTypeRenew, certificateRenewInterval, "certificates")
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
