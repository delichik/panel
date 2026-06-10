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
	Status          string
	Statuses        []string
	ServerID        string
	Type            string
	Types           []string
	IncludeInternal bool
	OperationID     string
	Limit           int
	Offset          int
}

var defaultHiddenTaskTypes = []string{"server_connectivity_test", "metrics_collect"}

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
	return createTask(ctx, s.db, in)
}

func (s *Service) CreateTx(ctx context.Context, tx *sql.Tx, in CreateInput) (Task, error) {
	return createTask(ctx, tx, in)
}

type taskExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func createTask(ctx context.Context, exec taskExecer, in CreateInput) (Task, error) {
	if strings.TrimSpace(in.Type) == "" {
		return Task{}, panelerr.Validation("task_type_required", "Task type is required")
	}
	now := time.Now().UTC()
	status := in.Status
	if status == "" {
		status = StatusQueued
	}
	t := Task{
		ID:                  id.New("task"),
		OperationID:         firstNonEmpty(in.OperationID, id.New("op")),
		Type:                in.Type,
		ServerID:            in.ServerID,
		NodeID:              firstNonEmpty(in.NodeID, in.ServerID),
		ResourceType:        in.ResourceType,
		ResourceID:          in.ResourceID,
		TriggerType:         in.TriggerType,
		TriggerResourceType: in.TriggerResourceType,
		TriggerResourceID:   in.TriggerResourceID,
		TriggerTaskID:       in.TriggerTaskID,
		TriggeredBy:         in.TriggeredBy,
		Status:              status,
		Summary:             in.Summary,
		RetryCount:          in.RetryCount,
		MaxRetries:          in.MaxRetries,
		NextRunAt:           in.NextRunAt,
		CreatedAt:           now,
	}
	var percentage any
	var startedAt any
	var finishedAt any
	switch status {
	case StatusCompleted:
		done := float64(100)
		t.Percentage = &done
		t.FinishedAt = &now
		percentage = done
		finishedAt = now.Format(time.RFC3339Nano)
		if t.Stage == "" {
			t.Stage = "completed"
		}
	case StatusRunning:
		t.StartedAt = &now
		startedAt = now.Format(time.RFC3339Nano)
	}
	nextRunAt := ""
	if in.NextRunAt != nil {
		nextRunAt = in.NextRunAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO tasks(id,operation_id,type,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,status,stage,percentage,summary,retry_count,max_retries,next_run_at,created_at,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.OperationID, t.Type, t.ServerID, t.NodeID, t.ResourceType, t.ResourceID, t.TriggerType, t.TriggerResourceType, t.TriggerResourceID, t.TriggerTaskID, t.TriggeredBy, t.Status, t.Stage, percentage, t.Summary, t.RetryCount, t.MaxRetries, nullString(nextRunAt), now.Format(time.RFC3339Nano), startedAt, finishedAt)
	return t, err
}

func (s *Service) Start(ctx context.Context, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error='', next_run_at=NULL, percentage=COALESCE(percentage, 0), started_at=COALESCE(started_at, ?), finished_at=NULL WHERE id=?`, StatusRunning, now, taskID)
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
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=?, percentage=100, summary=?, next_run_at=NULL, finished_at=? WHERE id=?`, StatusCompleted, "completed", summary, now, taskID)
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

func (s *Service) FailRetryable(ctx context.Context, taskID string, cause error) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	msg := Redact(cause.Error())
	if task.MaxRetries > 0 && task.RetryCount >= task.MaxRetries {
		return s.Block(ctx, taskID, cause)
	}
	nextRetry := task.RetryCount + 1
	nextRun := time.Now().UTC().Add(backoffDuration(nextRetry))
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, retry_count=?, next_run_at=?, finished_at=NULL WHERE id=?`,
		StatusFailedRetryable, msg, nextRetry, nextRun.Format(time.RFC3339Nano), taskID)
	return err
}

func (s *Service) Block(ctx context.Context, taskID string, cause error) error {
	msg := Redact(cause.Error())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, next_run_at=NULL, finished_at=? WHERE id=?`, StatusBlocked, msg, now, taskID)
	return err
}

