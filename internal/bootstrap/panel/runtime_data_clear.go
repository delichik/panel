package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"panel/internal/modules/observability/diagnostics"
	"panel/internal/platform/activitylog"
	platformevents "panel/internal/platform/events"
	"panel/internal/platform/logging"
)

const runtimeClearBatchSize = 1000

// clearRuntimeData pauses durable workers so deleted coordination rows cannot
// be recreated by an in-flight completion. Resource configuration and current
// desired/observed application state are intentionally retained.
func (a *App) clearRuntimeData(ctx context.Context) (result diagnostics.ClearRuntimeDataResult, clearErr error) {
	if a == nil || a.store == nil {
		return diagnostics.ClearRuntimeDataResult{}, fmt.Errorf("database store is unavailable")
	}
	stage := "stopping_workers"
	report := func(next string) {
		stage = next
		diagnostics.ReportClearRuntimeDataProgress(ctx, next, result.Cleared)
	}
	var reportsPaused, tasksPaused, appsPaused, metricsPaused, eventsPaused bool
	// Always release admission, even after a panic or a partially committed
	// deletion. A failed worker restart is part of the result, never discarded.
	defer func() {
		if recover() != nil {
			clearErr = errors.New("runtime data clearing panicked")
		}
		if clearErr != nil {
			result.FailedStage = stage
		}
		report("resuming_workers")
		if eventsPaused {
			a.eventLogs.maintenance.Resume()
		}
		if metricsPaused {
			a.metricsCleanup.Start(context.Background())
		}
		if appsPaused {
			if err := a.applicationSvc.ResumeRuntimeWriters(context.Background()); err != nil {
				clearErr = errors.Join(clearErr, err)
				result.Error = "clear_runtime_data_resume_failed"
				if result.FailedStage == "" {
					result.FailedStage = "resuming_workers"
				}
			}
		}
		if tasksPaused {
			a.tasks.ResumeRuntimeWriters(context.Background())
		}
		if reportsPaused {
			a.agentReports.writers.Resume()
		}
		a.runtimeWriters.Resume()
	}()
	report(stage)
	// Each pause/drain stage gets its own budget so earlier waits cannot starve
	// later gates (DIAG-CLR-008).
	pauseStage := func(fn func(context.Context) error) error {
		drainCtx, drainCancel := context.WithTimeout(ctx, 30*time.Second)
		defer drainCancel()
		return fn(drainCtx)
	}
	if err := pauseStage(a.runtimeWriters.PauseContext); err != nil {
		return result, err
	}
	// Initial Agent probing also writes reports and can still be in progress
	// when a user clears immediately after Panel startup.
	if a.checkDone != nil {
		waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
		select {
		case <-a.checkDone:
			waitCancel()
		case <-waitCtx.Done():
			waitCancel()
			return result, waitCtx.Err()
		}
	}
	if a.agentReports != nil {
		reportsPaused = true
		if err := pauseStage(a.agentReports.writers.PauseContext); err != nil {
			return result, err
		}
	}
	if a.tasks != nil {
		tasksPaused = true
		if err := pauseStage(a.tasks.PauseRuntimeWriters); err != nil {
			return result, err
		}
	}
	if a.applicationSvc != nil {
		appsPaused = true
		if err := pauseStage(a.applicationSvc.PauseRuntimeWriters); err != nil {
			return result, err
		}
	}
	if a.metricsCleanup != nil {
		metricsPaused = a.metricsCleanup.Running()
		a.metricsCleanup.Stop()
	}
	if a.eventLogs != nil {
		eventsPaused = true
		if err := pauseStage(a.eventLogs.maintenance.PauseContext); err != nil {
			return result, err
		}
	}
	report("clearing_coordination")
	if err := clearAppCoordination(ctx, a.store.AppDB()); err != nil {
		return result, err
	}
	// Clear the user-visible projections immediately after their source ledger.
	// The retired coordination database may contain very large legacy tables
	// and must not delay removal of the activity history.
	report("clearing_logs")
	if err := clearNamedTablesBatched(ctx, a.store.LogDB(), []string{
		"task_logs", "event_run_steps", "event_runs",
		"runtime_event_details", "runtime_events",
		"activity_operation_versions", "activity_projection_events", "activity_search",
	}, runtimeClearBatchSize); err != nil {
		return result, err
	}
	if _, err := a.store.LogDB().ExecContext(ctx, `UPDATE activity_projection_checkpoint SET seq=0`); err != nil {
		return result, err
	}
	// Product diagnostics: AppDB process index + JSONL sink (DIAG-CLR-002).
	if err := platformevents.NewStore(a.store.AppDB()).ClearBatched(ctx); err != nil {
		return result, err
	}
	if err := logging.ClearJSONLFiles(); err != nil {
		return result, err
	}
	report("clearing_coordination")
	if err := clearAllUserTablesBatched(ctx, a.store.CoordDB(), runtimeClearBatchSize); err != nil {
		return result, err
	}
	report("clearing_metrics")
	if err := clearNamedTablesBatched(ctx, a.store.MetricsDB(), []string{"metrics_snapshots"}, runtimeClearBatchSize); err != nil {
		return result, err
	}
	result.Cleared = true
	report("compacting")
	// Logical clearing above uses SQLite's fast whole-table deletion path where
	// possible. Checkpoint first so the cleared state is durable; VACUUM is best
	// effort physical compaction and must not turn a successful clear into a
	// rolled-back or timed-out HTTP operation.
	for _, db := range []*sql.DB{a.store.AppDB(), a.store.LogDB(), a.store.CoordDB(), a.store.MetricsDB()} {
		_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
		compactCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, _ = db.ExecContext(compactCtx, `VACUUM`)
		cancel()
	}
	return result, nil
}

func clearAppCoordination(ctx context.Context, db *sql.DB) error {
	// jobs stays in the list so a database that still has the legacy table is
	// cleared. Deploy rows now live in event_runs and are removed with it.
	if err := clearNamedTablesBatched(ctx, db, []string{"task_logs", "event_run_steps", "event_runs", "jobs", "application_reconcile_states"}, runtimeClearBatchSize); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE application_instances SET last_reconcile_job_id='',last_error_code='',last_error_class='',last_error_message='',last_error_detail='',last_error='',last_error_at=NULL`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET job_id='',last_deployment_id='',last_error='',planning_error_json=''`); err != nil {
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
