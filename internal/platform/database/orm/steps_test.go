package orm

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestStepsApplyAndSkip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := RegisterSteps(
		Step{ID: "step-a", Run: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS step_a (id TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO step_a (id) VALUES ('a')`)
			return err
		}},
		Step{ID: "step-b", Run: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS step_b (id TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO step_b (id) VALUES ('b')`)
			return err
		}},
	); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, db); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM step_a`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("step_a rows = %d, err = %v", n, err)
	}
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orm_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("applied = %d, err = %v", applied, err)
	}
	// second run skips everything
	if err := MigrateSteps(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM step_a`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("step_a rerun count = %d, err = %v", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM orm_migrations`).Scan(&applied); err != nil || applied != 2 {
		t.Fatalf("applied after rerun = %d, err = %v", applied, err)
	}
}

// stepFailCount makes the shared "step-fail" step fail only on its first
// invocation so later tests are not blocked by it.
var stepFailCount int

func TestStepsFailureRollsBack(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := RegisterSteps(
		Step{ID: "step-ok", Run: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS step_ok (id TEXT PRIMARY KEY)`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO step_ok (id) VALUES ('ok')`)
			return err
		}},
		Step{ID: "step-fail", Run: func(ctx context.Context, tx *sql.Tx) error {
			stepFailCount++
			if stepFailCount == 1 {
				return fmt.Errorf("boom")
			}
			return nil
		}},
	); err != nil {
		t.Fatal(err)
	}
	err := MigrateSteps(ctx, db)
	if err == nil {
		t.Fatal("expected step failure")
	}
	// The whole transaction, including the orm_migrations table, is
	// rolled back on failure.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='orm_migrations'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("orm_migrations table survived a rolled back run: %d", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM step_ok`).Scan(&n); err == nil && n != 0 {
		t.Fatalf("step_ok side effect not rolled back: %d rows", n)
	}
}

func TestStepsOrderPreserved(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	var order []string
	if err := RegisterSteps(
		Step{ID: "step-order-1", Run: func(ctx context.Context, tx *sql.Tx) error {
			order = append(order, "1")
			return nil
		}},
		Step{ID: "step-order-2", Run: func(ctx context.Context, tx *sql.Tx) error {
			order = append(order, "2")
			return nil
		}},
	); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSteps(ctx, db); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "1" || order[1] != "2" {
		t.Fatalf("order = %v", order)
	}
	// already applied: steps must not run again
	order = nil
	if err := MigrateSteps(ctx, db); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatalf("steps ran again: %v", order)
	}
}

func TestRegisterStepsValidation(t *testing.T) {
	if err := RegisterSteps(Step{ID: "", Run: func(ctx context.Context, tx *sql.Tx) error { return nil }}); err == nil {
		t.Fatal("expected empty id error")
	}
	if err := RegisterSteps(Step{ID: "step-nil", Run: nil}); err == nil {
		t.Fatal("expected nil run error")
	}
	if err := RegisterSteps(Step{ID: "step-a", Run: func(ctx context.Context, tx *sql.Tx) error { return nil }}); err == nil {
		t.Fatal("expected duplicate id error")
	}
}
