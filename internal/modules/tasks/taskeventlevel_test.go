package tasks

import (
	"context"
	"errors"
	"testing"

	"panel/internal/platform/notifications"
)

// eventLevel reads the unified events index level for one process event id.
func eventLevel(t *testing.T, svc *Service, eventID string) string {
	t.Helper()
	var level string
	if err := svc.db.QueryRow(`SELECT level FROM events WHERE id=?`, eventID).Scan(&level); err != nil {
		t.Fatalf("read events level for %s: %v", eventID, err)
	}
	return level
}

func resetTaskNotifications(t *testing.T) {
	t.Helper()
	notifications.ResetForTest()
	t.Cleanup(notifications.ResetForTest)
}

// seedNotificationBaseline publishes a warm-up visible notification so tests can
// use Wait(after>0) to read later items (after=0 is always a baseline).
func seedNotificationBaseline(t *testing.T) uint64 {
	t.Helper()
	notifications.PublishProcess(notifications.ProcessEvent{EventID: "evt-warmup", Module: "test", State: "running", Level: "info"})
	_, cursor := notifications.Wait(context.Background(), 0, 0)
	if cursor != 1 {
		t.Fatalf("warm-up baseline cursor = %d, want 1", cursor)
	}
	return cursor
}

// TestQuietTaskLifecycleEventsAreDebugAndNeverNotify pins TASK-REG-003 after the
// unified-logging migration: routine (Quiet) task creation/success facts are
// recorded as debug and therefore never reach the user-visible notification
// channel, while failures stay error and still notify.
func TestQuietTaskLifecycleEventsAreDebugAndNeverNotify(t *testing.T) {
	svc := newTestService(t)
	resetTaskNotifications(t)
	baseline := seedNotificationBaseline(t)
	svc.Registry().Replace(Definition{Type: "agent_check", Quiet: true, ConcurrencyPolicy: ConcurrencyParallelAllowed})
	ctx := context.Background()

	done, err := svc.Create(ctx, CreateInput{Type: "agent_check", Summary: "Checking agent for srv_1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, done.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Advance(ctx, done.ID, "checked", "agent is compatible"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, done.ID, "Checking agent for srv_1"); err != nil {
		t.Fatal(err)
	}
	if got := eventLevel(t, svc, eventRunLogID(done.ID)); got != "debug" {
		t.Fatalf("quiet task success level = %q, want debug", got)
	}
	if _, cursor := notifications.Wait(ctx, 0, 0); cursor != baseline {
		t.Fatalf("quiet task lifecycle advanced notification cursor to %d, want %d", cursor, baseline)
	}

	failed, err := svc.Create(ctx, CreateInput{Type: "agent_check", Summary: "Checking agent for srv_2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, failed.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Fail(ctx, failed.ID, errors.New("agent unreachable")); err != nil {
		t.Fatal(err)
	}
	if got := eventLevel(t, svc, eventRunLogID(failed.ID)); got != "error" {
		t.Fatalf("quiet task failure level = %q, want error", got)
	}
	items, cursor := notifications.Wait(ctx, baseline, 0)
	if len(items) != 1 || cursor != baseline+1 {
		t.Fatalf("quiet task failure must notify once: items = %#v, cursor = %d", items, cursor)
	}
	if items[0].EventID != eventRunLogID(failed.ID) || items[0].Level != "error" {
		t.Fatalf("quiet task failure notification = %#v", items[0])
	}
}

// TestNonQuietTaskLifecycleEventsStayInfoAndNotify guards the inverse boundary:
// user-visible task types keep info-level facts and publish start/terminal
// notifications (TASK-REG-003 must not leak to non-quiet types).
func TestNonQuietTaskLifecycleEventsStayInfoAndNotify(t *testing.T) {
	svc := newTestService(t)
	resetTaskNotifications(t)
	baseline := seedNotificationBaseline(t)
	ctx := context.Background()

	task, err := svc.Create(ctx, CreateInput{Type: "sample_task", Summary: "user visible work"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if got := eventLevel(t, svc, eventRunLogID(task.ID)); got != "info" {
		t.Fatalf("non-quiet task level = %q, want info", got)
	}
	if _, cursor := notifications.Wait(ctx, 0, 0); cursor != baseline+2 {
		t.Fatalf("non-quiet task notifications cursor = %d, want %d (start+terminal)", cursor, baseline+2)
	}
}