func (s *Service) RunNow(ctx context.Context, taskID string) (Task, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, next_run_at=NULL, finished_at=NULL WHERE id=? AND status IN (?,?,?)`,
		StatusQueued, taskID, StatusQueued, StatusScheduled, StatusFailedRetryable)
	if err != nil {
		return Task{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return s.Get(ctx, taskID)
	}
	return s.Get(ctx, taskID)
}

func (s *Service) FirstRunnable(ctx context.Context, taskType, resourceType, resourceID string) (Task, bool, error) {
	args := []any{taskType, resourceType, resourceID, StatusQueued, StatusScheduled, StatusFailedRetryable}
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+`
		FROM tasks
		WHERE type=? AND resource_type=? AND resource_id=? AND status IN (?,?,?)
		  AND (next_run_at IS NULL OR next_run_at='' OR next_run_at<=?)
		ORDER BY created_at ASC LIMIT 1`, append(args, time.Now().UTC().Format(time.RFC3339Nano))...)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) ExistingActive(ctx context.Context, taskType, resourceType, resourceID string) (Task, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+`
		FROM tasks
		WHERE type=? AND resource_type=? AND resource_id=? AND status IN (?,?,?,?)
		ORDER BY created_at DESC LIMIT 1`, taskType, resourceType, resourceID, StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) Get(ctx context.Context, taskID string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id=?`, taskID)
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
	statuses := cleanFilterValues(append(filter.Statuses, filter.Status)...)
	appendEqualOrIn(&conditions, &args, "status", statuses)
	if filter.ServerID != "" {
		conditions = append(conditions, `server_id=?`)
		args = append(args, filter.ServerID)
	}
	types := cleanFilterValues(append(filter.Types, filter.Type)...)
	if len(types) > 0 {
		appendEqualOrIn(&conditions, &args, "type", types)
	} else if !filter.IncludeInternal {
		conditions = append(conditions, `type NOT IN (`+placeholders(len(defaultHiddenTaskTypes))+`)`)
		for _, taskType := range defaultHiddenTaskTypes {
			args = append(args, taskType)
		}
	}
	if filter.OperationID != "" {
		conditions = append(conditions, `operation_id=?`)
		args = append(args, filter.OperationID)
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
	query := `SELECT ` + taskColumns + ` FROM tasks` + where + ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
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

func cleanFilterValues(values ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == "all" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendEqualOrIn(conditions *[]string, args *[]any, column string, values []string) {
	if len(values) == 0 {
		return
	}
	if len(values) == 1 {
		*conditions = append(*conditions, column+`=?`)
		*args = append(*args, values[0])
		return
	}
	*conditions = append(*conditions, column+` IN (`+placeholders(len(values))+`)`)
	for _, value := range values {
		*args = append(*args, value)
	}
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}

func (s *Service) ExpireStaleRunning(ctx context.Context, now time.Time, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	finishedAt := now.UTC().Format(time.RFC3339Nano)
	cutoff := now.UTC().Add(-maxAge).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=CASE WHEN stage='' THEN 'expired' ELSE stage END, error=CASE WHEN error='' THEN ? ELSE error END, next_run_at=NULL, finished_at=? WHERE status=? AND COALESCE(NULLIF(started_at,''), created_at)<=?`,
		StatusFailed, "Task did not report completion before the stale task timeout and was marked failed", finishedAt, StatusRunning, cutoff)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
}

func (s *Service) ExpireStaleQueued(ctx context.Context, now time.Time, maxAge time.Duration, taskTypes []string) (int, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	cleanTypes := make([]string, 0, len(taskTypes))
	seen := map[string]struct{}{}
	for _, taskType := range taskTypes {
		taskType = strings.TrimSpace(taskType)
		if taskType == "" {
			continue
		}
		if _, ok := seen[taskType]; ok {
			continue
		}
		seen[taskType] = struct{}{}
		cleanTypes = append(cleanTypes, taskType)
	}
	if len(cleanTypes) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(cleanTypes))
	args := make([]any, 0, 5+len(cleanTypes))
	for i, taskType := range cleanTypes {
		placeholders[i] = "?"
		args = append(args, taskType)
	}
	finishedAt := now.UTC().Format(time.RFC3339Nano)
	cutoff := now.UTC().Add(-maxAge).Format(time.RFC3339Nano)
	message := "Task stayed queued past the worker startup timeout and was marked failed; retry the operation if it is still needed"
	query := `UPDATE tasks SET status=?, stage=CASE WHEN stage='' THEN 'expired' ELSE stage END, error=CASE WHEN error='' THEN ? ELSE error END, next_run_at=NULL, finished_at=? WHERE status=? AND created_at<=? AND type IN (` + strings.Join(placeholders, ",") + `)`
	updateArgs := []any{StatusFailed, message, finishedAt, StatusQueued, cutoff}
	updateArgs = append(updateArgs, args...)
	res, err := s.db.ExecContext(ctx, query, updateArgs...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
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

func (s *Service) UpsertStep(ctx context.Context, taskID string, in StepInput) (Step, error) {
	if strings.TrimSpace(in.Step) == "" {
		return Step{}, panelerr.Validation("task_step_required", "Task step is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var existingID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM task_steps WHERE task_id=? AND step=?`, taskID, in.Step).Scan(&existingID)
	if err == sql.ErrNoRows {
		existingID = id.New("step")
		started := any(nil)
		finished := any(nil)
		if in.Status == StatusRunning || in.Status == StatusCompleted {
			started = now
		}
		if in.Status == StatusCompleted || in.Status == StatusFailed || in.Status == StatusCancelled {
			finished = now
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO task_steps(id,task_id,step,status,percentage,metadata_json,started_at,finished_at,error) VALUES(?,?,?,?,?,?,?,?,?)`,
			existingID, taskID, in.Step, in.Status, in.Percentage, in.MetadataJSON, started, finished, in.Error)
		if err != nil {
			return Step{}, err
		}
		return s.step(ctx, existingID)
	}
	if err != nil {
		return Step{}, err
	}
	assignments := `status=?,percentage=?,metadata_json=?,error=?`
	args := []any{in.Status, in.Percentage, in.MetadataJSON, in.Error}
	if in.Status == StatusRunning {
		assignments += `,started_at=COALESCE(started_at,?)`
		args = append(args, now)
	}
	if in.Status == StatusCompleted || in.Status == StatusFailed || in.Status == StatusCancelled {
		assignments += `,finished_at=?`
		args = append(args, now)
	}
	args = append(args, existingID)
	if _, err := s.db.ExecContext(ctx, `UPDATE task_steps SET `+assignments+` WHERE id=?`, args...); err != nil {
		return Step{}, err
	}
	return s.step(ctx, existingID)
}

func (s *Service) Steps(ctx context.Context, taskID string) ([]Step, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,task_id,step,status,percentage,metadata_json,started_at,finished_at,error FROM task_steps WHERE task_id=? ORDER BY id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Step{}
	for rows.Next() {
		step, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func (s *Service) Retry(ctx context.Context, taskID string) (Task, error) {
	old, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	return s.Create(ctx, CreateInput{
		OperationID:         old.OperationID,
		Type:                old.Type,
		ServerID:            old.ServerID,
		NodeID:              old.NodeID,
		ResourceType:        old.ResourceType,
		ResourceID:          old.ResourceID,
		TriggerType:         "retry",
		TriggerResourceType: old.ResourceType,
		TriggerResourceID:   old.ResourceID,
		TriggerTaskID:       old.ID,
		Summary:             "Retrying " + old.Summary,
		MaxRetries:          old.MaxRetries,
	})
}

type scanner interface{ Scan(dest ...any) error }

const taskColumns = `id,operation_id,type,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,status,stage,percentage,summary,error,retry_count,max_retries,next_run_at,created_at,started_at,finished_at`

func scanTask(row scanner) (Task, error) {
	var t Task
	var pct sql.NullFloat64
	var created string
	var startedNS, finishedNS, nextRunNS sql.NullString
	err := row.Scan(&t.ID, &t.OperationID, &t.Type, &t.ServerID, &t.NodeID, &t.ResourceType, &t.ResourceID, &t.TriggerType, &t.TriggerResourceType, &t.TriggerResourceID, &t.TriggerTaskID, &t.TriggeredBy, &t.Status, &t.Stage, &pct, &t.Summary, &t.Error, &t.RetryCount, &t.MaxRetries, &nextRunNS, &created, &startedNS, &finishedNS)
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if pct.Valid {
		t.Percentage = &pct.Float64
	} else if t.Status == StatusCompleted {
		done := float64(100)
		t.Percentage = &done
	}
	if nextRunNS.Valid {
		v, _ := time.Parse(time.RFC3339Nano, nextRunNS.String)
		t.NextRunAt = &v
	}
	if startedNS.Valid {
		v, _ := time.Parse(time.RFC3339Nano, startedNS.String)
		t.StartedAt = &v
	}
	if finishedNS.Valid {
		v, _ := time.Parse(time.RFC3339Nano, finishedNS.String)
		t.FinishedAt = &v
	}
	return t, nil
}

func (s *Service) step(ctx context.Context, stepID string) (Step, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,task_id,step,status,percentage,metadata_json,started_at,finished_at,error FROM task_steps WHERE id=?`, stepID)
	return scanStep(row)
}

func scanStep(row scanner) (Step, error) {
	var step Step
	var started, finished sql.NullString
	if err := row.Scan(&step.ID, &step.TaskID, &step.Step, &step.Status, &step.Percentage, &step.MetadataJSON, &started, &finished, &step.Error); err != nil {
		return Step{}, err
	}
	if started.Valid {
		v, _ := time.Parse(time.RFC3339Nano, started.String)
		step.StartedAt = &v
	}
	if finished.Valid {
		v, _ := time.Parse(time.RFC3339Nano, finished.String)
		step.FinishedAt = &v
	}
	return step, nil
}

func backoffDuration(retryCount int) time.Duration {
	if retryCount <= 1 {
		return 30 * time.Second
	}
	delay := 30 * time.Second
	for i := 1; i < retryCount; i++ {
		delay *= 2
		if delay >= 10*time.Minute {
			return 10 * time.Minute
		}
	}
	return delay
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func Redact(s string) string {
	replacers := []string{"password=", "privateKey=", "passphrase="}
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
