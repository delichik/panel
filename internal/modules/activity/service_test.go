package activity

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"panel/internal/platform/activitylog"
)

func testActivity(t *testing.T) *Service {
	t.Helper()
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	index, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "log.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close(); index.Close() })
	if e = activitylog.Migrate(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	s := NewService(db, index)
	if e = s.Init(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestAppendOnlyAndIdempotency(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	in := EventInput{EventID: "evt-one", EventType: "operation.requested", OperationID: "op-one", Text: "password=secret", SourceSeq: 1, OccurredAt: time.Now().UTC()}
	first, e := s.Append(ctx, []EventInput{in})
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Append(ctx, []EventInput{in})
	if e != nil || first.Events[0] != second.Events[0] {
		t.Fatalf("replay: %v %+v", e, second)
	}
	in.Text = "changed"
	if _, e = s.Append(ctx, []EventInput{in}); e == nil {
		t.Fatal("identity overwrite accepted")
	}
	for _, q := range []string{`UPDATE activity_events SET text='changed'`, `DELETE FROM activity_events`, `INSERT OR REPLACE INTO activity_events(event_id,event_type,source_seq,occurred_at,recorded_at) VALUES('evt-one','fake',1,'2026-09-10T00:00:00Z','2026-09-10T00:00:00Z')`} {
		if _, e = s.db.Exec(q); e == nil {
			t.Fatalf("mutation accepted: %s", q)
		}
	}
	event, e := s.GetEvent(ctx, "evt-one")
	if e != nil || event.Text != "password=[REDACTED]" {
		t.Fatalf("redaction: %+v %v", event, e)
	}
}
func TestSnapshotPaginationAndLateOutput(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	batch := []EventInput{}
	for i := 0; i < 406; i++ {
		batch = append(batch, EventInput{EventID: fmt.Sprint("evt-", i), EventType: "output.chunk", Kind: "output", OperationID: "op-long", ExecutionID: "exec-long", Text: fmt.Sprint("line ", i)})
	}
	if _, e := s.Append(ctx, batch); e != nil {
		t.Fatal(e)
	}
	f := Filter{OperationID: "op-long", Limit: 100}
	page, e := s.Events(ctx, f)
	if e != nil {
		t.Fatal(e)
	}
	snapshot := page.SnapshotSeq
	seen := map[string]bool{}
	if _, e = s.Append(ctx, []EventInput{{EventID: "late", EventType: "output.chunk", Kind: "output", OperationID: "op-long", ExecutionID: "exec-long", Text: "late output"}}); e != nil {
		t.Fatal(e)
	}
	for {
		for _, event := range page.Items {
			if seen[event.EventID] {
				t.Fatal("duplicate page event")
			}
			seen[event.EventID] = true
			if event.Seq > snapshot {
				t.Fatal("snapshot drift")
			}
		}
		if !page.HasMore {
			break
		}
		f.Cursor = page.NextCursor
		page, e = s.Events(ctx, f)
		if e != nil {
			t.Fatal(e)
		}
	}
	if len(seen) != 406 {
		t.Fatalf("lost output: %d", len(seen))
	}
	tail, e := s.Tail(ctx, Filter{OperationID: "op-long", AfterSeq: snapshot})
	if e != nil || len(tail.Items) != 1 || tail.Items[0].EventID != "late" {
		t.Fatalf("late output: %+v %v", tail, e)
	}
}
func TestHistoricalResultsAndProjectionRebuild(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	t0 := time.Now().UTC()
	appendEvent := func(id, typ, result string) {
		t.Helper()
		_, err := s.Append(ctx, []EventInput{{EventID: id, EventType: typ, OperationID: "op", ExecutionID: "exec", OccurredAt: t0, Text: result, Resources: []Resource{{Type: "server", ID: "srv", Name: "Original", Role: "target"}}, Data: map[string]any{"phase": "ended", "result": result}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	appendEvent("first", "execution.finished", "failed")
	before, err := s.Operations(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Items) != 1 || before.Items[0].Result != "failed" {
		t.Fatalf("projection %+v", before)
	}
	appendEvent("second", "verification.finished", "succeeded")
	old, err := s.Operation(ctx, "op", before.SnapshotSeq, 0)
	if err != nil || old.Operation.Result != "failed" {
		t.Fatalf("historical result changed: %+v %v", old, err)
	}
	current, err := s.Operation(ctx, "op", 0, 0)
	if err != nil || current.Operation.Result != "succeeded" {
		t.Fatalf("current %+v %v", current, err)
	}
	for _, q := range []string{`DELETE FROM activity_projection_events`, `DELETE FROM activity_operation_versions`, `DELETE FROM activity_search`, `UPDATE activity_projection_checkpoint SET seq=0`} {
		if _, err = s.index.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rebuilt, err := s.Operation(ctx, "op", 0, 0)
	if err != nil || rebuilt.Operation.Result != current.Operation.Result || rebuilt.Operation.EventCount != current.Operation.EventCount {
		t.Fatalf("rebuild %+v %v", rebuilt, err)
	}
}
func TestAppendTxRollsBackWithBusinessState(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	_, _ = s.db.Exec(`CREATE TABLE business(id TEXT)`)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = tx.Exec(`INSERT INTO business VALUES('intent')`)
	if _, e = s.AppendTx(ctx, tx, []EventInput{{EventID: "rollback", EventType: "operation.requested"}}); e != nil {
		t.Fatal(e)
	}
	_ = tx.Rollback()
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM activity_events`).Scan(&count)
	if count != 0 {
		t.Fatal("uncommitted receipt became history")
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM business`).Scan(&count)
	if count != 0 {
		t.Fatal("business state escaped transaction")
	}
}

func TestMultiServerResultsDoNotCollapseByApplication(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	for i, result := range []string{"succeeded", "failed"} {
		_, err := s.Append(ctx, []EventInput{{EventID: fmt.Sprint("target", i), EventType: "execution.finished", OperationID: "deploy", ExecutionID: fmt.Sprint("exec", i), Resources: []Resource{{Type: "application", ID: "app", Role: "target"}, {Type: "server", ID: fmt.Sprint("server", i), Role: "executor"}}, Data: map[string]any{"phase": "ended", "result": result}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	detail, err := s.Operation(ctx, "deploy", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Operation.Result != "partial" {
		t.Fatalf("multi-target result: %+v", detail.Operation)
	}
}
func TestCompletedExecutionWaitsForSourceEvidence(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	_, err := s.Append(ctx, []EventInput{{EventID: "remote-finish", EventType: "execution.finished", OperationID: "op", ExecutionID: "exec", SourceID: "agent:server", SourceStreamID: "exec", Data: map[string]any{"status": "succeeded"}}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.Operation(ctx, "op", 0, 0)
	if err != nil || detail.Operation.EvidenceComplete {
		t.Fatalf("premature complete evidence: %+v %v", detail, err)
	}
	_, err = s.Append(ctx, []EventInput{{EventID: "remote-close", EventType: "stream.closed", OperationID: "op", ExecutionID: "exec", SourceID: "agent:server", SourceStreamID: "exec"}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = s.Operation(ctx, "op", 0, 0)
	if err != nil || !detail.Operation.EvidenceComplete {
		t.Fatalf("stream close missing: %+v %v", detail, err)
	}
}

func TestSearchShortUnicodeAndWildcardLiteral(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	for i, text := range []string{"节点 nginx ready", "disk 90% full", "ordinary"} {
		if _, err := s.Append(ctx, []EventInput{{EventID: fmt.Sprint("search", i), EventType: "system.observed", Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"节点", "nginx", "%"} {
		page, err := s.Events(ctx, Filter{Q: q})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("search %q: %+v %v", q, page, err)
		}
	}
}

func TestTailCursorDrainsEveryBatch(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if _, err := s.Append(ctx, []EventInput{{EventID: fmt.Sprint("tail", i), EventType: "output.chunk", Kind: "output"}}); err != nil {
			t.Fatal(err)
		}
	}
	f := Filter{Limit: 3, AfterSeq: 1}
	seen := map[int64]bool{}
	for {
		page, err := s.Tail(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if seen[e.Seq] {
				t.Fatal("tail repeated a cursor page")
			}
			seen[e.Seq] = true
		}
		if !page.HasMore {
			break
		}
		f.Cursor = page.NextCursor
	}
	if len(seen) != 7 {
		t.Fatalf("tail lost records: %d", len(seen))
	}
}
func TestIndependentTasksAndQueuedRetryProjection(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	add := func(id, execution, logical, phase, result string) {
		t.Helper()
		_, err := s.Append(ctx, []EventInput{{EventID: id, EventType: "execution.changed", OperationID: "op", ExecutionID: execution, Resources: []Resource{{Type: "server", ID: "same", Role: "target"}}, Data: map[string]any{"logicalExecutionId": logical, "phase": phase, "result": result}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("a", "exec-a", "task-a", "ended", "failed")
	add("b", "exec-b", "task-b", "ended", "succeeded")
	detail, err := s.Operation(ctx, "op", 0, 0)
	if err != nil || detail.Operation.Result != "partial" {
		t.Fatalf("independent tasks collapsed: %+v %v", detail.Operation, err)
	}
	add("retry", "exec-c", "task-a", "queued", "")
	detail, err = s.Operation(ctx, "op", 0, 0)
	if err != nil || detail.Operation.Phase == "ended" {
		t.Fatalf("queued retry hidden: %+v %v", detail.Operation, err)
	}
	add("recovered", "exec-c", "task-a", "ended", "succeeded")
	detail, err = s.Operation(ctx, "op", 0, 0)
	if err != nil || detail.Operation.Result != "succeeded" {
		t.Fatalf("old failure overrides recovery: %+v %v", detail.Operation, err)
	}
}
func TestOutputDoesNotCopyExecutionTreePerLine(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	_, err := s.Append(ctx, []EventInput{{EventID: "request", EventType: "operation.requested", OperationID: "op"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		_, err = s.Append(ctx, []EventInput{{EventID: fmt.Sprint("output", i), EventType: "output.chunk", Kind: "output", OperationID: "op", Text: "line"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Operations(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].EventCount != 51 {
		t.Fatalf("event count omitted output: %+v", page.Items[0])
	}
	var count int
	if err = s.index.QueryRow(`SELECT COUNT(*) FROM activity_operation_versions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("output copied execution state %d times: %v", count, err)
	}
}

func TestLateErrorOutputPersistsAcrossProjectionBatches(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	if _, err := s.Append(ctx, []EventInput{{EventID: "finish", EventType: "execution.finished", OperationID: "op", Data: map[string]any{"phase": "ended", "result": "succeeded"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, []EventInput{{EventID: "late-error", EventType: "output.chunk", Kind: "output", Level: "error", OperationID: "op", Text: "late stderr"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CatchUp(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, []EventInput{{EventID: "later-output", EventType: "output.chunk", Kind: "output", OperationID: "op", Text: "next batch"}}); err != nil {
		t.Fatal(err)
	}
	page, err := s.Operations(ctx, Filter{HadError: "true"})
	if err != nil || len(page.Items) != 1 || !page.Items[0].HadError {
		t.Fatalf("late error lost: %+v %v", page, err)
	}
}

// ORCH-ACT-001: operation summaries and search use structured errors, not trigger text.
func TestFailureSummaryUsesStructuredJobErrorInsteadOfTriggerReason(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	_, err := s.Append(ctx, []EventInput{{
		EventID:     "job-failed",
		EventType:   "execution.finished",
		Level:       "error",
		Domain:      "application",
		Action:      "apply",
		OperationID: "intent-one",
		ExecutionID: "job-one:attempt:1",
		Trigger:     "agent_report",
		Text:        "agent_report",
		Data: map[string]any{
			"phase":     "ended",
			"result":    "failed",
			"error":     "image pull failed: password=registry-secret",
			"detail":    "registry rejected the manifest",
			"errorCode": "image_pull_failed",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.Operation(ctx, "intent-one", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := detail.Operation.FailureSummary, "image pull failed: password=[REDACTED]"; got != want {
		t.Fatalf("failure summary = %q, want %q", got, want)
	}
	event, err := s.GetEvent(ctx, "job-failed")
	if err != nil {
		t.Fatal(err)
	}
	if event.Text != "agent_report" {
		t.Fatalf("raw trigger reason changed: %q", event.Text)
	}
	page, err := s.Events(ctx, Filter{Q: "image_pull_failed"})
	if err != nil || len(page.Items) != 1 || page.Items[0].EventID != "job-failed" {
		t.Fatalf("structured error should be searchable: page=%#v err=%v", page, err)
	}
}

// ORCH-ACT-001: structured failure fields have a stable fallback order.
func TestFailureSummaryStructuredFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		want string
	}{
		{name: "detail", data: map[string]any{"detail": "runtime detail", "errorCode": "runtime_failed"}, want: "runtime detail"},
		{name: "error code", data: map[string]any{"errorCode": "runtime_failed"}, want: "runtime_failed"},
		{name: "no structured failure", data: map[string]any{}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Event{EventInput: EventInput{Level: "error", Text: "manual", Data: tc.data}}
			state := newState("operation")
			applyEvent(&state, e)
			if got := state.Operation.FailureSummary; got != tc.want {
				t.Fatalf("failure summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQueuedAggregateDoesNotReportSuccessBeforeChildren(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	_, err := s.Append(ctx, []EventInput{{EventID: "parent", EventType: "operation.requested", OperationID: "batch", ExecutionID: "parent-exec", Data: map[string]any{"isAggregate": true, "phase": "queued"}}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.Operation(ctx, "batch", 0, 0)
	if err != nil || detail.Operation.Phase != "queued" || detail.Operation.Result != "" {
		t.Fatalf("premature batch success %+v %v", detail.Operation, err)
	}
}
