package panel

import (
	"context"
	"database/sql"
	"fmt"

	"panel/internal/modules/observability/diagnostics"
	"panel/internal/platform/activitylog"
)

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
	if err := clearAllUserTables(ctx, a.store.CoordDB()); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if err := clearNamedTables(ctx, a.store.LogDB(), []string{
		"runtime_event_details", "runtime_events",
		"activity_operation_versions", "activity_projection_events", "activity_search",
	}); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if _, err := a.store.LogDB().ExecContext(ctx, `UPDATE activity_projection_checkpoint SET seq=0`); err != nil {
		return diagnostics.ClearRuntimeDataResult{}, err
	}
	if err := clearNamedTables(ctx, a.store.MetricsDB(), []string{"metrics_snapshots"}); err != nil {
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// SQLite has no TRUNCATE statement. An unconditional DELETE with no
	// DELETE trigger uses its truncate optimization; disabling secure_delete
	// avoids overwriting every freed page before VACUUM replaces the file.
	if _, err = tx.ExecContext(ctx, `PRAGMA secure_delete=OFF`); err != nil {
		return err
	}
	statements := []string{
		`DELETE FROM task_steps`,
		`DELETE FROM tasks`,
		`DELETE FROM jobs`,
		`DELETE FROM application_reconcile_states`,
		`UPDATE application_instances SET last_reconcile_job_id='',last_error_code='',last_error_class='',last_error_message='',last_error_detail=''`,
		`UPDATE applications SET job_id='',last_deployment_id='',last_error=''`,
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err = activitylog.Clear(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func clearNamedTables(ctx context.Context, db *sql.DB, tables []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `PRAGMA secure_delete=OFF`); err != nil {
		return err
	}
	for _, table := range tables {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
			return err
		}
		if exists == 1 {
			if _, err = tx.ExecContext(ctx, `DELETE FROM "`+table+`"`); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func clearAllUserTables(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('orm_meta','orm_migrations')`)
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
	return clearNamedTables(ctx, db, tables)
}
