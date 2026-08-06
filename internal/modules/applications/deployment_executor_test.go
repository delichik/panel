package applications

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
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

func TestRunDeployTaskSupersedesDisabledAndDeletedApply(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, svc *Service) Application
	}{
		{
			name: "disabled",
			prepare: func(t *testing.T, svc *Service) Application {
				t.Helper()
				app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
				if err != nil {
					t.Fatal(err)
				}
				return app
			},
		},
		{
			name: "deletion requested",
			prepare: func(t *testing.T, svc *Service) Application {
				t.Helper()
				app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
				if err != nil {
					t.Fatal(err)
				}
				app.Enabled = true
				app.DeletionRequested = true
				app.UpdatedAt = time.Now().UTC()
				if err := svc.updateApplication(ctx, app); err != nil {
					t.Fatal(err)
				}
				return app
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, runtime, _, closeStore := newTestService(t)
			defer closeStore()
			app := tc.prepare(t, svc)
			runtime.deploys = nil
			spec := appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}
			operation, err := svc.createLifecycleOperationForServerIDsWithOptions(ctx, app, spec, "", LifecycleTypeDeploy, []string{"srv-a"}, lifecycleOperationCreateOptions{
				DesiredState: appruntime.DesiredRunning,
				Action:       LifecycleTargetActionApply,
				InitialState: LifecycleTargetStateReady,
				Trigger:      "test",
			})
			if err != nil {
				t.Fatal(err)
			}
			target, err := svc.lifecycleTargetByID(ctx, lifecycleTargetID(operation.ID, "srv-a"))
			if err != nil {
				t.Fatal(err)
			}
			task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, target, "Syncing application web", "test"))
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
				t.Fatalf("apply executor must not deploy a disabled or deleted application, got %#v", runtime.deploys)
			}
			got, err := svc.lifecycleTargetByID(ctx, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != LifecycleTargetStateSuperseded {
				t.Fatalf("expected superseded lifecycle target, got %#v", got)
			}
			storedTask, err := svc.tasks.Get(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if storedTask.Status != tasks.StatusCompleted {
				t.Fatalf("expected superseded deploy task to complete, got %#v", storedTask)
			}
		})
	}
}

func TestApplyRetryRemovesPreviousContainerAfterDeleteFailure(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "old-name", "name: old-name\nimage: nginx\n")

	oldContainerName := runtimeContainerName(app)
	oldSpec := appruntime.Spec{
		InstanceID:    runtimeInstanceID(app.ID, "srv-a"),
		ContainerName: oldContainerName,
		Name:          app.Name,
		Image:         "nginx",
		Generation:    app.Generation,
		SpecHash:      app.SpecHash,
	}
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", oldSpec, appruntime.DesiredRunning, appruntime.StatusRunning, "old-container", ""); err != nil {
		t.Fatal(err)
	}
	current, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Name = "new-name"
	current.SpecYAML = "name: new-name\nimage: nginx\n"
	current.UpdatedAt = time.Now().UTC()
	if err := svc.updateApplication(ctx, current); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CreatedTargets) != 1 {
		t.Fatalf("expected one apply target, got %#v", plan)
	}
	target := plan.CreatedTargets[0]
	task, err := svc.tasks.Create(ctx, targetTaskInputForTest(t, target, "Syncing application new-name", "test"))
	if err != nil {
		t.Fatal(err)
	}
	runtime.deleteFailures = 1
	def, ok := svc.tasks.Registry().Definition(TaskTypeTargetApply)
	if !ok || def.Execute == nil {
		t.Fatal("expected target apply executor")
	}
	runErr := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks})
	if runErr == nil {
		t.Fatal("expected first apply attempt to fail while removing the previous container")
	}
	if err := svc.tasks.Fail(ctx, task.ID, runErr); err != nil {
		t.Fatal(err)
	}
	failedTarget, err := svc.lifecycleTargetByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failedTarget.State != LifecycleTargetStateFailedRetryable || failedTarget.ErrorCode != "remove_container_failed" {
		t.Fatalf("expected retryable remove_previous_container failure, got %#v", failedTarget)
	}
	instance, err := svc.runtimeInstanceForServer(ctx, app.ID, "srv-a")
	if err != nil {
		t.Fatal(err)
	}
	if instance.ContainerName != oldContainerName {
		t.Fatalf("failed retry must preserve the previous container name, got %q want %q", instance.ContainerName, oldContainerName)
	}

	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET next_run_at=? WHERE id=?`,
		formatTime(time.Now().UTC().Add(-time.Second)), target.ID); err != nil {
		t.Fatal(err)
	}
	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	if err := dispatcher.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.processExecuteTarget(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stored, err := svc.lifecycleTargetByID(ctx, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State == LifecycleTargetStateSucceeded {
			oldDeleteCount := 0
			for _, deleted := range runtime.deletes {
				if deleted == oldContainerName {
					oldDeleteCount++
				}
			}
			if oldDeleteCount < 2 {
				t.Fatalf("retry should try to remove the old container again, deletes=%#v", runtime.deletes)
			}
			finalInstance, err := svc.runtimeInstanceForServer(ctx, app.ID, "srv-a")
			if err != nil {
				t.Fatal(err)
			}
			newContainerName := runtimeContainerName(current)
			if finalInstance.ContainerName != newContainerName {
				t.Fatalf("successful apply should switch the instance to the new container name, got %q want %q", finalInstance.ContainerName, newContainerName)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, err := svc.lifecycleTargetByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("retry apply did not succeed, target=%#v deletes=%#v", stored, runtime.deletes)
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
