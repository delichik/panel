package events

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/platform/logging"
)

// Store persists events rows in AppDB.
type Store struct {
	db *sql.DB
}

// NewStore wraps an AppDB connection. db may be nil only in tests that never call methods.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

var _ logging.ContextStore = (*Store)(nil)

// Upsert inserts or merges a diagnostic events row by id (ENG-RUN-003).
// It only writes the AppDB `events` index — never `jobs` / `event_runs` lease,
// status, attempts, or fence execution_id. events.execution_id is a correlation
// pointer only, not the Run fence. Once a diagnostic row has finished_at set,
// later log upserts must not reopen state to the default "running".
func (s *Store) Upsert(ctx context.Context, row logging.ContextRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	rec := fromLoggingRecord(row)
	if strings.TrimSpace(rec.ID) == "" || strings.TrimSpace(rec.Module) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if rec.Origin == "" {
		rec.Origin = OriginProcess
	}
	if rec.State == "" {
		rec.State = "running"
	}
	if rec.Level == "" {
		if rec.HadError {
			rec.Level = "error"
		} else {
			rec.Level = "info"
		}
	}
	if rec.StartedAt == "" {
		rec.StartedAt = now
	}
	if rec.UpdatedAt == "" {
		rec.UpdatedAt = now
	}
	seq := rec.LastSourceSeq
	if seq < 0 {
		seq = 0
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (
	id, module, level, origin, operation_id, execution_id, request_id,
	title, summary, state, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	module=excluded.module,
	level=CASE WHEN excluded.level != '' THEN excluded.level ELSE events.level END,
	origin=CASE WHEN excluded.origin != '' THEN excluded.origin ELSE events.origin END,
	operation_id=CASE WHEN excluded.operation_id != '' THEN excluded.operation_id ELSE events.operation_id END,
	execution_id=CASE WHEN excluded.execution_id != '' THEN excluded.execution_id ELSE events.execution_id END,
	request_id=CASE WHEN excluded.request_id != '' THEN excluded.request_id ELSE events.request_id END,
	title=CASE WHEN excluded.title != '' THEN excluded.title ELSE events.title END,
	summary=CASE WHEN excluded.summary != '' THEN excluded.summary ELSE events.summary END,
	state=CASE
		WHEN events.finished_at IS NOT NULL AND events.finished_at != '' THEN events.state
		WHEN excluded.state != '' THEN excluded.state
		ELSE events.state
	END,
	had_error=CASE WHEN excluded.had_error != 0 THEN 1 ELSE events.had_error END,
	actor_json=CASE WHEN excluded.actor_json != '' THEN excluded.actor_json ELSE events.actor_json END,
	resources_json=CASE WHEN excluded.resources_json != '' THEN excluded.resources_json ELSE events.resources_json END,
	log_file=CASE WHEN excluded.log_file != '' THEN excluded.log_file ELSE events.log_file END,
	updated_at=excluded.updated_at,
	finished_at=CASE WHEN excluded.finished_at != '' THEN excluded.finished_at ELSE events.finished_at END
`,
		rec.ID, rec.Module, rec.Level, rec.Origin, rec.OperationID, rec.ExecutionID, rec.RequestID,
		rec.Title, rec.Summary, rec.State, boolToInt(rec.HadError), rec.ActorJSON, rec.ResourcesJSON,
		seq, rec.LogFile, rec.StartedAt, rec.UpdatedAt, rec.FinishedAt,
	)
	return err
}

// Touch bumps updated_at / summary / level and optionally marks had_error.
func (s *Store) Touch(ctx context.Context, eventID string, level string, msg string) error {
	if s == nil || s.db == nil {
		return nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hadError := 0
	lvl := strings.ToLower(strings.TrimSpace(level))
	switch lvl {
	case "error", "fatal", "panic", "dpanic":
		hadError = 1
	}
	summary := strings.TrimSpace(msg)
	if len(summary) > 512 {
		summary = summary[:512]
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE events SET
	updated_at = ?,
	level = CASE WHEN ? != '' THEN ? ELSE level END,
	had_error = CASE WHEN ? != 0 THEN 1 ELSE had_error END,
	summary = CASE WHEN ? != '' THEN ? ELSE summary END
WHERE id = ?
`, now, lvl, lvl, hadError, summary, summary, eventID)
	return err
}

// Get returns one row by id.
func (s *Store) Get(ctx context.Context, eventID string) (Record, error) {
	var rec Record
	if s == nil || s.db == nil {
		return rec, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, module, level, origin, operation_id, execution_id, request_id,
	title, summary, state, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
FROM events WHERE id = ?
`, strings.TrimSpace(eventID))
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
	if v := strings.TrimSpace(filter.State); v != "" {
		conds = append(conds, "state = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(filter.Level); v != "" {
		conds = append(conds, "level = ?")
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
SELECT id, module, level, origin, operation_id, execution_id, request_id,
	title, summary, state, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
FROM events`
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

// UpdateFinished marks an event finished with optional state text.
func (s *Store) UpdateFinished(ctx context.Context, eventID, state string, hadError bool) error {
	if s == nil || s.db == nil {
		return nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(state) == "" {
		if hadError {
			state = "failed"
		} else {
			state = "succeeded"
		}
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE events SET
	finished_at = ?,
	updated_at = ?,
	state = ?,
	had_error = CASE WHEN ? != 0 THEN 1 ELSE had_error END
WHERE id = ?
`, now, now, state, boolToInt(hadError), eventID)
	return err
}

// SetLastSourceSeq stores the absolute agent/source sequence for ACK tracking.
func (s *Store) SetLastSourceSeq(ctx context.Context, eventID string, seq int64) error {
	if s == nil || s.db == nil {
		return nil
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" || seq < 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE events SET last_source_seq = ?, updated_at = ? WHERE id = ?
`, seq, now, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO events (
	id, module, level, origin, operation_id, execution_id, request_id,
	title, summary, state, had_error, actor_json, resources_json,
	last_source_seq, log_file, started_at, updated_at, finished_at
) VALUES (?, 'agent-evidence', 'info', ?, '', ?, '', '', '', 'running', 0, '', '', ?, '', ?, ?, '')
`, eventID, OriginProcess, eventID, seq, now, now)
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
		&rec.ID, &rec.Module, &rec.Level, &rec.Origin, &rec.OperationID, &rec.ExecutionID, &rec.RequestID,
		&rec.Title, &rec.Summary, &rec.State, &hadError, &rec.ActorJSON, &rec.ResourcesJSON,
		&rec.LastSourceSeq, &rec.LogFile, &rec.StartedAt, &rec.UpdatedAt, &rec.FinishedAt,
	)
	if err != nil {
		return Record{}, err
	}
	rec.HadError = hadError != 0
	return rec, nil
}

func fromLoggingRecord(row logging.ContextRecord) Record {
	id := strings.TrimSpace(row.EventID)
	return Record{
		ID:            id,
		Module:        strings.TrimSpace(row.Module),
		Level:         strings.TrimSpace(row.Level),
		Origin:        strings.TrimSpace(row.Origin),
		OperationID:   strings.TrimSpace(row.OperationID),
		ExecutionID:   strings.TrimSpace(row.ExecutionID),
		RequestID:     strings.TrimSpace(row.RequestID),
		Title:         row.Title,
		Summary:       row.Summary,
		State:         row.State,
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
