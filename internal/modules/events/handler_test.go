package events_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	modevents "panel/internal/modules/events"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/events"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/logging"
	"panel/internal/platform/notifications"
)

func openTestHandler(t *testing.T) (*modevents.Handler, *events.Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	logDir := filepath.Join(cfg.DataRoot, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	svc := modevents.NewService(store.AppDB(), logDir)
	return modevents.NewHandler(svc), events.NewStore(store.AppDB()), logDir
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected error: %#v", envelope.Error)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T", envelope.Data)
	}
	return data
}

func TestHandlerListGetAndLines(t *testing.T) {
	h, store, logDir := openTestHandler(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, logging.ContextRecord{
		EventID:     "event-a",
		Module:      "panel",
		Origin:      "process",
		OperationID: "op-1",
		Title:       "boot",
		Summary:     "started",
		LogFile:     "panel-20260101.jsonl",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	path := filepath.Join(logDir, "panel-20260101.jsonl")
	lines := []map[string]any{
		{"ts": "2026-01-01T00:00:00Z", "level": "info", "module": "panel", "eventId": "event-a", "msg": "one"},
		{"ts": "2026-01-01T00:00:01Z", "level": "error", "module": "panel", "eventId": "event-a", "msg": "two"},
		{"ts": "2026-01-01T00:00:02Z", "level": "info", "module": "panel", "eventId": "other", "msg": "skip"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()

	t.Run("list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events?origin=process&limit=10", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		data := decodeData(t, rec)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items=%v", items)
		}
		row, _ := items[0].(map[string]any)
		if row["id"] != "event-a" || row["module"] != "panel" {
			t.Fatalf("row=%#v", row)
		}
	})

	t.Run("get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events/event-a", nil)
		req.SetPathValue("eventId", "event-a")
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		data := decodeData(t, rec)
		if data["id"] != "event-a" || data["operationId"] != "op-1" {
			t.Fatalf("data=%#v", data)
		}
	})

	t.Run("get missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events/missing", nil)
		req.SetPathValue("eventId", "missing")
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d", rec.Code)
		}
	})

	t.Run("event logs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events/event-a/logs?level=error&limit=10", nil)
		req.SetPathValue("eventId", "event-a")
		rec := httptest.NewRecorder()
		h.EventLogs(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		data := decodeData(t, rec)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items=%v", items)
		}
		line, _ := items[0].(map[string]any)
		if line["msg"] != "two" {
			t.Fatalf("line=%#v", line)
		}
	})

	t.Run("list rejects bad query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events?unknown=1", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}

