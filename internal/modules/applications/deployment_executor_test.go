package applications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
)

func TestRunDeployTaskClaimsReadyTargetBeforeRuntimeMutation(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, plan.CreatedTargets[0], "Syncing application web", "test"))
	if err != nil {
		t.Fatal(err)
	}
	claimObservedBeforeMutation := false
	runtime.writeHook = func(agentcontract.RuntimeWriteFilesRequest) {
		target, err := svc.lifecycleTargetByID(ctx, plan.CreatedTargets[0].ID)
		if err != nil {
			t.Fatalf("load target during write: %v", err)
		}
		if target.ClaimedTaskID != task.ID || target.LeaseOwner != lifecycleTaskLeaseOwner(task.ID) {
			t.Fatalf("target should be claimed before remote mutation, got %#v task=%#v", target, task)
		}
		claimObservedBeforeMutation = true
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 1 {
		t.Fatalf("expected runtime mutation after claim, got deploys=%#v", runtime.deploys)
	}
	if !claimObservedBeforeMutation {
		t.Fatal("expected claim check before first remote mutation")
	}
	target, err := svc.lifecycleTargetByID(ctx, plan.CreatedTargets[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateSucceeded {
		t.Fatalf("target should finish succeeded, got %#v", target)
	}
	if target.LeaseOwner != "" || target.LeaseExpiresAt != nil {
		t.Fatalf("succeeded target should release task lease, got %#v", target)
	}
	if target.ClaimedTaskID != task.ID {
		t.Fatalf("succeeded target should retain claimed task for log trace, got %#v task=%#v", target, task)
	}
}

func TestRunDeployTaskSkipsTargetClaimedByAnotherWorker(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?, status=?, lease_owner=?, lease_expires_at=?, claimed_task_id=?, updated_at=?
		WHERE id=?`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		"other-worker",
		formatTime(time.Now().UTC().Add(time.Minute)),
		"other-task",
		formatTime(time.Now().UTC()),
		targetID); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(deployTaskParams{
		AppID:                app.ID,
		ServerID:             "srv-a",
		LifecycleOperationID: plan.CreatedTargets[0].OperationID,
		LifecycleTargetID:    targetID,
		Generation:           app.Generation,
		SpecHash:             app.SpecHash,
		Action:               LifecycleTargetActionApply,
	})
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeTargetApply,
		ServerID:     "srv-a",
		ResourceType: "application",
		ResourceID:   app.ID,
		ParamsJSON:   string(params),
		Summary:      "Syncing application web",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("executor should not mutate runtime for target claimed elsewhere, got %#v", runtime.deploys)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.ClaimedTaskID != "other-task" || target.LeaseOwner != "other-worker" {
		t.Fatalf("foreign claim should remain untouched, got %#v", target)
	}
}

func TestRunDeployTaskStopsWhenLeaseLostBeforeRemoteMutation(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	svc.operationQueue = leaseStealingQueue{svc: svc, targetID: targetID}
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, plan.CreatedTargets[0], "Syncing application web", "test"))
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	err = def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks})
	if !errors.Is(err, errLifecycleTargetLeaseLost) {
		t.Fatalf("expected lease lost error, got %v", err)
	}
	if len(runtime.writes) != 0 || len(runtime.deploys) != 0 || len(runtime.pulls) != 0 {
		t.Fatalf("executor should stop before remote mutation, writes=%#v pulls=%#v deploys=%#v", runtime.writes, runtime.pulls, runtime.deploys)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.LeaseOwner != "other-worker" || target.ClaimedTaskID != "other-task" {
		t.Fatalf("lease stealing queue should retain ownership, got %#v", target)
	}
}

func TestLifecycleTargetFailureDoesNotOverwriteTerminalTarget(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	now := formatTime(time.Now().UTC())
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,attempt=?,next_run_at='',finished_at=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateSucceeded,
		lifecycleStatusForState(LifecycleTargetStateSucceeded),
		2,
		now,
		now,
		targetID); err != nil {
		t.Fatal(err)
	}
	if err := svc.failLifecycleTargetExecution(ctx, targetID, "pull_image", "pull_image_failed", errors.New("late failure"), true); err != nil {
		t.Fatal(err)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateSucceeded || target.ErrorCode != "" {
		t.Fatalf("terminal target should not be overwritten by late failure, got %#v", target)
	}
}

func TestTargetTaskFailureDoesNotOverwriteRetryableTarget(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	target := plan.CreatedTargets[0]
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, target, "Syncing application web", "test"))
	if err != nil {
		t.Fatal(err)
	}
	nextRunAt := time.Now().UTC().Add(time.Minute)
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,attempt=?,next_run_at=?,error_code=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateFailedRetryable,
		lifecycleStatusForState(LifecycleTargetStateFailedRetryable),
		1,
		formatTime(nextRunAt),
		"pull_image_failed",
		formatTime(time.Now().UTC()),
		target.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.failLifecycleTargetForTask(ctx, task, errors.New("task wrapper failed")); err != nil {
		t.Fatal(err)
	}
	got, err := svc.lifecycleTargetByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != LifecycleTargetStateFailedRetryable || got.NextRunAt == nil || got.ErrorCode != "pull_image_failed" {
		t.Fatalf("task failure should preserve retryable target, got %#v", got)
	}
}

func TestTaskFallbackClaimRespectsRetryBackoff(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,next_run_at=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateFailedRetryable,
		lifecycleStatusForState(LifecycleTargetStateFailedRetryable),
		formatTime(time.Now().UTC().Add(time.Minute)),
		formatTime(time.Now().UTC()),
		targetID); err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, plan.CreatedTargets[0], "Syncing application web", "test"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.ensureLifecycleTargetClaimedForTask(ctx, task, deployTaskOptions(task))
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("fallback task claim should respect target retry backoff")
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateFailedRetryable || target.ClaimedTaskID != "" {
		t.Fatalf("backoff target should remain unclaimed, got %#v", target)
	}
}

func TestTaskFallbackRejectsExpiredLease(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, plan.CreatedTargets[0], "Syncing application web", "test"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?, status=?, lease_owner=?, lease_expires_at=?, claimed_task_id=?, updated_at=?
		WHERE id=?`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		lifecycleTaskLeaseOwner(task.ID),
		formatTime(time.Now().UTC().Add(-time.Minute)),
		task.ID,
		formatTime(time.Now().UTC()),
		targetID); err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 0 || len(runtime.writes) != 0 || len(runtime.pulls) != 0 {
		t.Fatalf("expired lease should not mutate runtime, writes=%#v pulls=%#v deploys=%#v", runtime.writes, runtime.pulls, runtime.deploys)
	}
	got, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != LifecycleTargetStateClaimed || got.ClaimedTaskID != task.ID {
		t.Fatalf("expired lease should be left for recovery, got %#v", got)
	}
}

type leaseStealingQueue struct {
	svc      *Service
	targetID string
}

func (q leaseStealingQueue) Execute(ctx context.Context, serverID string, run func(context.Context) error) error {
	_, err := q.svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET lease_owner=?, claimed_task_id=?, lease_expires_at=?, updated_at=?
		WHERE id=?`,
		"other-worker",
		"other-task",
		formatTime(time.Now().UTC().Add(time.Minute)),
		formatTime(time.Now().UTC()),
		q.targetID)
	if err != nil {
		return err
	}
	return run(ctx)
}
