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
	if len(got) != 1 || got[0].ID != matching.ID {
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
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected no tasks, got %#v", got)
	}
}
