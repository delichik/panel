package panel

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestClearTableBatchedCommitsEachBatch(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE records(id INTEGER PRIMARY KEY); CREATE TRIGGER stop_third_batch BEFORE DELETE ON records WHEN OLD.id=2001 BEGIN SELECT RAISE(ABORT,'stop'); END`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO records(id) VALUES(?)`)
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2505; id++ {
		if _, err = stmt.Exec(id); err != nil {
			t.Fatal(err)
		}
	}
	_ = stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err = clearTableBatched(context.Background(), db, "records", 1000); err == nil {
		t.Fatal("expected the third batch to fail")
	}
	var remaining int
	if err = db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 505 {
		t.Fatalf("completed batches must remain committed: remaining=%d", remaining)
	}
	if _, err = db.Exec(`DROP TRIGGER stop_third_batch`); err != nil {
		t.Fatal(err)
	}
	if err = clearTableBatched(context.Background(), db, "records", 1000); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM records`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("records not cleared: remaining=%d err=%v", remaining, err)
	}
}
