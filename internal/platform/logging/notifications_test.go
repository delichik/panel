package logging

import (
	"context"
	"testing"

	"panel/internal/platform/notifications"
)

// resetNotifications clears the in-memory channel shared by tests in this
// package; event publishing happens on every RecordContext call.
func resetNotifications(t *testing.T) {
	t.Helper()
	notifications.ResetForTest()
	t.Cleanup(notifications.ResetForTest)
}

// TestRecordContextPublishesStartAndTerminalOnce covers ENG-EVT-011: the
// process notification channel sees one start and one terminal transition per
// event, while events rows keep their existing upsert-per-call semantics.
func TestRecordContextPublishesStartAndTerminalOnce(t *testing.T) {
	resetNotifications(t)
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	ctx := With(context.Background(), Context{EventID: "evt-notify", Module: "deploy"})
	if err := RecordContext(ctx, ContextMeta{
		Origin:  "user",
		Title:   "Deploy app → srv",
		Summary: "started",
		State:   "running",
		Level:   "info",
	}); err != nil {
		t.Fatal(err)
	}
	if items, cursor := notifications.Wait(context.Background(), 0, 0); len(items) != 0 || cursor != 1 {
		t.Fatalf("after start: items = %#v, cursor = %d; want baseline cursor 1", items, cursor)
	}

	// Repeated running updates must not publish again.
	if err := RecordContext(ctx, ContextMeta{
		Origin:  "user",
		Title:   "Deploy app → srv",
		Summary: "step 2",
		State:   "running",
	}); err != nil {
		t.Fatal(err)
	}
	if _, cursor := notifications.Wait(context.Background(), 0, 0); cursor != 1 {
		t.Fatalf("repeated running published again: cursor = %d, want 1", cursor)
	}

	// The terminal transition publishes exactly once with the final fields.
	if err := RecordContext(ctx, ContextMeta{
		Origin:   "user",
		Title:    "Deploy app → srv",
		Summary:  "finished",
		State:    "succeeded",
		Level:    "info",
		Finished: true,
	}); err != nil {
		t.Fatal(err)
	}
	items, cursor := notifications.Wait(context.Background(), 1, 0)
	if len(items) != 1 || items[0].Seq != 2 || cursor != 2 {
		t.Fatalf("after terminal: items = %#v, cursor = %d; want one item seq 2", items, cursor)
	}
	terminal := items[0]
	if terminal.EventID != "evt-notify" || terminal.Module != "deploy" || terminal.Title != "Deploy app → srv" {
		t.Fatalf("terminal notification = %#v", terminal)
	}
	if terminal.State != "succeeded" || terminal.Summary != "finished" || terminal.Origin != "user" || terminal.HadError {
		t.Fatalf("terminal fields = %#v", terminal)
	}
	if terminal.UpdatedAt == "" {
		t.Fatalf("terminal notification missing updatedAt: %#v", terminal)
	}

	// A trailing terminal update is suppressed by the dedup table but still
	// upserts its events row.
	if err := RecordContext(ctx, ContextMeta{
		Origin:   "user",
		Title:    "Deploy app → srv",
		Summary:  "finished again",
		State:    "succeeded",
		Level:    "info",
		Finished: true,
	}); err != nil {
		t.Fatal(err)
	}
	if items, cursor := notifications.Wait(context.Background(), 2, 0); len(items) != 0 || cursor != 2 {
		t.Fatalf("post-terminal: items = %#v, cursor = %d; want no new notification", items, cursor)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsert) != 4 {
		t.Fatalf("events rows = %d, want 4 (existing upsert semantics)", len(store.upsert))
	}
}

// TestRecordContextPublishesWithoutStore verifies the notification channel does
// not depend on the events store being configured.
func TestRecordContextPublishesWithoutStore(t *testing.T) {
	resetNotifications(t)
	SetContextStore(nil)
	t.Cleanup(func() { SetContextStore(nil) })

	ctx := With(context.Background(), Context{EventID: "evt-no-store", Module: "deploy"})
	if err := RecordContext(ctx, ContextMeta{
		Origin:  "process",
		Title:   "Backup",
		Summary: "started",
		State:   "running",
	}); err != nil {
		t.Fatal(err)
	}
	if _, cursor := notifications.Wait(context.Background(), 0, 0); cursor != 1 {
		t.Fatalf("start notification missing without store: cursor = %d, want 1", cursor)
	}

	if err := RecordContext(ctx, ContextMeta{
		Origin:   "process",
		Title:    "Backup",
		State:    "failed",
		HadError: true,
		Finished: true,
	}); err != nil {
		t.Fatal(err)
	}
	items, cursor := notifications.Wait(context.Background(), 1, 0)
	if len(items) != 1 || cursor != 2 {
		t.Fatalf("terminal without store: items = %#v, cursor = %d", items, cursor)
	}
	if items[0].State != "failed" || !items[0].HadError {
		t.Fatalf("terminal without store = %#v", items[0])
	}
}

// TestNotificationsSkipHTTPModuleAndPlainLogs ensures only explicit process
// events are published: HTTP request streams and ordinary log lines never are.
func TestNotificationsSkipHTTPModuleAndPlainLogs(t *testing.T) {
	resetNotifications(t)
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	httpCtx := With(context.Background(), Context{EventID: "evt-http", Module: "http"})
	if err := RecordContext(httpCtx, ContextMeta{Title: "request", State: "running", Origin: "process"}); err != nil {
		t.Fatal(err)
	}
	if _, cursor := notifications.Wait(context.Background(), 0, 0); cursor != 0 {
		t.Fatalf("http module published a notification: cursor = %d, want 0", cursor)
	}

	logCtx := With(context.Background(), Context{EventID: "evt-log-only", Module: "deploy"})
	From(logCtx).Info("plain diagnostic line")
	if _, cursor := notifications.Wait(context.Background(), 0, 0); cursor != 0 {
		t.Fatalf("plain log line published a notification: cursor = %d, want 0", cursor)
	}
}
