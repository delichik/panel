package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/runtimeevents"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := NewService(store.LogDB())
	for _, def := range []Definition{
		{Type: "test", ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "sample_task", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "sample_restart", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "package_refresh", AllowRunNow: true, AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "package_upgrade_selected", DisallowCancel: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "package_upgrade_all", DisallowCancel: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "metrics_collect", Hidden: true, AllowRunNow: true, AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "server_connectivity_test", Hidden: true, AllowRunNow: true, AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "server_info_collect", AllowRunNow: true, AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "server_ufw_install", AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "server_restart", AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
		{Type: "certificate_issue", AllowRunNow: true, AllowRetry: true, ConcurrencyPolicy: ConcurrencyResourceExclusive},
	} {
		svc.MustRegister(def)
	}
	return svc
}

func TestCancelRejectsNonCancellableTask(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "package_upgrade_selected", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.Cancel(ctx, task.ID, "user requested")
	var pe *panelerr.Error
	if !errors.As(err, &pe) || pe.Code != "task_cancel_unsupported" {
		t.Fatalf("expected non-cancellable task to be rejected, got %v", err)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("expected task to stay running, got %s", got.Status)
	}
}

func TestCancelByServerSkipsNonCancellableTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	blocked, err := svc.Create(ctx, CreateInput{Type: "package_upgrade_all", ServerID: "srv_1", ResourceType: "server", ResourceID: "srv_1", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if !svc.isCancellationBlocked("package_upgrade_all") || !svc.isCancellationBlocked(blocked.Type) {
		t.Fatalf("package_upgrade_all should be registered as non-cancellable, got %q", blocked.Type)
	}
	blockedFromDB, err := svc.Get(ctx, blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.isCancellationBlocked(blockedFromDB.Type) {
		t.Fatalf("loaded task type %q should be non-cancellable", blockedFromDB.Type)
	}
	cancellable, err := svc.Create(ctx, CreateInput{Type: "package_refresh", ServerID: "srv_1", ResourceType: "server", ResourceID: "srv_1", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	count, err := svc.CancelByServer(ctx, "srv_1", "server removed")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected only the cancellable task to be cancelled, got %d", count)
	}
	gotBlocked, err := svc.Get(ctx, blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotBlocked.Status != StatusRunning {
		t.Fatalf("expected non-cancellable task to stay running, got %s", gotBlocked.Status)
	}
	gotCancellable, err := svc.Get(ctx, cancellable.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotCancellable.Status != StatusCancelled {
		t.Fatalf("expected cancellable task to be cancelled, got %s", gotCancellable.Status)
	}
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
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Status: StatusCompleted, Summary: "registered"})
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
	runningApp, err := svc.Create(ctx, CreateInput{Type: "sample_task", ServerID: "srv_1", Summary: "deploy"})
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
	completedApp, err := svc.Create(ctx, CreateInput{Type: "sample_restart", ServerID: "srv_1", Summary: "restart", Status: StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.List(ctx, ListFilter{
		Statuses: []string{StatusRunning, StatusFailed},
		Types:    []string{"sample_task", "package_refresh"},
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

func TestListOperationPagePaginatesOperationsAndReturnsTheirTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	oldOp, err := svc.Create(ctx, CreateInput{OperationID: "op-old", Type: "test", Summary: "old"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.Create(ctx, CreateInput{OperationID: "op-batch", Type: "test", Summary: "batch parent", ChildCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	childIDs := []string{}
	for i := 1; i <= 3; i++ {
		child, err := svc.Create(ctx, CreateInput{OperationID: "op-batch", Type: "test", ParentTaskID: parent.ID, ChildIndex: i, ChildCount: 3, Summary: "batch child"})
		if err != nil {
			t.Fatal(err)
		}
		childIDs = append(childIDs, child.ID)
	}
	newOp, err := svc.Create(ctx, CreateInput{OperationID: "op-new", Type: "test", Summary: "new"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.List(ctx, ListFilter{Limit: 2, OperationPage: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || got.Page != 1 || got.PageSize != 2 {
		t.Fatalf("unexpected operation page metadata: %#v", got)
	}
	ids := map[string]bool{}
	for _, item := range got.Items {
		ids[item.ID] = true
		if item.OperationID == oldOp.OperationID {
			t.Fatalf("old operation should not be on the first operation page: %#v", got.Items)
		}
	}
	if !ids[newOp.ID] || !ids[parent.ID] {
		t.Fatalf("expected newest operation and batch parent, got %#v", got.Items)
	}
	for _, id := range childIDs {
		if !ids[id] {
			t.Fatalf("expected batch child %s on the operation page, got %#v", id, got.Items)
		}
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

func TestStartIsIdempotentForActiveExecution(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatalf("expected duplicate start to be idempotent, got %v", err)
	}
}

func TestRetryPreservesTaskParams(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	original, err := svc.Create(ctx, CreateInput{
		OperationID:   "op-1",
		Type:          "server_info_collect",
		ExecutionMode: ExecutionModeSerial,
		ScheduleKey:   "server-info:srv-1",
		ServerID:      "srv-1",
		ResourceType:  "server",
		ResourceID:    "srv-1",
		TriggeredBy:   "scheduler",
		ParamsJSON:    `{"bootstrap":false}`,
		MetadataJSON:  `{"source":"test"}`,
		Summary:       "Collecting server information",
		MaxRetries:    3,
	})
	if err != nil {
		t.Fatal(err)
	}

	retry, err := svc.Retry(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ParamsJSON != original.ParamsJSON {
		t.Fatalf("retry should preserve params json, got %q want %q", retry.ParamsJSON, original.ParamsJSON)
	}
	if retry.MetadataJSON != original.MetadataJSON || retry.ScheduleKey != original.ScheduleKey || retry.ExecutionMode != original.ExecutionMode {
		t.Fatalf("retry should preserve execution context, got %#v from %#v", retry, original)
	}
	if retry.TriggerType != "retry" || retry.TriggerTaskID != original.ID {
		t.Fatalf("retry trigger should point at original task, got %#v", retry)
	}
	if retry.ParentTaskID != "" || retry.ChildCount != 0 || retry.ChildIndex != 0 {
		t.Fatalf("retry should not reattach to old batch parent, got %#v", retry)
	}
}

func TestFinishExecutionKeepsRunningDatabaseTaskTracked(t *testing.T) {
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
	if !svc.HasRunningExecution(task.ID) {
		t.Fatal("running database task should keep its live execution")
	}
	failed, err := svc.FailRunningWithoutExecution(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if failed != 0 {
		t.Fatalf("expected tracked running task to remain active, got %d failed tasks", failed)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("expected task to remain running, got %#v", got)
	}
}

func TestFinishExecutionClearsNonRunningDatabaseTask(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET status=? WHERE id=?`, StatusFailed, task.ID); err != nil {
		t.Fatal(err)
	}

	svc.FinishExecution(task.ID)

	if svc.HasRunningExecution(task.ID) {
		t.Fatal("non-running database task should clear its live execution")
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
		Type:                "sample_task",
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

func TestCleanupRetainedDeletesOldTerminalHistory(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	old, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "old completed", Status: StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AppendLog(ctx, old.ID, "stdout", "old log line"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET finished_at=? WHERE id=?`, time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano), old.ID); err != nil {
		t.Fatal(err)
	}

	recent, err := svc.Create(ctx, CreateInput{Type: "test", Summary: "recent completed", Status: StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET finished_at=? WHERE id=?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), recent.ID); err != nil {
		t.Fatal(err)
	}

	deleted, err := svc.CleanupRetained(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected exactly one old task to be deleted, got %d", deleted)
	}
	if _, err := svc.Get(ctx, old.ID); err == nil {
		t.Fatal("expected old task to be removed")
	}
	var logCount int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_logs WHERE task_id=?`, old.ID).Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if logCount != 0 {
		t.Fatalf("expected task logs for the old task to be removed, got %d", logCount)
	}
	if _, err := svc.Get(ctx, recent.ID); err != nil {
		t.Fatalf("expected recent task to remain, got %v", err)
	}
}

func TestExpireStaleQueuedFailsOldScheduledTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	svc.MustRegister(Definition{Type: "scheduled_stale", StaleQueuedAfter: time.Minute})
	// scheduled 任务只有 next_run_at 已到期（<= now）才会被过期清理误杀修复后的行为。
	past := now.Add(-time.Minute)
	task, err := svc.Create(ctx, CreateInput{Type: "scheduled_stale", Status: StatusScheduled, NextRunAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `UPDATE tasks SET created_at=? WHERE id=?`, old, task.ID); err != nil {
		t.Fatal(err)
	}

	expired, err := svc.ExpireStaleQueued(ctx, now, 10*time.Minute, []string{"scheduled_stale"})
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("expected one stale scheduled task to expire, got %d", expired)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.FinishedAt == nil || !strings.Contains(got.Error, "worker startup timeout") {
		t.Fatalf("expected stale scheduled task to fail, got %#v", got)
	}
}

func TestListFiltersByQuery(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	web, err := svc.Create(ctx, CreateInput{Type: "sample_task", Summary: "deploy web frontend"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := svc.Create(ctx, CreateInput{Type: "package_refresh", Summary: "refresh certificates"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := svc.Create(ctx, CreateInput{Type: "sample_restart", Summary: "restart node"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Fail(ctx, failed.ID, errors.New("timeout while contacting agent")); err != nil {
		t.Fatal(err)
	}
	underscore, err := svc.Create(ctx, CreateInput{Type: "sample_task", Summary: "foo_bar"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, CreateInput{Type: "sample_task", Summary: "fooXbar"})
	if err != nil {
		t.Fatal(err)
	}
	assertQueryIDs := func(q string, want ...string) {
		t.Helper()
		got, err := svc.List(ctx, ListFilter{Q: q, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		gotIDs := map[string]bool{}
		for _, item := range got.Items {
			gotIDs[item.ID] = true
		}
		if len(got.Items) != len(want) {
			t.Fatalf("q=%q: got %d items %#v, want %d", q, len(got.Items), got.Items, len(want))
		}
		for _, id := range want {
			if !gotIDs[id] {
				t.Fatalf("q=%q: missing %s in %#v", q, id, got.Items)
			}
		}
	}
	assertQueryIDs("web", web.ID)
	assertQueryIDs("certificates", cert.ID)
	assertQueryIDs("timeout", failed.ID)
	assertQueryIDs("package_refresh", cert.ID)
	assertQueryIDs(web.ID, web.ID)
	// LIKE 通配符必须转义：foo_bar 只匹配字面下划线，不能匹配 fooXbar。
	assertQueryIDs("foo_bar", underscore.ID)
	_ = other
}

func TestHandlerListAcceptsQuery(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task", Summary: "deploy web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), CreateInput{Type: "sample_restart", Summary: "restart"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks?q=deploy&includeInternal=true", nil)
	rec := httptest.NewRecorder()
	NewHandler(svc).List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list to succeed, got %d", rec.Code)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatal(err)
	}
	var result ListResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != task.ID {
		t.Fatalf("expected one matching task, got %#v", result)
	}
}

func TestAppendLogTrimsOldestBeyondCap(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < maxTaskLogLinesPerTask+5; i++ {
		if err := svc.AppendLog(ctx, task.ID, "stdout", "line "+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM task_logs WHERE task_id=?`, task.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != maxTaskLogLinesPerTask {
		t.Fatalf("expected %d logs after trim, got %d", maxTaskLogLinesPerTask, count)
	}
	logs, _, err := svc.Logs(ctx, task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || logs[0].Line != "line 5" {
		t.Fatalf("expected oldest logs to roll off, first=%#v", logs)
	}
}

func TestAppendLogTruncatesOverlongLine(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task"})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("长", maxTaskLogLineLength+100)
	if err := svc.AppendLog(context.Background(), task.ID, "stdout", long); err != nil {
		t.Fatal(err)
	}
	logs, _, err := svc.Logs(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || len([]rune(logs[0].Line)) != maxTaskLogLineLength {
		t.Fatalf("expected truncated log of %d runes, got %d", maxTaskLogLineLength, len([]rune(logs[0].Line)))
	}
}

func TestFailDoesNotAppendLogToTerminalTask(t *testing.T) {
	svc := newTestService(t)
	task, err := svc.Create(context.Background(), CreateInput{Type: "sample_task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(context.Background(), task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Fail(context.Background(), task.ID, errors.New("late failure")); err != nil {
		t.Fatal(err)
	}
	logs, _, err := svc.Logs(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Fatalf("terminal task should not receive failure log, got %#v", logs)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("terminal status must not change, got %#v", got)
	}
}

func TestExpireStaleQueuedKeepsFutureScheduledTask(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{Type: "stale_scheduled", StaleQueuedAfter: time.Minute})
	ctx := context.Background()
	future := time.Now().UTC().Add(time.Hour)
	task, err := svc.Create(ctx, CreateInput{Type: "stale_scheduled", Status: StatusScheduled, NextRunAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`UPDATE tasks SET created_at=? WHERE id=?`, old, task.ID); err != nil {
		t.Fatal(err)
	}
	expired, err := svc.ExpireStaleQueued(ctx, time.Now().UTC(), time.Minute, []string{"stale_scheduled"})
	if err != nil {
		t.Fatal(err)
	}
	if expired != 0 {
		t.Fatalf("future scheduled task must not expire, got %d", expired)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusScheduled {
		t.Fatalf("expected scheduled, got %#v", got)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`UPDATE tasks SET next_run_at=? WHERE id=?`, past, task.ID); err != nil {
		t.Fatal(err)
	}
	expired, err = svc.ExpireStaleQueued(ctx, time.Now().UTC(), time.Minute, []string{"stale_scheduled"})
	if err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Fatalf("due scheduled task should expire, got %d", expired)
	}
}

func TestCancelByServerWritesCancelEvents(t *testing.T) {
	svc := newTestService(t)
	events := runtimeevents.NewService(svc.db)
	svc.SetRuntimeEvents(events)
	ctx := context.Background()
	task, err := svc.Create(ctx, CreateInput{Type: "package_refresh", ServerID: "srv_1", ResourceType: "server", ResourceID: "srv_1", Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	count, err := svc.CancelByServer(ctx, "srv_1", "server removed")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 cancelled, got %d", count)
	}
	result, err := events.ListSystemEvents(ctx, runtimeevents.ListFilter{EventType: runtimeevents.EventTaskCancelled, Category: runtimeevents.CategoryTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("expected one cancel event, got %#v", result.Items)
	}
	if result.Items[0].Source != task.TriggerType && result.Items[0].Source != "task" {
		t.Fatalf("unexpected event source: %#v", result.Items[0])
	}
	// 再次批量取消（已终态）不应追加新事件。
	count, err = svc.CancelByServer(ctx, "srv_1", "server removed")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 cancelled on second pass, got %d", count)
	}
	result, err = events.ListSystemEvents(ctx, runtimeevents.ListFilter{EventType: runtimeevents.EventTaskCancelled, Category: runtimeevents.CategoryTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 {
		t.Fatalf("expected still one cancel event, got %#v", result.Items)
	}
}