func TestHandlerRoutesRegistered(t *testing.T) {
	h, _, _ := openTestHandler(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	notifyReq := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after=0&wait=0", nil)
	notifyRec := httptest.NewRecorder()
	mux.ServeHTTP(notifyRec, notifyReq)
	if notifyRec.Code != http.StatusOK {
		t.Fatalf("notifications status=%d body=%s", notifyRec.Code, notifyRec.Body.String())
	}
}

func TestHandlerNotificationsLongPoll(t *testing.T) {
	notifications.ResetForTest()
	t.Cleanup(notifications.ResetForTest)
	h, _, _ := openTestHandler(t)

	items, cursor := getNotifications(t, h, "/api/v1/notifications?after=0&wait=0")
	if len(items) != 0 || cursor != 0 {
		t.Fatalf("empty hub baseline: items=%v cursor=%d, want empty/0", items, cursor)
	}

	notifications.PublishProcess(notifications.ProcessEvent{
		EventID: "evt-a", Module: "deploy", Title: "Deploy app", Summary: "started", State: "running", Level: "info", Origin: "user",
	})
	notifications.PublishProcess(notifications.ProcessEvent{
		EventID: "evt-b", Module: "deploy", Title: "Deploy db", Summary: "started", State: "running", Level: "info", Origin: "user",
	})
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=0&wait=0")
	if len(items) != 0 || cursor != 2 {
		t.Fatalf("baseline after publish: items=%v cursor=%d, want empty/2", items, cursor)
	}
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=1&wait=0")
	if len(items) != 1 || cursor != 2 {
		t.Fatalf("increment: items=%v cursor=%d, want one item/2", items, cursor)
	}
	if items[0]["eventId"] != "evt-b" || items[0]["module"] != "deploy" || items[0]["state"] != "running" {
		t.Fatalf("increment item = %#v", items[0])
	}

	notifications.PublishProcess(notifications.ProcessEvent{
		EventID: "evt-a", Module: "deploy", Title: "Deploy app", Summary: "finished", State: "succeeded", Level: "info", Origin: "user",
	})
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=2&wait=0")
	if len(items) != 1 || items[0]["seq"].(float64) != 3 || items[0]["state"] != "succeeded" || cursor != 3 {
		t.Fatalf("terminal: items=%v cursor=%d", items, cursor)
	}

	// A publication that happened before the poll returns immediately.
	notifications.PublishProcess(notifications.ProcessEvent{
		EventID: "evt-c", Module: "deploy", Title: "Deploy api", State: "running", Level: "info",
	})
	startedAt := time.Now()
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=3&wait=5")
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second || len(items) != 1 || cursor != 4 {
		t.Fatalf("immediate poll: items=%v cursor=%d after %s", items, cursor, elapsed)
	}

	// A pending long poll wakes as soon as a notification is published.
	go func() {
		time.Sleep(50 * time.Millisecond)
		notifications.PublishProcess(notifications.ProcessEvent{
			EventID: "evt-d", Module: "deploy", Title: "Deploy api", State: "running", Level: "info",
		})
	}()
	startedAt = time.Now()
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=4&wait=5")
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second || len(items) != 1 || cursor != 5 || items[0]["eventId"] != "evt-d" {
		t.Fatalf("wakeup poll: items=%v cursor=%d after %s", items, cursor, elapsed)
	}

	// A poll with nothing new holds for wait seconds, then returns the cursor.
	startedAt = time.Now()
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=5&wait=1")
	if elapsed := time.Since(startedAt); elapsed < 900*time.Millisecond {
		t.Fatalf("timeout poll returned after %s, want ~1s", elapsed)
	}
	if len(items) != 0 || cursor != 5 {
		t.Fatalf("timeout poll: items=%v cursor=%d, want empty/5", items, cursor)
	}

	// A cursor beyond the server's latest is a baseline (e.g. Panel restarted).
	items, cursor = getNotifications(t, h, "/api/v1/notifications?after=99&wait=0")
	if len(items) != 0 || cursor != 5 {
		t.Fatalf("ahead-of-server poll: items=%v cursor=%d, want empty/5", items, cursor)
	}
}

func TestHandlerNotificationsRespectsRequestContext(t *testing.T) {
	notifications.ResetForTest()
	t.Cleanup(notifications.ResetForTest)
	h, _, _ := openTestHandler(t)
	notifications.PublishProcess(notifications.ProcessEvent{EventID: "evt-ctx", Module: "deploy", State: "running"})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications?after=1&wait=30", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	time.AfterFunc(50*time.Millisecond, cancel)
	startedAt := time.Now()
	h.Notifications(rec, req)
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("cancelled request waited %s, want immediate", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_, cursor := getNotifications(t, h, "/api/v1/notifications?after=0&wait=0")
	if cursor != 1 {
		t.Fatalf("cursor = %d, want 1", cursor)
	}
}

func TestHandlerNotificationsQueryValidation(t *testing.T) {
	notifications.ResetForTest()
	t.Cleanup(notifications.ResetForTest)
	h, _, _ := openTestHandler(t)

	cases := []struct {
		name string
		path string
		code string
	}{
		{"unknown parameter", "/api/v1/notifications?cursor=1", "notifications_query_invalid"},
		{"repeated after", "/api/v1/notifications?after=1&after=2", "notifications_query_invalid"},
		{"repeated wait", "/api/v1/notifications?wait=0&wait=0", "notifications_query_invalid"},
		{"after not a number", "/api/v1/notifications?after=abc", "notifications_after_invalid"},
		{"after negative", "/api/v1/notifications?after=-1", "notifications_after_invalid"},
		{"wait not a number", "/api/v1/notifications?wait=abc", "notifications_wait_invalid"},
		{"wait negative", "/api/v1/notifications?wait=-1", "notifications_wait_invalid"},
		{"wait too large", "/api/v1/notifications?wait=31", "notifications_wait_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			h.Notifications(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var envelope httpx.Envelope
			if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if envelope.Error == nil || envelope.Error.Code != tc.code {
				t.Fatalf("error = %#v, want code %q", envelope.Error, tc.code)
			}
		})
	}
}

func getNotifications(t *testing.T, h *modevents.Handler, target string) ([]map[string]any, uint64) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.Notifications(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("notifications status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	rawItems, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items = %#v", data["items"])
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("item = %#v", raw)
		}
		items = append(items, item)
	}
	cursor, ok := data["cursor"].(float64)
	if !ok {
		t.Fatalf("cursor = %#v", data["cursor"])
	}
	return items, uint64(cursor)
}

func TestHandlerClearEventsAndLogs(t *testing.T) {
	h, store, logDir := openTestHandler(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, logging.ContextRecord{
		EventID: "event-clear",
		Module:  "panel",
		Origin:  "process",
		Title:   "clear me",
		Summary: "before",
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDir, "panel-20260102.jsonl")
	if err := os.WriteFile(path, []byte(`{"ts":"2026-01-02T00:00:00Z","level":"info","module":"panel","eventId":"event-clear","msg":"line"}\n`), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/v1/events/clear", strings.NewReader(`{"confirmation":"WRONG"}`))
	bad.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	h.Clear(badRec, bad)
	if badRec.Code != http.StatusUnprocessableEntity && badRec.Code != http.StatusBadRequest {
		t.Fatalf("bad confirmation status=%d body=%s", badRec.Code, badRec.Body.String())
	}

	ok := httptest.NewRequest(http.MethodPost, "/api/v1/events/clear", strings.NewReader(`{"confirmation":"CLEAR EVENTS AND LOGS"}`))
	ok.Header.Set("Content-Type", "application/json")
	okRec := httptest.NewRecorder()
	h.Clear(okRec, ok)
	if okRec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", okRec.Code, okRec.Body.String())
	}
	rows, err := store.List(ctx, events.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("events remain: %#v", rows)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("jsonl still present: %v", err)
	}
}
