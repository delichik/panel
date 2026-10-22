package activity

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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

func TestInitAndCatchUpNoProjection(t *testing.T) {
	s := testActivity(t)
	ctx := context.Background()
	mark, err := s.CatchUp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mark != 0 {
		t.Fatalf("catch-up watermark = %d, want 0 without projection", mark)
	}
	// Legacy Append remains available for schema/tools, but CatchUp must not advance.
	if _, err = s.Append(ctx, []EventInput{{EventID: "evt", EventType: "operation.requested", OperationID: "op"}}); err != nil {
		t.Fatal(err)
	}
	mark, err = s.CatchUp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mark != 0 {
		t.Fatalf("catch-up projected after append: watermark=%d", mark)
	}
}

func TestAppendOnlyAndIdempotency(t *testing.T) {
	t.Skip("legacy activity_events ledger immutability; product facts use unified logging")
}
func TestSnapshotPaginationAndLateOutput(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestHistoricalResultsAndProjectionRebuild(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestAppendTxRollsBackWithBusinessState(t *testing.T) {
	t.Skip("legacy activity_events ledger; product facts use unified logging")
}
func TestMultiServerResultsDoNotCollapseByApplication(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestCompletedExecutionWaitsForSourceEvidence(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestSearchShortUnicodeAndWildcardLiteral(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestTailCursorDrainsEveryBatch(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestIndependentTasksAndQueuedRetryProjection(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestOutputDoesNotCopyExecutionTreePerLine(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestLateErrorOutputPersistsAcrossProjectionBatches(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestFailureSummaryUsesStructuredJobErrorInsteadOfTriggerReason(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestFailureSummaryStructuredFallbacks(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestQueuedAggregateDoesNotReportSuccessBeforeChildren(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestOperationSummaryOmitsEventTree(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
func TestOperationsOriginSplitsUserAndSystemIntents(t *testing.T) {
	t.Skip("activity projection retired; product facts use unified logging")
}
