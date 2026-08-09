package applications

import (
	"context"
	"sync"
	"testing"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
)

func TestPlanApplicationDeploymentCreatesReadyTargets(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	result, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "application_save"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedTargets) != 2 {
		t.Fatalf("expected targets for both servers, got %#v", result)
	}
	for _, target := range result.CreatedTargets {
		if target.State != LifecycleTargetStateReady || target.Status != LifecycleTargetStatusPending || target.Action != LifecycleTargetActionApply {
			t.Fatalf("target should be ready apply with pending projection, got %#v", target)
		}
		if target.DesiredGeneration != app.Generation || target.DesiredSpecHash != app.SpecHash {
			t.Fatalf("target desired revision mismatch, got %#v app=%#v", target, app)
		}
	}
}

func TestPlanApplicationDeploymentReusesActiveTarget(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	first, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.CreatedTargets) != 1 {
		t.Fatalf("expected first plan to create one target, got %#v", first)
	}
	if len(second.CreatedTargets) != 0 || len(second.ReusedTargets) != 1 || second.ReusedTargets[0].ID != first.CreatedTargets[0].ID {
		t.Fatalf("expected second plan to reuse active target, got %#v after %#v", second, first)
	}
}

func TestPlanApplicationDeploymentDoesNotBumpApplicationVersion(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	before, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := before.Version
	beforeUpdatedAt := before.UpdatedAt
	if _, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "scheduler"}); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != beforeVersion || !after.UpdatedAt.Equal(beforeUpdatedAt) {
		t.Fatalf("planning should not bump user configuration version, before=%d/%s after=%#v", beforeVersion, beforeUpdatedAt.Format(time.RFC3339Nano), after)
	}
	if !after.Enabled {
		t.Fatalf("enabled application should remain enabled after planning, got %#v", after)
	}
}

func TestCreateLifecycleOperationRollsBackOnActiveTargetConflict(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	spec := appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}

	if _, err := svc.createLifecycleOperationForServerIDsWithOptions(ctx, app, spec, "", LifecycleTypeDeploy, []string{"srv-a"}, lifecycleOperationCreateOptions{
		DesiredState: appruntime.DesiredRunning,
		Action:       LifecycleTargetActionApply,
		InitialState: LifecycleTargetStateReady,
		Trigger:      "test",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.createLifecycleOperationForServerIDsWithOptions(ctx, app, spec, "", LifecycleTypeDeploy, []string{"srv-a"}, lifecycleOperationCreateOptions{
		DesiredState: appruntime.DesiredRunning,
		Action:       LifecycleTargetActionApply,
		InitialState: LifecycleTargetStateReady,
		Trigger:      "test",
	})
	if err == nil || !isLifecycleTargetActiveConflict(err) {
		t.Fatalf("expected active target conflict, got %v", err)
	}
	var operationCount int
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM application_lifecycle_operations WHERE application_id=?`, app.ID).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	var targetCount int
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM application_lifecycle_targets WHERE application_id=?`, app.ID).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 || targetCount != 1 {
		t.Fatalf("conflicting create must roll back operation and targets, operations=%d targets=%d", operationCount, targetCount)
	}
}

func TestPlanTargetActionsConcurrentConflictReusesExistingTarget(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	current, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{Generation: current.Generation, SpecHash: current.SpecHash}

	start := make(chan struct{})
	type planOutcome struct {
		created int
		reused  int
		err     error
	}
	results := make(chan planOutcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, planErr := svc.planTargetActions(ctx, current, spec, []string{"srv-a"}, LifecycleTargetActionApply, appruntime.DesiredRunning, "test")
			results <- planOutcome{created: len(result.CreatedTargets), reused: len(result.ReusedTargets), err: planErr}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	createdTotal, reusedTotal := 0, 0
	for outcome := range results {
		if outcome.err != nil {
			t.Fatalf("concurrent planning returned error: %v", outcome.err)
		}
		createdTotal += outcome.created
		reusedTotal += outcome.reused
	}
	if createdTotal != 1 || reusedTotal != 1 {
		t.Fatalf("concurrent planning should create once and reuse once, created=%d reused=%d", createdTotal, reusedTotal)
	}
	var operationCount int
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM application_lifecycle_operations WHERE application_id=?`, app.ID).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	var targetCount int
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM application_lifecycle_targets WHERE application_id=?`, app.ID).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 || targetCount != 1 {
		t.Fatalf("concurrent planning must not leave orphan operations, operations=%d targets=%d", operationCount, targetCount)
	}
}

