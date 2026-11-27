package applications

import (
	"context"
	"testing"

	controlplane "panel/internal/orchestrator"
)

// TestOnOrchestratorJobFailedRecordsReconcileFailure verifies that every Job
// failure is recorded against the application reconcile failure counter and
// that ReconcileStopAfterFailures consecutive failures terminate automatic
// reconciliation via reconcile_stopped.
func TestOnOrchestratorJobFailedRecordsReconcileFailure(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ensure at least one instance row so reconcile state rows can be created.
	if _, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "test"}); err != nil {
		t.Fatal(err)
	}
	job := controlplane.Job{ApplicationID: app.ID}

	for i := 0; i < ReconcileStopAfterFailures-1; i++ {
		svc.onOrchestratorJobFailed(ctx, job, controlplane.ReconcileResponse{})
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReconcileStopped {
		t.Fatalf("reconcile_stopped set before %d failures", ReconcileStopAfterFailures)
	}
	var failures int
	if err := svc.db.QueryRow(`SELECT MAX(reconcile_failures) FROM application_reconcile_states WHERE application_id=?`, app.ID).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != ReconcileStopAfterFailures-1 {
		t.Fatalf("reconcile_failures = %d, want %d", failures, ReconcileStopAfterFailures-1)
	}

	svc.onOrchestratorJobFailed(ctx, job, controlplane.ReconcileResponse{})
	got, err = svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReconcileStopped {
		t.Fatalf("reconcile_stopped = false after %d failures", ReconcileStopAfterFailures)
	}
	if err := svc.db.QueryRow(`SELECT MAX(reconcile_failures) FROM application_reconcile_states WHERE application_id=?`, app.ID).Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != ReconcileStopAfterFailures {
		t.Fatalf("reconcile_failures = %d, want %d", failures, ReconcileStopAfterFailures)
	}
}
