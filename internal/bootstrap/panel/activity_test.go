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
func TestActivityResponseLinksOnlyCommittedFacts(t *testing.T) {
	service := testActivityService(t)
	ctx := activitylog.WithReceipt(context.Background())
	receipt, err := service.Append(ctx, []activity.EventInput{{EventID: "accepted", EventType: "operation.requested", OperationID: "op"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, committed := range []bool{true, false} {
		recorder := httptest.NewRecorder()
		response := &activityResponse{ResponseWriter: recorder}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(202)
		_, _ = response.Write([]byte(`{"data":{"resourceId":"app"}}`))
		eventID := "accepted"
		if !committed {
			eventID = "not-committed"
		}
		activitylog.RecordReceipt(ctx, "op", eventID, receipt.HeadSeq)
		response.finish(ctx, service)
		var body map[string]map[string]any
		if err = json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if committed && body["data"]["operationId"] != "op" {
			t.Fatal("missing committed receipt")
		}
		if !committed && body["data"]["operationId"] != nil {
			t.Fatal("uncommitted fact leaked into response")
		}
	}
}
func TestSystemEventWriterStopsFailedBackgroundRetry(t *testing.T) {
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
		t.Fatal("writer stop blocked on retry")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("background writer survived stop")
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
