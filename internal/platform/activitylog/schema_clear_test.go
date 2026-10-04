package activitylog

import (
	"context"
	"testing"
)

func TestClearLedgerReinstallsAppendOnlyProtection(t *testing.T) {
	db := newControlSchemaTestDB(t)
	if _, err := db.Exec(`INSERT INTO activity_events(event_id,event_type,source_seq,occurred_at,recorded_at) VALUES('event-1','test',1,'2026-09-14T00:00:00Z','2026-09-14T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = Clear(context.Background(), tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM activity_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("events after clear = %d, err=%v", count, err)
	}
	if _, err = db.Exec(`INSERT INTO activity_events(event_id,event_type,source_seq,occurred_at,recorded_at) VALUES('event-2','test',1,'2026-09-14T00:00:00Z','2026-09-14T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM activity_events`); err == nil {
		t.Fatal("append-only delete protection was not restored")
	}
}
