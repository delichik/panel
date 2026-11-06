package applications

import (
	"testing"

	"panel/internal/modules/tasks"
)

func TestBindRunEngineWorkerWiresExecuteAndJobRunner(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()

	def, ok := svc.tasks.Registry().Definition("application_deploy")
	if !ok || def.Execute == nil {
		t.Fatal("RegisterTasks must leave application_deploy Execute wired")
	}
	if def.RecoveryPolicy != tasks.RecoveryRemoteFence {
		t.Fatalf("recovery = %q, want remote_fence", def.RecoveryPolicy)
	}

	worker := tasks.NewWorker(svc.tasks)
	svc.BindRunEngineWorker(worker)
	if svc.orchestrator == nil || !svc.orchestrator.JobRunnerConfigured() {
		t.Fatal("BindRunEngineWorker must set Controller JobRunner")
	}
	if !svc.orchestrator.ExternalScanConfigured() {
		t.Fatal("BindRunEngineWorker must hand due/wake to the unified worker")
	}
}
