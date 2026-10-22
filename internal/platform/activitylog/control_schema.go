package activitylog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"modernc.org/sqlite"
)

// Functions are registered during package initialization, before any SQLite
// connection is opened. They apply the same redaction boundary to raw SQL
// control writes and Go producers. Kept for Migrate/Clear compatibility while
// product facts move to unified logging.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("activity_redact", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] == nil {
			return "", nil
		}
		return Redact(fmt.Sprint(args[0])), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("activity_redact_json", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var value any
		if err := json.Unmarshal([]byte(fmt.Sprint(args[0])), &value); err != nil {
			return nil, err
		}
		out, err := json.Marshal(redactValue(value))
		return string(out), err
	})
	sqlite.MustRegisterDeterministicScalarFunction("activity_hash", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		digest := sha256.Sum256([]byte(fmt.Sprint(args[0])))
		return hex.EncodeToString(digest[:]), nil
	})
}

// activityControlTriggerNames are the historical control-plane triggers that
// inserted into activity_events. InstallControlTriggers now only drops them.
var activityControlTriggerNames = []string{
	"activity_task_requested",
	"activity_task_transition",
	"activity_task_source_lost",
	"activity_step_insert",
	"activity_step_update",
	"activity_task_output",
	"activity_job_requested",
	"activity_job_transition",
	"activity_job_verified",
	"activity_job_new_intent",
	"activity_job_intent_relation",
	"activity_job_shared_result",
	"activity_observation",
}

// InstallControlTriggers is a no-op installer that DROPs legacy activity_*
// control triggers when present. Product/process facts no longer INSERT into
// activity_events; callers must not fail when triggers are absent.
func InstallControlTriggers(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("activity control begin: %w", err)
	}
	defer tx.Rollback()
	for _, name := range activityControlTriggerNames {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
			return fmt.Errorf("activity control drop %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("activity control commit: %w", err)
	}
	return nil
}
