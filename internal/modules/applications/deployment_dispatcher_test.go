package applications

import (
	"context"
	"database/sql"
	"testing"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/tasks"
)

func TestDeploymentDispatcherClaimExecuteIsConditional(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateReady, lifecycleStatusForState(LifecycleTargetStateReady), formatTime(time.Now().UTC()), targetID); err != nil {
		t.Fatal(err)
	}

	first := NewDeploymentDispatcher(svc, WithDeploymentDispatcherOwner("worker-a")).(*deploymentDispatcher)
	second := NewDeploymentDispatcher(svc, WithDeploymentDispatcherOwner("worker-b")).(*deploymentDispatcher)
	claimed, ok, err := first.claimExecuteTarget(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || claimed.State != LifecycleTargetStateClaimed || claimed.ClaimedTaskID == "" || claimed.LeaseOwner != lifecycleTaskLeaseOwner(claimed.ClaimedTaskID) {
		t.Fatalf("expected first dispatcher to claim target, got ok=%v target=%#v", ok, claimed)
	}
	_, ok, err = second.claimExecuteTarget(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second dispatcher should not claim an already claimed target")
	}
	stored, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LeaseOwner != lifecycleTaskLeaseOwner(claimed.ClaimedTaskID) || stored.ClaimedTaskID != claimed.ClaimedTaskID {
		t.Fatalf("claim should remain owned by first dispatcher, got %#v", stored)
	}
}

