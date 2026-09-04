package orm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Step is a versioned, one-time migration step.
type Step struct {
	ID  string
	Run func(ctx context.Context, tx *sql.Tx) error
}

// RunSteps applies the given steps in order inside a single transaction on
// the given database, recording each applied step in orm_migrations.
// Already applied steps are skipped. Callers pass exactly the steps that
// belong to one database, so libraries stay isolated.
func RunSteps(ctx context.Context, db *sql.DB, steps []Step) error {
	for _, s := range steps {
		if strings.TrimSpace(s.ID) == "" {
			return fmt.Errorf("orm: step id is required")
		}
		if s.Run == nil {
			return fmt.Errorf("orm: step %s has a nil Run function", s.ID)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orm_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	for _, s := range steps {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM orm_migrations WHERE id = ?`, s.ID).Scan(&exists)
		if err == sql.ErrNoRows {
			if err := s.Run(ctx, tx); err != nil {
				return fmt.Errorf("orm: step %s failed: %w", s.ID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO orm_migrations (id, applied_at) VALUES (?, ?)`, s.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return tx.Commit()
}
