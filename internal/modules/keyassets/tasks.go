package keyassets

import (
	"context"
	"strings"
	"time"

	"panel/internal/modules/tasks"
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
		{Type: TaskTypeTLSReissue, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeSSHRegenerate, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeExport, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeImport, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}

// StartPeriodic starts the module-owned panel TLS reconcile scheduler loop.
func (s *Service) StartPeriodic(ctx context.Context, taskSvc *tasks.Service) {
	if s == nil || taskSvc == nil {
		return
	}
	tasks.StartSchedulerLoop(ctx, taskSvc, TaskTypePanelTLSReconcile, panelTLSReconcileInterval, "keyassets")
}