func TestDeploymentDispatcherClaimTaskIsScheduledUntilBound(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	target := plan.CreatedTargets[0]
	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	task, err := dispatcher.createClaimTask(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusScheduled || task.NextRunAt == nil || !task.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("dispatcher-created task must stay invisible to the polling worker until it is bound, got %#v", task)
	}
	now := time.Now().UTC()
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?, status=?, lease_owner=?, lease_expires_at=?, claimed_task_id=?, updated_at=?
		WHERE id=?`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		lifecycleTaskLeaseOwner(task.ID),
		formatTime(now.Add(time.Minute)),
		task.ID,
		formatTime(now),
		target.ID); err != nil {
		t.Fatal(err)
	}
	dispatcher.runClaimedTask(ctx, task.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stored, err := svc.lifecycleTargetByID(ctx, target.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State == LifecycleTargetStateSucceeded {
			if stored.ClaimedTaskID != task.ID {
				t.Fatalf("bound task should own the lifecycle target, got %#v", stored)
			}
			taskDeadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(taskDeadline) {
				storedTask, err := svc.tasks.Get(ctx, task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if storedTask.Status == tasks.StatusCompleted {
					if len(runtime.deploys) != 1 {
						t.Fatalf("bound dispatcher task should run immediately, got %#v", runtime.deploys)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			storedTask, err := svc.tasks.Get(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("bound dispatcher task should complete after execution, got %#v", storedTask)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, err := svc.lifecycleTargetByID(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("bound dispatcher task did not finish, target=%#v", stored)
}

func TestDeploymentDispatcherStartReturnsRecoveryError(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	if err := svc.lifecycleDB().Close(); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDeploymentDispatcher(svc).(*deploymentDispatcher)
	if err := dispatcher.Start(context.Background()); err == nil {
		t.Fatal("expected dispatcher start to return recovery error")
	}
	dispatcher.mu.Lock()
	running := dispatcher.cancel != nil
	dispatcher.mu.Unlock()
	if running {
		t.Fatal("dispatcher should not stay running after startup recovery fails")
	}
}

func TestDeploymentDispatcherExecuteRunsClaimedTargetTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	targetID := plan.CreatedTargets[0].ID
	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	if err := dispatcher.processExecuteTarget(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		target, err := svc.lifecycleTargetByID(ctx, targetID)
		if err != nil {
			t.Fatal(err)
		}
		if target.State == LifecycleTargetStateSucceeded {
			if target.ClaimedTaskID == "" {
				t.Fatalf("succeeded target should retain claimed task: %#v", target)
			}
			if len(runtime.deploys) != 1 {
				t.Fatalf("expected one runtime deployment, got %#v", runtime.deploys)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("dispatcher-created task did not execute target, target=%#v deploys=%#v", target, runtime.deploys)
}

func TestDeploymentDispatcherRecoverRequeuesDurableTargets(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a", "srv-b"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	readyID := lifecycleTargetID(operation.ID, "srv-a")
	verifyID := lifecycleTargetID(operation.ID, "srv-b")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,updated_at=? WHERE id=?`,
		LifecycleTargetStatePlanned, lifecycleStatusForState(LifecycleTargetStatePlanned), formatTime(now), readyID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,lease_owner='',lease_expires_at='',updated_at=? WHERE id=?`,
		LifecycleTargetStateVerifying, lifecycleStatusForState(LifecycleTargetStateVerifying), formatTime(now), verifyID); err != nil {
		t.Fatal(err)
	}
	terminalApp, err := svc.Create(ctx, SaveInput{Name: "api", Enabled: false, SpecYAML: "name: api\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	terminalOperation, err := svc.createLifecycleOperationForServerIDs(ctx, terminalApp, appruntime.Spec{Generation: terminalApp.Generation, SpecHash: terminalApp.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	terminalID := lifecycleTargetID(terminalOperation.ID, "srv-a")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,finished_at=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateSucceeded, lifecycleStatusForState(LifecycleTargetStateSucceeded), formatTime(now), formatTime(now), terminalID); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	if err := dispatcher.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	executeID, ok := dispatcher.executeQueue.dequeue()
	if !ok || executeID != readyID {
		t.Fatalf("expected planned target to be recovered and requeued for execute, got %q ok=%v", executeID, ok)
	}
	recovered, err := svc.lifecycleTargetByID(ctx, readyID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != LifecycleTargetStateReady {
		t.Fatalf("planned target should recover to ready, got %#v", recovered)
	}
	verifyQueuedID, ok := dispatcher.verifyQueue.dequeue()
	if !ok || verifyQueuedID != verifyID {
		t.Fatalf("expected verifying target to be requeued for verify, got %q ok=%v", verifyQueuedID, ok)
	}
	aggregateID, ok := dispatcher.aggregateQueue.dequeue()
	if !ok || aggregateID == "" {
		t.Fatalf("expected terminal operation to be requeued for aggregate, got %q ok=%v", aggregateID, ok)
	}
}

func TestDeploymentDispatcherRecoverExpiredLeaseSchedulesRetry(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,stage=?,lease_owner=?,lease_expires_at=?,claimed_task_id=?,updated_at=?
		WHERE id=?`,
		LifecycleTargetStateApplying,
		lifecycleStatusForState(LifecycleTargetStateApplying),
		"pull_image",
		"task:old",
		formatTime(time.Now().UTC().Add(-time.Minute)),
		"old",
		formatTime(time.Now().UTC()),
		targetID); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	if err := dispatcher.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateFailedRetryable || target.Attempt != 1 || target.NextRunAt == nil || target.ErrorCode != "lease_lost" {
		t.Fatalf("expected expired mutation lease to schedule retryable lease_lost, got %#v", target)
	}
	if !target.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("expected retry to be scheduled in the future, got %#v", target.NextRunAt)
	}
}

