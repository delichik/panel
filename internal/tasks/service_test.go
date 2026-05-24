package tasks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
		Summary:             "deploy api",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OperationID != "op_1" || got.NodeID != "srv_1" || got.TriggerType != "user" || got.TriggeredBy != "alice" {
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
