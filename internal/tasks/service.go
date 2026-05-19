package tasks

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/id"
	"panel/internal/panelerr"
)

type Service struct {
	db *sql.DB
}

type ListFilter struct {
	Status   string
	ServerID string
	Type     string
	Limit    int
	Offset   int
}

type ListResult struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Task, error) {
	if strings.TrimSpace(in.Type) == "" {
		return Task{}, panelerr.Validation("task_type_required", "Task type is required")
	}
	now := time.Now().UTC()
	t := Task{ID: id.New("task"), Type: in.Type, ServerID: in.ServerID, Status: StatusQueued, Summary: in.Summary, CreatedAt: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tasks(id,type,server_id,status,stage,summary,created_at) VALUES(?,?,?,?,?,?,?)`, t.ID, t.Type, t.ServerID, t.Status, t.Stage, t.Summary, now.Format(time.RFC3339Nano))
	return t, err
}

func (s *Service) Start(ctx context.Context, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, started_at=? WHERE id=?`, StatusRunning, now, taskID)
	return err
}

func (s *Service) Advance(ctx context.Context, taskID, stage, message string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET stage=? WHERE id=?`, stage, taskID); err != nil {
		return err
	}
	if message != "" {
		return s.AppendLog(ctx, taskID, "system", message)
	}
	return nil
}

func (s *Service) AppendLog(ctx context.Context, taskID, stream, line string) error {
	line = Redact(line)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_logs(task_id,time,stream,line) VALUES(?,?,?,?)`, taskID, now, stream, line)
	return err
}

func (s *Service) Complete(ctx context.Context, taskID, summary string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=?, summary=?, finished_at=? WHERE id=?`, StatusCompleted, "finalizing", summary, now, taskID)
	return err
}

func (s *Service) Fail(ctx context.Context, taskID string, err error) error {
	msg := Redact(err.Error())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	_, updateErr := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, finished_at=? WHERE id=?`, StatusFailed, msg, now, taskID)
	return updateErr
}

func (s *Service) Get(ctx context.Context, taskID string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,type,server_id,status,stage,percentage,summary,error,created_at,started_at,finished_at FROM tasks WHERE id=?`, taskID)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, panelerr.NotFound("task")
	}
	return task, err
}

func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	args := []any{}
	conditions := []string{}
	if filter.Status != "" {
		conditions = append(conditions, `status=?`)
		args = append(args, filter.Status)
	}
	if filter.ServerID != "" {
		conditions = append(conditions, `server_id=?`)
		args = append(args, filter.ServerID)
	}
	if filter.Type != "" {
		conditions = append(conditions, `type=?`)
		args = append(args, filter.Type)
	}
	where := ""
	if len(conditions) > 0 {
		where = ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	countQuery := `SELECT COUNT(*) FROM tasks` + where
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}
	query := `SELECT id,type,server_id,status,stage,percentage,summary,error,created_at,started_at,finished_at FROM tasks` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return ListResult{}, err
		}
		out = append(out, task)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: out, Total: total, PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
}

func (s *Service) Logs(ctx context.Context, taskID string, after int64) ([]Log, int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,time,stream,line FROM task_logs WHERE task_id=? AND id>? ORDER BY id ASC LIMIT 200`, taskID, after)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	logs := []Log{}
	next := after
	for rows.Next() {
		var l Log
		var ts string
		if err := rows.Scan(&l.Cursor, &ts, &l.Stream, &l.Line); err != nil {
			return nil, after, err
		}
		l.Time, _ = time.Parse(time.RFC3339Nano, ts)
		next = l.Cursor
		logs = append(logs, l)
	}
	return logs, next, rows.Err()
}

type scanner interface{ Scan(dest ...any) error }

func scanTask(row scanner) (Task, error) {
	var t Task
	var pct sql.NullFloat64
	var created, started, finished string
	var startedNS, finishedNS sql.NullString
	err := row.Scan(&t.ID, &t.Type, &t.ServerID, &t.Status, &t.Stage, &pct, &t.Summary, &t.Error, &created, &startedNS, &finishedNS)
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	started = startedNS.String
	finished = finishedNS.String
	if pct.Valid {
		t.Percentage = &pct.Float64
	}
	if startedNS.Valid {
		v, _ := time.Parse(time.RFC3339Nano, started)
		t.StartedAt = &v
	}
	if finishedNS.Valid {
		v, _ := time.Parse(time.RFC3339Nano, finished)
		t.FinishedAt = &v
	}
	return t, nil
}

func Redact(s string) string {
	replacers := []string{"password=", "privateKey=", "passphrase=", "PANEL_SESSION_SECRET="}
	for _, r := range replacers {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(r))
		if idx >= 0 {
			end := strings.IndexAny(s[idx+len(r):], " \n\r\t")
			if end < 0 {
				s = s[:idx+len(r)] + "[REDACTED]"
			} else {
				pos := idx + len(r) + end
				s = s[:idx+len(r)] + "[REDACTED]" + s[pos:]
			}
		}
	}
	return s
}
