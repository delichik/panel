package applications

import (
	"context"
	"testing"

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
