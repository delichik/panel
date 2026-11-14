package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"panel/internal/modules/activity"
	"panel/internal/modules/runtimeevents"
	"panel/internal/platform/activitylog"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
	platformevents "panel/internal/platform/events"
	"panel/internal/platform/logging"
)

func testActivityService(t *testing.T) *activity.Service {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = activitylog.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	service := activity.NewService(db)
	if err = service.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return service
}
func TestActivityResponseLinksStreamReceipt(t *testing.T) {
	service := testActivityService(t)
	ctx := activitylog.WithReceipt(logging.Ensure(context.Background(), logging.Context{Module: "http"}))
	streamID := "stream-op"
	if c, ok := logging.Lookup(ctx); ok {
		streamID = c.StreamID
	}
	activitylog.RecordReceipt(ctx, streamID, streamID, 0)

	recorder := httptest.NewRecorder()
	response := &activityResponse{ResponseWriter: recorder}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(202)
	_, _ = response.Write([]byte(`{"data":{"resourceId":"app"}}`))
	response.finish(ctx, service)
	var body map[string]map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["data"]["operationId"] != streamID {
		t.Fatalf("missing stream receipt: %#v", body["data"])
	}
}
func TestSystemEventWriterStopsWithoutBlocking(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	writer := &systemEventWriter{service: activity.NewService(db)}
	finished := make(chan struct{})
	go func() {
		writer.Log(context.Background(), runtimeevents.WriteEventInput{EventType: "agent.disconnected", Summary: "disconnected"})
		close(finished)
	}()
	time.Sleep(10 * time.Millisecond)
	stopped := make(chan struct{})
	go func() { writer.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("writer stop blocked")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("background writer survived stop")
	}
}

func TestSystemEventWriterDoesNotCreateEventsRows(t *testing.T) {
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

	eventsStore := platformevents.NewStore(store.AppDB())
	logging.SetContextStore(eventsStore)
	t.Cleanup(func() { logging.SetContextStore(nil) })
	// Do not call logging.Configure: JSONL sink is process-global and would
	// lock files under t.TempDir, breaking cleanup. ContextStore alone is enough.

	writer := &systemEventWriter{service: activity.NewService(store.AppDB())}
	writer.Log(context.Background(), runtimeevents.WriteEventInput{
		EventType: "agent.disconnected",
		Severity:  "warning",
		Summary:   "Agent report stream disconnected: srv-1",
		Source:    "agent-report",
	})

	rows, err := eventsStore.List(context.Background(), platformevents.ListFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("agent.disconnected must not create events rows, got %#v", rows)
	}
}
func TestActivityResponseDoesNotBufferDownloads(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := &activityResponse{ResponseWriter: recorder}
	response.Header().Set("Content-Type", "application/octet-stream")
	data := strings.Repeat("binary", 200000)
	if _, err := response.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	response.finish(context.Background(), nil)
	if recorder.Body.String() != data || response.buffer.Len() != 0 {
		t.Fatal("download content changed or remained buffered")
	}
}