func TestPlanApplicationDeploymentUsesObservedRuntimeDriftOverCachedRunningState(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	spec := appruntime.Spec{
		InstanceID:    runtimeInstanceID(app.ID, "srv-a"),
		ContainerName: runtimeContainerName(app),
		Generation:    app.Generation,
		SpecHash:      app.SpecHash,
	}
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", spec, appruntime.DesiredRunning, appruntime.StatusRunning, "container-srv-a", ""); err != nil {
		t.Fatal(err)
	}

	normal, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, TriggerType: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(normal.CreatedTargets) != 0 {
		t.Fatalf("cached running instance should satisfy a normal plan, got %#v", normal)
	}

	observed, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID:        app.ID,
		ServerIDs:            []string{"srv-a"},
		ObservedRuntimeDrift: true,
		TriggerType:          "agent_report",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observed.CreatedTargets) != 1 || observed.CreatedTargets[0].ServerID != "srv-a" {
		t.Fatalf("observed runtime drift should create an apply target despite cached running state, got %#v", observed)
	}

	repeated, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID:        app.ID,
		ServerIDs:            []string{"srv-a"},
		ObservedRuntimeDrift: true,
		TriggerType:          "agent_report",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.CreatedTargets) != 0 || len(repeated.ReusedTargets) != 1 || repeated.ReusedTargets[0].ID != observed.CreatedTargets[0].ID {
		t.Fatalf("repeated observed drift should reuse the active target, got %#v after %#v", repeated, observed)
	}
}

func TestPlanApplicationDeploymentSupersedesStaleApplyRevision(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx:1\n")

	first, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	app.SpecYAML = "name: web\nimage: nginx:2\n"
	app.Generation++
	app.SpecHash = "next-spec"
	if err := svc.updateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	second, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.SupersededTargets) != 1 || second.SupersededTargets[0].ID != first.CreatedTargets[0].ID || len(second.CreatedTargets) != 1 {
		t.Fatalf("expected stale apply superseded and replaced, got first=%#v second=%#v", first, second)
	}
	oldTarget, err := svc.lifecycleTargetByID(ctx, first.CreatedTargets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldTarget.State != LifecycleTargetStateSuperseded {
		t.Fatalf("old target should be superseded, got %#v", oldTarget)
	}
}

func TestPlanApplicationDeploymentPurgeReplacesEarlyApply(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	applyPlan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	purgePlan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, StopServers: []string{"srv-a"}, Purge: true, Force: true, TriggerType: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if len(purgePlan.CreatedTargets) != 1 || purgePlan.CreatedTargets[0].Action != LifecycleTargetActionPurge {
		t.Fatalf("expected purge target to replace early apply, got %#v", purgePlan)
	}
	oldTarget, err := svc.lifecycleTargetByID(ctx, applyPlan.CreatedTargets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldTarget.State != LifecycleTargetStateSuperseded {
		t.Fatalf("old apply target should be superseded, got %#v", oldTarget)
	}
}

func TestPlanApplicationDeploymentRemovedServerPurgesRetryableApply(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           false,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{
		InstanceID:    runtimeInstanceID(app.ID, "srv-a"),
		ContainerName: runtimeContainerName(app),
		Generation:    app.Generation,
		SpecHash:      app.SpecHash,
	}
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", spec, appruntime.DesiredRunning, appruntime.StatusRunning, "container-srv-a", ""); err != nil {
		t.Fatal(err)
	}
	app.Enabled = true
	if err := svc.updateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}
	applyPlan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(applyPlan.CreatedTargets) != 1 {
		t.Fatalf("expected one apply target, got %#v", applyPlan)
	}
	applyTarget := applyPlan.CreatedTargets[0]
	if applyTarget.Action != LifecycleTargetActionApply {
		t.Fatalf("expected apply target, got %#v", applyTarget)
	}
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,lease_owner='',lease_expires_at='',next_run_at=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateFailedRetryable, LifecycleTargetStatusFailed, formatTime(time.Now().UTC().Add(time.Minute)), formatTime(time.Now().UTC()), applyTarget.ID); err != nil {
		t.Fatal(err)
	}
	app.DeploymentServers = []string{"srv-b"}
	if err := svc.updateApplication(ctx, app); err != nil {
		t.Fatal(err)
	}

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "application_save"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.SupersededTargets) != 1 || plan.SupersededTargets[0].ID != applyTarget.ID {
		t.Fatalf("expected retryable apply on removed server to be superseded, got %#v", plan)
	}
	foundPurge := false
	for _, target := range plan.CreatedTargets {
		if target.ServerID == "srv-a" && target.Action == LifecycleTargetActionPurge {
			foundPurge = true
		}
	}
	if !foundPurge {
		t.Fatalf("expected purge target for removed server, got %#v", plan)
	}
}

