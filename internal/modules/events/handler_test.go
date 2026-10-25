package events_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	modevents "panel/internal/modules/events"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/events"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/logging"
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
}
