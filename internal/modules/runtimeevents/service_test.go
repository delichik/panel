package runtimeevents

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"panel/internal/platform/config"
	"panel/internal/platform/database"
)

func TestSystemEventsFilterAndDetailWrapper(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_, _, err := svc.Write(ctx, WriteEventInput{
		EventType:   EventTaskCreated,
		Category:    CategoryTask,
		SubjectType: "task",
		SubjectID:   "task-1",
		Severity:    SeverityInfo,
		Source:      "task",
		Summary:     "Task created",
		OccurredAt:  now,
		Detail: &EventDetailInput{
			PayloadJSON:    `{"taskId":"task-1"}`,
			Error:          "task failed",
			TaskRefsJSON:   `["task-1"]`,
			LogRefsJSON:    `["log-1"]`,
			TargetRefsJSON: `["target-1"]`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Write(ctx, WriteEventInput{
		EventType:   EventLogAttached,
		Category:    CategoryLog,
		SubjectType: "task",
		SubjectID:   "task-1",
		Severity:    SeverityInfo,
		Source:      "task",
		Summary:     "Task log attached",
		OccurredAt:  now.Add(time.Second),
		Detail:      &EventDetailInput{PayloadJSON: `{bad`, TaskRefsJSON: `{bad`},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.SetSubjectNameResolver(func(_ context.Context, subjectType, subjectID string) string {
		return "resolved-" + subjectType + "-" + subjectID
	})

	result, err := svc.ListSystemEvents(ctx, ListFilter{Category: CategoryTask, SubjectID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Items) != 1 || result.Items[0].Category != CategoryTask {
		t.Fatalf("unexpected filtered events: %#v", result)
	}

	detail, err := svc.GetSystemEventDetail(ctx, result.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Event.ID != result.Items[0].ID {
		t.Fatalf("event wrapper mismatch: %#v", detail)
	}
	payload, ok := detail.Payload.(map[string]any)
	if !ok || payload["taskId"] != "task-1" {
		t.Fatalf("unexpected payload: %#v", detail.Payload)
	}
	if !reflect.DeepEqual(detail.TaskRefs, []any{"task-1"}) || !reflect.DeepEqual(detail.LogRefs, []any{"log-1"}) || !reflect.DeepEqual(detail.TargetRefs, []any{"target-1"}) {
		t.Fatalf("unexpected refs: %#v", detail)
	}

	logs, err := svc.ListSystemEvents(ctx, ListFilter{Category: CategoryLog, SubjectID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || len(logs.Items) != 1 {
		t.Fatalf("unexpected log events: %#v", logs)
	}
	badDetail, err := svc.GetSystemEventDetail(ctx, logs.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if badDetail.Payload != `{bad` || len(badDetail.TaskRefs) != 0 {
		t.Fatalf("invalid detail should degrade safely: %#v", badDetail)
	}
}

func TestSystemEventsWithoutResolverKeepSubjectNameEmpty(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC()

	_, _, err := svc.Write(ctx, WriteEventInput{
		EventType:   EventTaskCreated,
		Category:    CategoryTask,
		SubjectType: "task",
		SubjectID:   "task-1",
		Severity:    SeverityInfo,
		Source:      "task",
		Summary:     "Task created",
		OccurredAt:  now,
		Detail:      &EventDetailInput{PayloadJSON: `{}`},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ListSystemEvents(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].SubjectName != "" {
		t.Fatalf("subject name must stay empty without resolver: %#v", result.Items)
	}
}

func TestCleanupPrunesDetailsBeforeDeletingRecords(t *testing.T) {
	svc, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -10)

	event, _, err := svc.Write(ctx, WriteEventInput{
		EventType:   EventTaskCreated,
		Category:    CategoryTask,
		SubjectType: "task",
		SubjectID:   "task-1",
		Severity:    SeverityInfo,
		Source:      "task",
		Summary:     "Old event",
		OccurredAt:  old,
		Detail:      &EventDetailInput{PayloadJSON: `{"old":true}`},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Cleanup(ctx, 30, 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.DetailsPruned != 1 || result.EventsDeleted != 0 {
		t.Fatalf("cleanup result = %#v", result)
	}
	detail, err := svc.GetEventDetail(ctx, event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DetailAvailable || detail.PayloadJSON != "{}" {
		t.Fatalf("detail should be pruned: %#v", detail)
	}

	result, err = svc.Cleanup(ctx, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("record cleanup result = %#v", result)
	}
	events, err := svc.ListSystemEvents(ctx, ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 0 {
		t.Fatalf("events after retention cleanup = %#v", events)
	}
}

func newTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := database.Open(config.Config{
		DataRoot:             dir,
		AppDatabase:          filepath.Join(dir, "app.db"),
		LogDatabase:          filepath.Join(dir, "log.db"),
		CoordinationDatabase: filepath.Join(dir, "coordination.db"),
		MetricsDatabase:      filepath.Join(dir, "metrics.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store.LogDB()), func() { _ = store.Close() }
}