func TestDeploymentDispatcherCreateTaskFailureMarksRetryable(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,action=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateReady, lifecycleStatusForState(LifecycleTargetStateReady), LifecycleTargetActionApply, formatTime(time.Now().UTC()), targetID); err != nil {
		t.Fatal(err)
	}
	svc.tasks = nil

	dispatcher := NewDeploymentDispatcher(svc).(*deploymentDispatcher)
	if _, ok, err := dispatcher.claimExecuteTarget(ctx, targetID); err != nil || ok {
		t.Fatalf("expected unavailable task service to leave target unclaimed without panic, ok=%v err=%v", ok, err)
	}
	svc.tasks = tasks.NewService(svc.lifecycleDB())
	if _, _, err := dispatcher.claimExecuteTarget(ctx, targetID); err == nil {
		t.Fatal("expected unregistered task type to fail task creation")
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateFailedRetryable || target.ErrorCode != "task_create_failed" {
		t.Fatalf("expected task creation failure to mark retryable, got %#v", target)
	}
	if target.Attempt != 1 || target.NextRunAt == nil {
		t.Fatalf("expected task creation failure to record retry backoff, got %#v", target)
	}
	svc.RegisterTasks(svc.tasks)
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET next_run_at=? WHERE id=?`, formatTime(time.Now().UTC().Add(-time.Second)), targetID); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recoveredTarget, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredTarget.State != LifecycleTargetStateReady {
		t.Fatalf("due retryable target should recover to ready before claim, got %#v", recoveredTarget)
	}
	executeID, ok := dispatcher.executeQueue.dequeue()
	if !ok || executeID != targetID {
		t.Fatalf("expected due task_create_failed target to be requeued, got %q ok=%v", executeID, ok)
	}
}

func TestDeploymentDispatcherClaimVerifyIsConditional(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateVerifying, lifecycleStatusForState(LifecycleTargetStateVerifying), formatTime(time.Now().UTC()), targetID); err != nil {
		t.Fatal(err)
	}

	first := NewDeploymentDispatcher(svc, WithDeploymentDispatcherOwner("verifier-a")).(*deploymentDispatcher)
	second := NewDeploymentDispatcher(svc, WithDeploymentDispatcherOwner("verifier-b")).(*deploymentDispatcher)
	ok, err := first.claimVerifyTarget(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected first verifier claim to succeed")
	}
	ok, err = second.claimVerifyTarget(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second verifier claim should not steal an unexpired lease")
	}
}

func TestDeploymentDispatcherVerifierAndAggregatorConvergeTargetAndOperation(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{ApplicationID: app.ID, InstanceID: runtimeInstanceID(app.ID, "srv-a"), Name: "web", Image: "nginx", Generation: app.Generation, SpecHash: app.SpecHash}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, spec, "task-apply", LifecycleTypeDeploy, []string{"srv-a"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	targetID := lifecycleTargetID(operation.ID, "srv-a")
	if err := svc.upsertRuntimeInstance(ctx, app.ID, "srv-a", spec, appruntime.DesiredRunning, appruntime.StatusRunning, "container-srv-a", ""); err != nil {
		t.Fatal(err)
	}
	now := formatTime(time.Now().UTC())
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?,status=?,stage=?,claimed_task_id=?,lease_owner='',lease_expires_at='',updated_at=?
		WHERE id=?`,
		LifecycleTargetStateVerifying,
		lifecycleStatusForState(LifecycleTargetStateVerifying),
		"inspect",
		"task-apply",
		now,
		targetID); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherOwner("verifier")).(*deploymentDispatcher)
	if err := dispatcher.processVerifyTarget(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if target.State != LifecycleTargetStateSucceeded || target.LeaseOwner != "" || target.ClaimedTaskID != "task-apply" {
		t.Fatalf("expected verifier to succeed target and preserve task trace, got %#v", target)
	}
	operationID, ok := dispatcher.aggregateQueue.dequeue()
	if !ok || operationID != operation.ID {
		t.Fatalf("expected verifier to enqueue aggregate, got %q ok=%v", operationID, ok)
	}
	if err := dispatcher.processAggregateOperation(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT status FROM application_lifecycle_operations WHERE id=?`, operation.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != LifecycleStatusDeployed {
		t.Fatalf("expected aggregate to deploy operation, got %s", status)
	}
}

func TestDeploymentAggregatorKeepsRetryableTargetsActive(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a", "srv-b"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(time.Now().UTC())
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,finished_at=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateSucceeded, lifecycleStatusForState(LifecycleTargetStateSucceeded), now, now, lifecycleTargetID(operation.ID, "srv-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets SET state=?,status=?,error=?,next_run_at=?,updated_at=? WHERE id=?`,
		LifecycleTargetStateFailedRetryable, lifecycleStatusForState(LifecycleTargetStateFailedRetryable), "temporary failure", formatTime(time.Now().UTC().Add(time.Minute)), now, lifecycleTargetID(operation.ID, "srv-b")); err != nil {
		t.Fatal(err)
	}

	dispatcher := NewDeploymentDispatcher(svc).(*deploymentDispatcher)
	if err := dispatcher.processAggregateOperation(ctx, operation.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	var finishedAt sql.NullString
	if err := svc.lifecycleDB().QueryRowContext(ctx, `SELECT status,finished_at FROM application_lifecycle_operations WHERE id=?`, operation.ID).Scan(&status, &finishedAt); err != nil {
		t.Fatal(err)
	}
	if status != LifecycleStatusDeploying || finishedAt.Valid {
		t.Fatalf("retryable target should keep operation active, status=%s finished=%v", status, finishedAt.Valid)
	}
}

func TestFailStaleTargetTaskAnchorsCancelsObsoleteAnchors(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n", DeploymentMode: DeploymentModeAll})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := svc.createLifecycleOperationForServerIDs(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, "", LifecycleTypeDeploy, []string{"srv-a", "srv-b"}, appruntime.DesiredRunning)
	if err != nil {
		t.Fatal(err)
	}
	var obsoleteTarget, liveTarget LifecycleTarget
	for _, target := range operation.Targets {
		if target.ServerID == "srv-a" {
			obsoleteTarget = target
		} else {
			liveTarget = target
		}
	}
	if err := svc.supersedeLifecycleTargetIfActive(ctx, obsoleteTarget.ID, "obsolete before start"); err != nil {
		t.Fatal(err)
	}

	manager := tasks.NewManager(svc.tasks)
	obsoleteAnchor, _, err := manager.Create(ctx, targetTaskInputForTest(t, obsoleteTarget, "obsolete anchor", "test"), tasks.Trigger{Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	liveAnchor, _, err := manager.Create(ctx, targetTaskInputForTest(t, liveTarget, "live anchor", "test"), tasks.Trigger{Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := svc.lifecycleDB().ExecContext(ctx, `UPDATE application_lifecycle_targets
		SET state=?, status=?, lease_owner=?, lease_expires_at=?, claimed_task_id=?, updated_at=?
		WHERE id=?`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		lifecycleTaskLeaseOwner(liveAnchor.ID),
		formatTime(now.Add(time.Minute)),
		liveAnchor.ID,
		formatTime(now),
		liveTarget.ID); err != nil {
		t.Fatal(err)
	}

	if err := svc.FailStaleTargetTaskAnchors(ctx); err != nil {
		t.Fatal(err)
	}

	gotObsolete, err := svc.tasks.Get(ctx, obsoleteAnchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotObsolete.Status != tasks.StatusCancelled {
		t.Fatalf("obsolete anchor should be cancelled, got %#v", gotObsolete)
	}
	gotLive, err := svc.tasks.Get(ctx, liveAnchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotLive.Status != tasks.StatusQueued {
		t.Fatalf("live anchor should stay queued, got %#v", gotLive)
	}
}

// TestDeploymentDispatcherVerifyDoesNotWaitForMutationLease 回归测试：
// apply 完成转入 verifying 时必须立即释放变更租约，验证无需等租约（3 分钟）
// 自然过期；否则每次部署都会被拖慢约 3 分钟，入口代理还会因此形成
// "同步→等待→完成→再同步"的自循环。
func TestDeploymentDispatcherVerifyDoesNotWaitForMutationLease(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app := enabledTestApplication(t, svc, "web", "name: web\nimage: nginx\n")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"srv-a"}, Force: true, TriggerType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CreatedTargets) != 1 {
		t.Fatalf("expected one apply target, got %#v", plan)
	}
	targetID := plan.CreatedTargets[0].ID
	dispatcher := NewDeploymentDispatcher(svc, WithDeploymentDispatcherQueueSize(8)).(*deploymentDispatcher)
	// 接入真实调度器：apply 完成后的验证走 EnqueueVerify/claimVerifyTarget 路径。
	svc.deployment = dispatcher
	if err := dispatcher.processExecuteTarget(ctx, targetID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		target, err := svc.lifecycleTargetByID(ctx, targetID)
		if err != nil {
			t.Fatal(err)
		}
		if target.State == LifecycleTargetStateVerifying {
			if target.LeaseOwner != "" || target.LeaseExpiresAt != nil {
				t.Fatalf("verifying target must have released the mutation lease, got %#v", target)
			}
			// 验证应立即可认领：修复前要等 3 分钟租约过期。
			ok, err := dispatcher.claimVerifyTarget(ctx, targetID)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatalf("verify must be claimable right after apply completes, target=%#v", target)
			}
			if err := dispatcher.processVerifyTarget(ctx, targetID); err != nil {
				t.Fatal(err)
			}
			succeeded, err := svc.lifecycleTargetByID(ctx, targetID)
			if err != nil {
				t.Fatal(err)
			}
			if succeeded.State != LifecycleTargetStateSucceeded {
				t.Fatalf("expected verify to succeed target, got %#v", succeeded)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	target, err := svc.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("apply task did not reach verifying state, target=%#v", target)
}
