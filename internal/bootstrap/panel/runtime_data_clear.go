package panel

import (
	"context"
	"database/sql"
	"fmt"

	"panel/internal/modules/observability/diagnostics"
	"panel/internal/platform/activitylog"
)

const runtimeClearBatchSize = 1000

// clearRuntimeData pauses durable workers so deleted coordination rows cannot
// be recreated by an in-flight completion. Resource configuration and current
// desired/observed application state are intentionally retained.
func (a *App) clearRuntimeData(ctx context.Context) (diagnostics.ClearRuntimeDataResult, error) {
	if a == nil || a.store == nil {
		return diagnostics.ClearRuntimeDataResult{}, fmt.Errorf("database store is unavailable")
	}
	if a.applicationSvc != nil {
		if err := a.applicationSvc.StopOrchestrator(); err != nil {
			return diagnostics.ClearRuntimeDataResult{}, err
		}
		defer func() { _ = a.applicationSvc.StartOrchestrator(context.Background()) }()
	}
	if a.tasks != nil {
		a.tasks.Stop()
		defer a.tasks.Start(context.Background())
	}
	if a.metricsCleanup != nil {
		a.metricsCleanup.Stop()
		defer a.metricsCleanup.Start(context.Background())
	}

	if err := clearAppCoordination(ctx, a.store.AppDB()); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	// Clear the user-visible projections immediately after their source ledger.
	// The retired coordination database may contain very large legacy tables
	// and must not delay removal of the activity history.
	if err := clearNamedTablesBatched(ctx, a.store.LogDB(), []string{
		"runtime_event_details", "runtime_events",
		"activity_operation_versions", "activity_projection_events", "activity_search",
	}, runtimeClearBatchSize); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if _, err := a.store.LogDB().ExecContext(ctx, `UPDATE activity_projection_checkpoint SET seq=0`); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if err := clearAllUserTablesBatched(ctx, a.store.CoordDB(), runtimeClearBatchSize); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if err := clearNamedTablesBatched(ctx, a.store.MetricsDB(), []string{"metrics_snapshots"}, runtimeClearBatchSize); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	// Logical clearing above uses SQLite's fast whole-table deletion path where
	// possible. Checkpoint first so the cleared state is durable; VACUUM is best
	// effort physical compaction and must not turn a successful clear into a
	// rolled-back or timed-out HTTP operation.
	for _, db := range []*sql.DB{a.store.AppDB(), a.store.LogDB(), a.store.CoordDB(), a.store.MetricsDB()} {
		_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
		_, _ = db.ExecContext(context.Background(), `VACUUM`)
	}
	return diagnostics.ClearRuntimeDataResult{Cleared: true}, nil
}

func clearAppCoordination(ctx context.Context, db *sql.DB) error {
	for _, table := range []string{"task_steps", "tasks", "jobs", "application_reconcile_states"} {
		if err := clearTableBatched(ctx, db, table, runtimeClearBatchSize); err != nil {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE application_instances SET last_reconcile_job_id='',last_error_code='',last_error_class='',last_error_message='',last_error_detail=''`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET job_id='',last_deployment_id='',last_error=''`); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return activitylog.ClearBatched(ctx, db, runtimeClearBatchSize)
}

func clearNamedTablesBatched(ctx context.Context, db *sql.DB, tables []string, batchSize int) error {
	for _, table := range tables {
		var exists int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 1 {
			if err := clearTableBatched(ctx, db, table, batchSize); err != nil {
				return err
			}
		}
	}
	return nil
}

func clearAllUserTablesBatched(ctx context.Context, db *sql.DB, batchSize int) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	rows, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('orm_meta','orm_migrations')`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var table string
		if err = rows.Scan(&table); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, table)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, table := range tables {
		if err = clearTableBatchedConn(ctx, conn, table, batchSize); err != nil {
			return err
		}
	}
	return nil
}

func clearTableBatched(ctx context.Context, db *sql.DB, table string, batchSize int) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return clearTableBatchedConn(ctx, conn, table, batchSize)
}

func clearTableBatchedConn(ctx context.Context, conn *sql.Conn, table string, batchSize int) error {
	if batchSize <= 0 {
		batchSize = runtimeClearBatchSize
	}
	quoted := quoteSQLiteIdentifier(table)
	for {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM `+quoted+` WHERE rowid IN (SELECT rowid FROM `+quoted+` LIMIT ?)`, batchSize)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		deleted, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if deleted < int64(batchSize) {
			return nil
		}
	}
}

func quoteSQLiteIdentifier(value string) string {
	quoted := `"`
	for _, char := range value {
		if char == '"' {
			quoted += `""`
		} else {
			quoted += string(char)
		}
	}
	return quoted + `"`
}
