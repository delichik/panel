package tasks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/storage"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(store.AppDB())
}

func TestTaskLifecycleAndLogs(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "test task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Advance(ctx, task.ID, "running", "password=secret"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	logs, _, err := svc.Logs(ctx, task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || strings.Contains(logs[0].Line, "secret") {
		t.Fatalf("expected redacted log, got %#v", logs)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted || got.FinishedAt == nil {
		t.Fatalf("unexpected task state: %#v", got)
	}
	if got.Percentage == nil || *got.Percentage != 100 {
		t.Fatalf("expected completed task progress to be 100, got %#v", got.Percentage)
	}
}

func TestCreateCompletedTaskHasFinishedState(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "application_deploy", Status: StatusCompleted, Summary: "registered"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.FinishedAt == nil || got.Percentage == nil || *got.Percentage != 100 {
		t.Fatalf("expected completed task to have finished time and 100 percent progress: %#v", got)
	}
}

func TestListFiltersByStatusServerAndType(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	matching, err := svc.Create(ctx, CreateInput{Type: "metrics_collect", ServerID: "srv_1", Summary: "metrics"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, matching.ID); err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, CreateInput{Type: "package_refresh", ServerID: "srv_2", Summary: "packages"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, other.ID, "done"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.List(ctx, ListFilter{
		Status:   string(StatusRunning),
		ServerID: "srv_1",
		Type:     "metrics_collect",
		Limit:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != matching.ID {
		t.Fatalf("unexpected filtered tasks: %#v", got)
	}
}

func TestListFiltersByMultipleStatusesAndTypes(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	runningApp, err := svc.Create(ctx, CreateInput{Type: "application_deploy", ServerID: "srv_1", Summary: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, runningApp.ID); err != nil {
		t.Fatal(err)
	}
	failedPackage, err := svc.Create(ctx, CreateInput{Type: "package_refresh", ServerID: "srv_1", Summary: "packages", Status: StatusFailed})
	if err != nil {
		t.Fatal(err)
	}
	completedApp, err := svc.Create(ctx, CreateInput{Type: "application_restart", ServerID: "srv_1", Summary: "restart", Status: StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.List(ctx, ListFilter{
		Statuses: []string{StatusRunning, StatusFailed},
		Types:    []string{"application_deploy", "package_refresh"},
		ServerID: "srv_1",
		Limit:    50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("expected two matching tasks, got %#v", got)
	}
	ids := map[string]bool{got.Items[0].ID: true, got.Items[1].ID: true}
	if !ids[runningApp.ID] || !ids[failedPackage.ID] || ids[completedApp.ID] {
		t.Fatalf("unexpected filtered task ids: %#v", got.Items)
	}
}

func TestListReturnsEmptySliceWhenNoTasksMatch(t *testing.T) {
	svc := newTestService(t)
	got, err := svc.List(context.Background(), ListFilter{
		Status: "failed",
		Limit:  50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Items == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if got.Total != 0 || len(got.Items) != 0 {
		t.Fatalf("expected no tasks, got %#v", got)
	}
}

func TestListHidesInternalConnectivityTasksUnlessFiltered(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	hiddenConnectivity, err := svc.Create(ctx, CreateInput{Type: "server_connectivity_test", Summary: "hidden connectivity"})
	if err != nil {
		t.Fatal(err)
	}
	hiddenMetrics, err := svc.Create(ctx, CreateInput{Type: "metrics_collect", Summary: "hidden metrics"})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := svc.Create(ctx, CreateInput{Type: "server_info_collect", Summary: "visible discovery"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.List(ctx, ListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != visible.ID {
		t.Fatalf("expected only visible task, got %#v", got)
	}

	filtered, err := svc.List(ctx, ListFilter{Type: "server_connectivity_test", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != hiddenConnectivity.ID {
		t.Fatalf("explicit type filter should return internal task, got %#v", filtered)
	}

	all, err := svc.List(ctx, ListFilter{IncludeInternal: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || len(all.Items) != 3 {
		t.Fatalf("all type filter should include internal tasks, got %#v", all)
	}
	ids := map[string]bool{}
	for _, task := range all.Items {
		ids[task.ID] = true
	}
	if !ids[hiddenConnectivity.ID] || !ids[hiddenMetrics.ID] || !ids[visible.ID] {
		t.Fatalf("all type filter missed task ids: %#v", all.Items)
	}
}

func TestListCommonTasksExcludesSchedulerTriggeredTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if _, err := svc.Create(ctx, CreateInput{Type: "package_refresh", TriggerType: "scheduler", Summary: "scheduled refresh"}); err != nil {
		t.Fatal(err)
	}
	visible, err := svc.Create(ctx, CreateInput{Type: "package_refresh", TriggerType: "user", Summary: "manual refresh"})
	if err != nil {
		t.Fatal(err)
	}

	common, err := svc.List(ctx, ListFilter{ExcludeScheduled: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if common.Total != 1 || len(common.Items) != 1 || common.Items[0].ID != visible.ID {
		t.Fatalf("expected only user-triggered task, got %#v", common)
	}

	explicit, err := svc.List(ctx, ListFilter{Types: []string{"package_refresh"}, ExcludeScheduled: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Total != 2 {
		t.Fatalf("explicit type filter should include scheduled tasks, got %#v", explicit)
	}
}

func TestListOrdersNewestTasksFirst(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	oldTask, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "old"})
	if err != nil {
		t.Fatal(err)
	}
	newTask, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET created_at=? WHERE id=?`, time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339Nano), oldTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET created_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), newTask.ID); err != nil {
		t.Fatal(err)
	}

	got, err := svc.List(ctx, ListFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0].ID != newTask.ID || got.Items[1].ID != oldTask.ID {
		t.Fatalf("expected newest task first, got %#v", got.Items)
	}
}

func TestListPaginatesTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "task"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.List(ctx, ListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || got.Page != 2 || got.PageSize != 2 || len(got.Items) != 1 {
		t.Fatalf("unexpected paginated tasks: %#v", got)
	}
}

func TestFailRunningWithoutExecutionMarksOnlyUntrackedTasksFailed(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	tracked, err := svc.Create(ctx, CreateInput{Type: "server_ufw_install", Summary: "installing firewall", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := svc.Create(ctx, CreateInput{Type: "server_restart", Summary: "restarting"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET status=?,started_at=? WHERE id=?`, StatusRunning, now.Format(time.RFC3339Nano), untracked.ID); err != nil {
		t.Fatal(err)
	}

	failed, err := svc.FailRunningWithoutExecution(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("expected one untracked task to fail, got %d", failed)
	}
	gotTracked, err := svc.Get(ctx, tracked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTracked.Status != StatusRunning || !svc.HasRunningExecution(tracked.ID) {
		t.Fatalf("expected tracked task to remain running, got %#v", gotTracked)
	}
	gotUntracked, err := svc.Get(ctx, untracked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotUntracked.Status != StatusFailed || gotUntracked.FinishedAt == nil || !strings.Contains(gotUntracked.Error, "no active execution") {
		t.Fatalf("expected untracked task to fail, got %#v", gotUntracked)
	}
}

func TestRunningExecutionLifecycleFollowsTaskStatus(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if svc.HasRunningExecution(task.ID) {
		t.Fatal("queued task should not have a running execution")
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if !svc.HasRunningExecution(task.ID) {
		t.Fatal("started task should have a running execution")
	}
	if err := svc.Complete(ctx, task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if svc.HasRunningExecution(task.ID) {
		t.Fatal("completed task should not have a running execution")
	}
}

func TestFinishedExecutionIsFailedIfDatabaseStillSaysRunning(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}

	svc.FinishExecution(task.ID)
	failed, err := svc.FailRunningWithoutExecution(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("expected ended execution to be detected, got %d failed tasks", failed)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "no active execution") {
		t.Fatalf("expected task without live execution to fail, got %#v", got)
	}
}

func TestExpireStaleQueuedMarksOnlySelectedOldQueuedTasksFailed(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	oldWorker, err := svc.Create(ctx, CreateInput{Type: "server_ufw_install", Summary: "installing firewall"})
	if err != nil {
		t.Fatal(err)
	}
	oldScheduled, err := svc.Create(ctx, CreateInput{Type: "certificate_issue", Summary: "issue cert"})
	if err != nil {
		t.Fatal(err)
	}
	recentWorker, err := svc.Create(ctx, CreateInput{Type: "server_restart", Summary: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	for _, taskID := range []string{oldWorker.ID, oldScheduled.ID} {
		if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET created_at=? WHERE id=?`, old, taskID); err != nil {
			t.Fatal(err)
		}
	}

	expired, err := svc.ExpireStaleQueued(ctx, now, 10*time.Minute, []string{"server_ufw_install", "server_restart"})
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("expected one stale queued task to expire, got %d", expired)
	}
	gotOldWorker, err := svc.Get(ctx, oldWorker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOldWorker.Status != StatusFailed || gotOldWorker.FinishedAt == nil || !strings.Contains(gotOldWorker.Error, "worker startup timeout") {
		t.Fatalf("expected old worker task to fail, got %#v", gotOldWorker)
	}
	gotOldScheduled, err := svc.Get(ctx, oldScheduled.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotOldScheduled.Status != StatusQueued {
		t.Fatalf("scheduled task type should stay queued, got %#v", gotOldScheduled)
	}
	gotRecentWorker, err := svc.Get(ctx, recentWorker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRecentWorker.Status != StatusQueued {
		t.Fatalf("recent worker task should stay queued, got %#v", gotRecentWorker)
	}
}

func TestTaskOperationTriggerMetadataAndSteps(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{
		OperationID:         "op_1",
		Type:                "application_deploy",
		ServerID:            "srv_1",
		NodeID:              "srv_1",
		ResourceType:        "application",
		ResourceID:          "app_1",
		TriggerType:         "user",
		TriggerResourceType: "application",
		TriggerResourceID:   "app_1",
		TriggerTaskID:       "task_parent",
		TriggeredBy:         "alice",
		MetadataJSON:        `{"reason":"manual"}`,
		Summary:             "deploy api",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "op_1" || got.NodeID != "srv_1" || got.TriggerType != "user" || got.TriggeredBy != "alice" || got.MetadataJSON != `{"reason":"manual"}` {
		t.Fatalf("task metadata was not persisted: %#v", got)
	}
	step, err := svc.UpsertStep(ctx, task.ID, StepInput{Step: "schedule", Status: StatusRunning, Percentage: 25, MetadataJSON: `{"node":"srv_1"}`})
	if err != nil {
		t.Fatal(err)
	}
	if step.ID == "" || step.StartedAt == nil {
		t.Fatalf("expected started step: %#v", step)
	}
	step, err = svc.UpsertStep(ctx, task.ID, StepInput{Step: "schedule", Status: StatusCompleted, Percentage: 100})
	if err != nil {
		t.Fatal(err)
	}
	if step.FinishedAt == nil {
		t.Fatalf("expected finished step: %#v", step)
	}
	steps, err := svc.Steps(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Step != "schedule" || steps[0].Status != StatusCompleted {
		t.Fatalf("unexpected steps: %#v", steps)
	}
	retry, err := svc.Retry(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ID == task.ID || retry.OperationID != task.OperationID || retry.TriggerType != "retry" || retry.TriggerTaskID != task.ID {
		t.Fatalf("unexpected retry task: %#v", retry)
	}
}