func TestPlanApplicationDeploymentSkipsAutoDriftWhenReconcileStopped(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	if _, err := svc.db.ExecContext(ctx, `UPDATE applications SET reconcile_stopped=1 WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}

	result, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID:        app.ID,
		ServerIDs:            []string{"srv-a"},
		ObservedRuntimeDrift: true,
		TriggerType:          "scheduler",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedTargets) != 0 || len(result.OperationIDs) != 0 {
		t.Fatalf("auto drift must not create targets while reconcile is stopped, got %#v", result)
	}
	var stopped int
	if err := svc.db.QueryRowContext(ctx, `SELECT reconcile_stopped FROM applications WHERE id=?`, app.ID).Scan(&stopped); err != nil {
		t.Fatal(err)
	}
	if stopped != 1 {
		t.Fatalf("auto drift must keep the stopped flag, got %d", stopped)
	}
}

func TestPlanApplicationDeploymentUserSyncResetsReconcileStopped(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	if _, err := svc.db.ExecContext(ctx, `UPDATE applications SET reconcile_stopped=1 WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}

	result, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID: app.ID,
		ServerIDs:     []string{"srv-a"},
		Force:         true,
		TriggerType:   "application_sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedTargets) != 1 {
		t.Fatalf("expected user sync to create one target, got %#v", result)
	}
	var stopped int
	if err := svc.db.QueryRowContext(ctx, `SELECT reconcile_stopped FROM applications WHERE id=?`, app.ID).Scan(&stopped); err != nil {
		t.Fatal(err)
	}
	if stopped != 0 {
		t.Fatalf("user sync must clear the stopped flag, got %d", stopped)
	}
}

func TestRecordApplicationReconcileFailureMarksStoppedAtTen(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")
	spec := appruntime.Spec{
		InstanceID:    runtimeInstanceID(app.ID, "srv-a"),
		ContainerName: runtimeContainerName(app),
		Generation:    app.Generation,
		SpecHash:      app.SpecHash,
	}
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", spec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ReconcileStopAfterFailures; i++ {
		if err := svc.recordApplicationReconcileFailure(ctx, app.ID); err != nil {
			t.Fatal(err)
		}
	}
	var stopped int
	if err := svc.db.QueryRowContext(ctx, `SELECT reconcile_stopped FROM applications WHERE id=?`, app.ID).Scan(&stopped); err != nil {
		t.Fatal(err)
	}
	if stopped != 1 {
		t.Fatalf("expected stopped flag after %d failures, got %d", ReconcileStopAfterFailures, stopped)
	}

	// A user retry clears the flag and starts a fresh attempt.
	result, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID: app.ID,
		ServerIDs:     []string{"srv-a"},
		Force:         true,
		TriggerType:   "application_sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CreatedTargets) != 1 {
		t.Fatalf("expected user retry to create target, got %#v", result)
	}
	if err := svc.db.QueryRowContext(ctx, `SELECT reconcile_stopped FROM applications WHERE id=?`, app.ID).Scan(&stopped); err != nil {
		t.Fatal(err)
	}
	if stopped != 0 {
		t.Fatalf("expected user retry to clear stopped flag, got %d", stopped)
	}
}

func enabledTestApplication(t *testing.T, svc *Service, name, specYAML string) Application {
	t.Helper()
	app, err := svc.Create(context.Background(), SaveInput{Name: name, Enabled: false, SpecYAML: specYAML, DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	app.Enabled = true
	if err := svc.updateApplication(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	return app
}
