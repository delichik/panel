package runtimeevents

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/platform/config"
	"panel/internal/platform/database"
)

func TestWriteCreatesSimpleEventAndDedupe(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()

	in := WriteEventInput{
		EventType:   EventTaskCreated,
		Category:    CategoryTask,
		Severity:    SeverityInfo,
		Source:      "task",
		SourceModule: "tasks",
		DedupeKey:   "dedupe-1",
		Summary:     "Task created",
		OccurredAt:  now,
	}

	event, inserted, err := svc.Write(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted || event.EventType != EventTaskCreated || event.Summary != "Task created" {
		t.Fatalf("first write inserted/event = %v/%#v", inserted, event)
	}
	_, inserted, err = svc.Write(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate dedupe key must not insert a second event")
	}

	result, err := svc.ListSystemEvents(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("event total/items = %d/%d", result.Total, len(result.Items))
	}
	item := result.Items[0]
	if item.EventType != EventTaskCreated || item.Category != CategoryTask || item.Severity != SeverityInfo {
		t.Fatalf("unexpected event: %#v", item)
	}
}

func TestListSystemEventsFilters(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_, _, err := svc.Write(ctx, WriteEventInput{
		EventType: EventApplicationOperationCompleted, Category: CategoryApplication,
		Severity: SeverityInfo, Source: "user", SourceModule: "applications",
		Summary: "Application operation completed", OccurredAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Write(ctx, WriteEventInput{
		EventType: EventTaskFailed, Category: CategoryTask,
		Severity: SeverityError, Source: "task", SourceModule: "tasks",
		Summary: "Task failed", OccurredAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ListSystemEvents(ctx, ListFilter{Category: CategoryTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].EventType != EventTaskFailed {
		t.Fatalf("category filter mismatch: %#v", result.Items)
	}
	result, err = svc.ListSystemEvents(ctx, ListFilter{Severity: SeverityError})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].Severity != SeverityError {
		t.Fatalf("severity filter mismatch: %#v", result.Items)
	}
	from := now.Add(time.Second)
	result, err = svc.ListSystemEvents(ctx, ListFilter{From: &from})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].EventType != EventTaskFailed {
		t.Fatalf("time filter mismatch: %#v", result.Items)
	}
}

func TestBufferedWriterFlushesBatch(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	w := NewBufferedWriter(svc, time.Hour)
	for i := 0; i < 10; i++ {
		w.Log(ctx, WriteEventInput{
			EventType: EventTaskStarted, Category: CategoryTask,
			Severity: SeverityInfo, Source: "task", SourceModule: "tasks",
			Summary: "Task started", OccurredAt: time.Now().UTC(),
		})
	}
	w.flush()

	result, err := svc.ListSystemEvents(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 10 {
		t.Fatalf("flushed events = %d, want 10", result.Total)
	}
}

func TestCleanupDeletesOldEvents(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -10)

	if _, _, err := svc.Write(ctx, WriteEventInput{
		EventType: EventTaskCompleted, Category: CategoryTask, Severity: SeverityInfo,
		Source: "task", SourceModule: "tasks", Summary: "Old event", OccurredAt: old,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Write(ctx, WriteEventInput{
		EventType: EventTaskCompleted, Category: CategoryTask, Severity: SeverityInfo,
		Source: "task", SourceModule: "tasks", Summary: "Recent event", OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Cleanup(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("cleanup deleted = %d, want 1", result.EventsDeleted)
	}
	list, err := svc.ListSystemEvents(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || list.Items[0].Summary != "Recent event" {
		t.Fatalf("cleanup kept wrong events: %#v", list.Items)
	}
}

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := database.Open(config.Config{
		DataRoot:            dir,
		AppDatabase:         filepath.Join(dir, "app.db"),
		LogDatabase:         filepath.Join(dir, "log.db"),
		CoordinationDatabase: filepath.Join(dir, "coord.db"),
		MetricsDatabase:     filepath.Join(dir, "metrics.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store.LogDB()), func() { _ = store.Close() }
}