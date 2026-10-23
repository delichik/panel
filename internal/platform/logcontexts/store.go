package logcontexts

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/platform/logging"
)

// Store persists log_contexts rows in AppDB.
type Store struct {
	db *sql.DB
}

// NewStore wraps an AppDB connection. db may be nil only in tests that never call methods.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Ensure Store satisfies logging.ContextStore.
var _ logging.ContextStore = (*Store)(nil)

// Upsert inserts or merges a log context row by stream_id.
func (s *Store) Upsert(ctx context.Context, row logging.ContextRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	rec := fromLoggingRecord(row)
	if strings.TrimSpace(rec.StreamID) == "" || strings.TrimSpace(rec.Module) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if rec.Origin == "" {
		rec.Origin = OriginProcess
	}
	if rec.StartedAt == "" {
		rec.StartedAt = now
	}
	if rec.UpdatedAt == "" {
		rec.UpdatedAt = now
	}
	// last_source_seq is the agent ACK watermark only; Upsert never bumps it.
	seq := rec.LastSourceSeq
	if seq < 0 {
		seq = 0
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO log_contexts (
	stream_id, module, origin, operation_id, execution_id, request_id,
	title, summary, result, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(stream_id) DO UPDATE SET
	module=excluded.module,
	origin=CASE WHEN excluded.origin != '' THEN excluded.origin ELSE log_contexts.origin END,
	operation_id=CASE WHEN excluded.operation_id != '' THEN excluded.operation_id ELSE log_contexts.operation_id END,
	execution_id=CASE WHEN excluded.execution_id != '' THEN excluded.execution_id ELSE log_contexts.execution_id END,
	request_id=CASE WHEN excluded.request_id != '' THEN excluded.request_id ELSE log_contexts.request_id END,
	title=CASE WHEN excluded.title != '' THEN excluded.title ELSE log_contexts.title END,
	summary=CASE WHEN excluded.summary != '' THEN excluded.summary ELSE log_contexts.summary END,
	result=CASE WHEN excluded.result != '' THEN excluded.result ELSE log_contexts.result END,
	had_error=CASE WHEN excluded.had_error != 0 THEN 1 ELSE log_contexts.had_error END,
	actor_json=CASE WHEN excluded.actor_json != '' THEN excluded.actor_json ELSE log_contexts.actor_json END,
	resources_json=CASE WHEN excluded.resources_json != '' THEN excluded.resources_json ELSE log_contexts.resources_json END,
	log_file=CASE WHEN excluded.log_file != '' THEN excluded.log_file ELSE log_contexts.log_file END,
	updated_at=excluded.updated_at,
	finished_at=CASE WHEN excluded.finished_at != '' THEN excluded.finished_at ELSE log_contexts.finished_at END
`,
		rec.StreamID, rec.Module, rec.Origin, rec.OperationID, rec.ExecutionID, rec.RequestID,
		rec.Title, rec.Summary, rec.Result, boolToInt(rec.HadError), rec.ActorJSON, rec.ResourcesJSON,
		seq, rec.LogFile, rec.StartedAt, rec.UpdatedAt, rec.FinishedAt,
	)
	return err
}

// Touch bumps updated_at / summary and optionally marks had_error from level.
// It does not change last_source_seq (ACK watermark).
func (s *Store) Touch(ctx context.Context, streamID string, level string, msg string) error {
	if s == nil || s.db == nil {
		return nil
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hadError := 0
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error", "fatal", "panic", "dpanic":
		hadError = 1
	}
	summary := strings.TrimSpace(msg)
	if len(summary) > 512 {
		summary = summary[:512]
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE log_contexts SET
	updated_at = ?,
	had_error = CASE WHEN ? != 0 THEN 1 ELSE had_error END,
	summary = CASE WHEN ? != '' THEN ? ELSE summary END
WHERE stream_id = ?
`, now, hadError, summary, summary, streamID)
	return err
}

// Get returns one row by stream_id.
func (s *Store) Get(ctx context.Context, streamID string) (Record, error) {
	var rec Record
	if s == nil || s.db == nil {
		return rec, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `
SELECT stream_id, module, origin, operation_id, execution_id, request_id,
	title, summary, result, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
FROM log_contexts WHERE stream_id = ?
`, strings.TrimSpace(streamID))
	return scanRecord(row)
}

// List returns matching rows ordered by updated_at DESC.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var (
		conds []string
		args  []any
	)
	if v := strings.TrimSpace(filter.Origin); v != "" {
		conds = append(conds, "origin = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.Module); v != "" {
		conds = append(conds, "module = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.OperationID); v != "" {
		conds = append(conds, "operation_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.ExecutionID); v != "" {
		conds = append(conds, "execution_id = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.From); v != "" {
		conds = append(conds, "updated_at >= ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.To); v != "" {
		conds = append(conds, "updated_at <= ?")
		args = append(args, v)
	}
	if filter.HadError != nil {
		conds = append(conds, "had_error = ?")
		args = append(args, boolToInt(*filter.HadError))
	}
	query := `
SELECT stream_id, module, origin, operation_id, execution_id, request_id,
	title, summary, result, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
FROM log_contexts`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " LIMIT ?"
	args = append(args, limit)
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// UpdateFinished marks a stream finished with optional result text.
func (s *Store) UpdateFinished(ctx context.Context, streamID, result string, hadError bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE log_contexts SET
	finished_at = ?,
	updated_at = ?,
	result = CASE WHEN ? != '' THEN ? ELSE result END,
	had_error = CASE WHEN ? != 0 THEN 1 ELSE had_error END
WHERE stream_id = ?
`, now, now, result, result, boolToInt(hadError), streamID)
	return err
}

// SetLastSourceSeq stores the absolute agent/source sequence for ACK tracking.
// Unlike Upsert/Touch, this does not auto-increment. If the row is missing it
// inserts a minimal process-origin placeholder so ACK watermarks stay durable.
func (s *Store) SetLastSourceSeq(ctx context.Context, streamID string, seq int64) error {
	if s == nil || s.db == nil {
		return nil
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" || seq < 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE log_contexts SET last_source_seq = ?, updated_at = ? WHERE stream_id = ?
`, seq, now, streamID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO log_contexts (
	stream_id, module, origin, operation_id, execution_id, request_id,
	title, summary, result, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
) VALUES (?, 'agent-evidence', ?, '', ?, '', '', '', '', 0, '', '', ?, '', ?, ?, '')
`, streamID, OriginProcess, streamID, seq, now, now)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanRecord(row scannable) (Record, error) {
	var (
		rec      Record
		hadError int
	)
	err := row.Scan(
		&rec.StreamID, &rec.Module, &rec.Origin, &rec.OperationID, &rec.ExecutionID, &rec.RequestID,
		&rec.Title, &rec.Summary, &rec.Result, &hadError, &rec.ActorJSON, &rec.ResourcesJSON,
		&rec.LastSourceSeq, &rec.LogFile, &rec.StartedAt, &rec.UpdatedAt, &rec.FinishedAt,
	)
	if err != nil {
		return Record{}, err
	}
	rec.HadError = hadError != 0
	return rec, nil
}

func fromLoggingRecord(row logging.ContextRecord) Record {
	return Record{
		StreamID:      strings.TrimSpace(row.StreamID),
		Module:        strings.TrimSpace(row.Module),
		Origin:        strings.TrimSpace(row.Origin),
		OperationID:   strings.TrimSpace(row.OperationID),
		ExecutionID:   strings.TrimSpace(row.ExecutionID),
		RequestID:     strings.TrimSpace(row.RequestID),
		Title:         row.Title,
		Summary:       row.Summary,
		Result:        row.Result,
		HadError:      row.HadError,
		ActorJSON:     row.ActorJSON,
		ResourcesJSON: row.ResourcesJSON,
		LastSourceSeq: row.LastSourceSeq,
		LogFile:       row.LogFile,
		StartedAt:     row.StartedAt,
		UpdatedAt:     row.UpdatedAt,
		FinishedAt:    row.FinishedAt,
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
