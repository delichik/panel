package keyassets

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
)

const panelTLSReconcileInterval = 30 * time.Minute

// keyAssetConcurrencyKey serializes operations per key asset: reissue,
// regenerate and export on the same asset share one key. Bulk operations
// without a single asset ID (export) use a global key so they never run
// concurrently with each other.
func keyAssetConcurrencyKey(in tasks.CreateInput) string {
	if strings.TrimSpace(in.ResourceID) == "" {
		return "key_asset:global"
	}
	return "key_asset:" + in.ResourceID
}

func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	for _, def := range []tasks.Definition{
		{
			Type:              TaskTypePanelTLSReconcile,
			Summary:           "Reconciling Panel TLS assets",
			Hidden:            true,
			AllowRetry:        true,
			ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive,
			ConcurrencyKey: func(in tasks.CreateInput) string {
				return "key_asset:" + SystemPanelTLSAssetID
			},
			Execute: s.RunPanelTLSReconcileTask,
			Periodic: &tasks.Periodic{
				CollectInputs: s.CollectPanelTLSInputs,
			},
		},
		{Type: TaskTypeTLSReissue, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey, Execute: s.RunTLSReissueTask},
		{Type: TaskTypeSSHRegenerate, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey, Execute: s.RunSSHRegenerateTask},
		{Type: TaskTypeExport, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey, Execute: s.RunExportTask},
		{Type: TaskTypeImport, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed, Execute: s.RunImportTask},
	} {
		taskSvc.MustRegister(def)
	}
}

func (s *Service) queueAssetTask(ctx context.Context, taskType, resourceID, summary, paramsJSON string) (tasks.Task, error) {
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	task, created, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "key_asset",
		ResourceID:   resourceID,
		TriggerType:  "user",
		Summary:      summary,
		ParamsJSON:   paramsJSON,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return tasks.Task{}, panelerr.Conflict("key_asset_operation_in_progress", "Another operation on the same key asset is already in progress")
	}
	return task, nil
}

func (s *Service) finishAssetTask(tc tasks.TaskContext, run func(context.Context) (string, error)) error {
	ctx := tc.Context
	if s != nil && s.tasks != nil && strings.TrimSpace(tc.Task.ID) != "" {
		defer s.tasks.FinishExecution(tc.Task.ID)
	}
	summary, err := run(ctx)
	if err != nil {
		if s != nil && s.tasks != nil && strings.TrimSpace(tc.Task.ID) != "" {
			_ = s.tasks.Fail(ctx, tc.Task.ID, err)
		}
		return err
	}
	if s != nil && s.tasks != nil && strings.TrimSpace(tc.Task.ID) != "" {
		if strings.TrimSpace(summary) == "" {
			summary = tc.Task.Summary
		}
		return s.tasks.Complete(ctx, tc.Task.ID, summary)
	}
	return nil
}

func (s *Service) RunTLSReissueTask(tc tasks.TaskContext) error {
	return s.finishAssetTask(tc, func(ctx context.Context) (string, error) {
		return s.executeTLSReissue(ctx, strings.TrimSpace(firstNonEmpty(tc.Task.ResourceID, tc.Task.TriggerResourceID)))
	})
}

func (s *Service) RunSSHRegenerateTask(tc tasks.TaskContext) error {
	return s.finishAssetTask(tc, func(ctx context.Context) (string, error) {
		return s.executeSSHRegenerate(ctx, strings.TrimSpace(firstNonEmpty(tc.Task.ResourceID, tc.Task.TriggerResourceID)))
	})
}

func (s *Service) RunExportTask(tc tasks.TaskContext) error {
	return s.finishAssetTask(tc, func(ctx context.Context) (string, error) {
		var params exportTaskParams
		if err := json.Unmarshal([]byte(tc.Task.ParamsJSON), &params); err != nil {
			return "", err
		}
		if s.secrets == nil {
			return "", panelerr.Validation("task_service_unavailable", "Key asset secret store is unavailable")
		}
		password, err := s.secrets.Decrypt(exportPasswordAssetID, exportPasswordAssetID, exportPasswordField, params.PasswordCiphertext)
		if err != nil {
			return "", err
		}
		defer func() {
			for i := range password {
				password[i] = 0
			}
		}()
		if err := s.executeExport(ctx, tc.Task.ID, params.AssetIDs, string(password)); err != nil {
			return "", err
		}
		return "Exported key assets", nil
	})
}

func (s *Service) RunImportTask(tc tasks.TaskContext) error {
	return s.finishAssetTask(tc, func(ctx context.Context) (string, error) {
		var params importTaskParams
		if err := json.Unmarshal([]byte(tc.Task.ParamsJSON), &params); err != nil {
			return "", err
		}
		plan, err := s.getImportPlan(params.PlanID)
		if err != nil {
			return "", err
		}
		strategy, err := normalizeImportStrategy(params.Strategy)
		if err != nil {
			return "", err
		}
		in := ImportExecuteRequest{
			Strategy:                  strategy,
			ConfirmOverwriteInUse:     params.ConfirmOverwriteInUse,
			ConfirmDangerousOverwrite: params.ConfirmDangerousOverwrite,
		}
		if err := s.executeImportPlan(ctx, plan, strategy, in); err != nil {
			return "", err
		}
		return "Imported key assets", nil
	})
}

// StartPeriodic starts the module-owned panel TLS reconcile scheduler loop.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, TaskTypePanelTLSReconcile, panelTLSReconcileInterval, "keyassets")
}
