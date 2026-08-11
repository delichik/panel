package keyassets

import (
	"strings"

	"panel/internal/modules/tasks"
)

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
		{Type: TaskTypeTLSReissue, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeSSHRegenerate, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeExport, ConcurrencyPolicy: tasks.ConcurrencyResourceExclusive, ConcurrencyKey: keyAssetConcurrencyKey},
		{Type: TaskTypeImport, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
		{Type: TaskTypeSync, ConcurrencyPolicy: tasks.ConcurrencyParallelAllowed},
	} {
		taskSvc.MustRegister(def)
	}
}
