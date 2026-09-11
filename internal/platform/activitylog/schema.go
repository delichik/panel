package activitylog

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrate owns the immutable ledger separately from destructive ORM migration.
func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS activity_events (
 seq INTEGER PRIMARY KEY AUTOINCREMENT,
 event_id TEXT NOT NULL UNIQUE,
 event_version INTEGER NOT NULL DEFAULT 1,
 event_type TEXT NOT NULL,
 kind TEXT NOT NULL DEFAULT 'lifecycle',
 level TEXT NOT NULL DEFAULT 'info',
 domain TEXT NOT NULL DEFAULT 'system',
 action TEXT NOT NULL DEFAULT '', operation_id TEXT NOT NULL DEFAULT '',
 run_id TEXT NOT NULL DEFAULT '', execution_id TEXT NOT NULL DEFAULT '',
 step_id TEXT NOT NULL DEFAULT '', parent_step_id TEXT NOT NULL DEFAULT '',
 causation_event_id TEXT NOT NULL DEFAULT '',
 source_id TEXT NOT NULL DEFAULT 'panel', source_epoch TEXT NOT NULL DEFAULT 'panel',
 source_stream_id TEXT NOT NULL DEFAULT 'system', source_seq INTEGER NOT NULL,
 occurred_at TEXT NOT NULL, recorded_at TEXT NOT NULL,
 actor_kind TEXT NOT NULL DEFAULT 'system', actor_id TEXT NOT NULL DEFAULT '', actor_name TEXT NOT NULL DEFAULT '',
 initiator_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(initiator_json)),
 resources_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(resources_json)),
 trigger TEXT NOT NULL DEFAULT '', request_id TEXT NOT NULL DEFAULT '', stream TEXT NOT NULL DEFAULT '',
 message_code TEXT NOT NULL DEFAULT '', message_args_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(message_args_json)),
 text TEXT NOT NULL DEFAULT '', data_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(data_json)),
 content_hash TEXT NOT NULL DEFAULT '',
 UNIQUE(source_id,source_epoch,source_stream_id,source_seq))`,
		`CREATE INDEX IF NOT EXISTS activity_events_operation ON activity_events(operation_id,seq)`,
		`CREATE INDEX IF NOT EXISTS activity_events_execution ON activity_events(execution_id,seq)`,
		`CREATE INDEX IF NOT EXISTS activity_events_step ON activity_events(step_id,seq)`,
		`CREATE INDEX IF NOT EXISTS activity_events_time ON activity_events(recorded_at,seq)`,
		`CREATE TABLE IF NOT EXISTS activity_evidence_chunks (
 evidence_id TEXT NOT NULL, chunk_seq INTEGER NOT NULL,
 event_id TEXT NOT NULL REFERENCES activity_events(event_id) ON DELETE RESTRICT,
 codec TEXT NOT NULL DEFAULT 'identity',content BLOB NOT NULL,content_hash TEXT NOT NULL,
 PRIMARY KEY(evidence_id,chunk_seq))`,
		`CREATE TRIGGER IF NOT EXISTS activity_events_no_replace BEFORE INSERT ON activity_events
 WHEN EXISTS(SELECT 1 FROM activity_events WHERE event_id=NEW.event_id OR seq=NEW.seq OR
 (source_id=NEW.source_id AND source_epoch=NEW.source_epoch AND source_stream_id=NEW.source_stream_id AND source_seq=NEW.source_seq))
 BEGIN SELECT RAISE(ABORT,'activity_append_only'); END`,
		`CREATE TRIGGER IF NOT EXISTS activity_evidence_no_replace BEFORE INSERT ON activity_evidence_chunks
 WHEN EXISTS(SELECT 1 FROM activity_evidence_chunks WHERE evidence_id=NEW.evidence_id AND chunk_seq=NEW.chunk_seq)
 OR EXISTS(SELECT 1 FROM activity_events WHERE event_type='evidence.sealed' AND json_extract(data_json,'$.evidenceId')=NEW.evidence_id)
 BEGIN SELECT RAISE(ABORT,'activity_append_only'); END`,
	}
	for _, table := range []string{"activity_events", "activity_evidence_chunks"} {
		for _, verb := range []string{"UPDATE", "DELETE"} {
			statements = append(statements, fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s_no_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'activity_append_only'); END`, table, verb, verb, table))
		}
	}
	for _, q := range statements {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("activity schema: %w", err)
		}
	}
	return nil
}

const Columns = `seq,event_id,event_version,event_type,kind,level,domain,action,operation_id,run_id,execution_id,step_id,parent_step_id,causation_event_id,source_id,source_epoch,source_stream_id,source_seq,occurred_at,recorded_at,actor_kind,actor_id,actor_name,initiator_json,resources_json,trigger,request_id,stream,message_code,message_args_json,text,data_json,content_hash`
