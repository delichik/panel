package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"panel/internal/id"
	"panel/internal/panelerr"
)

type Service struct {
	db                *sql.DB
	registry          *Registry
	runningMu         sync.Mutex
	runningExecutions map[string]*RunningExecution
}

type ListFilter struct {
	Status           string
	Statuses         []string
	ServerID         string
	Type             string
	Types            []string
	IncludeInternal  bool
	ExcludeScheduled bool
	OperationID      string
	Limit            int
	Offset           int
}

var defaultHiddenTaskTypes = []string{"server_connectivity_test", "metrics_collect"}

var terminalStatuses = []string{StatusCompleted, StatusFailed, StatusBlocked, StatusCancelled}

type ListResult struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

func NewService(db *sql.DB) *Service {
	s := &Service{db: db, registry: NewRegistry(), runningExecutions: map[string]*RunningExecution{}}
	RegisterKnownTaskTypes(s)
	return s
}

func (s *Service) Registry() *Registry {
	if s.registry == nil {
		s.registry = NewRegistry()
	}
	return s.registry
}

func (s *Service) Register(def Definition) error {
	return s.Registry().Register(def)
}

func (s *Service) MustRegister(def Definition) {
	s.Registry().MustRegister(def)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Task, error) {
	return s.create(ctx, in, true)
}

func (s *Service) create(ctx context.Context, in CreateInput, validate bool) (Task, error) {
	def, ok := s.Registry().Definition(in.Type)
	if !ok {
		return Task{}, panelerr.Validation("task_type_unregistered", "Task type is not registered")
	}
	if validate && def.Validate != nil {
		if err := def.Validate(ctx, in); err != nil {
			return Task{}, err
		}
	}
	if in.MaxRetries == 0 && def.DefaultMaxRetries > 0 {
		in.MaxRetries = def.DefaultMaxRetries
	}
	in.ConcurrencyKey = ConcurrencyKeyFor(def, in)
	if in.ExecutionMode == "" {
		in.ExecutionMode = ExecutionModeSingle
	}
	if strings.TrimSpace(in.ParamsJSON) == "" {
		in.ParamsJSON = "{}"
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	if in.Status == StatusRunning {
		s.runningMu.Lock()
		defer s.runningMu.Unlock()
	}
	var registeredTaskID string
	beforeInsert := func(task Task) {
		if task.Status == StatusRunning {
			registeredTaskID = task.ID
			s.registerRunningExecutionLocked(task.ID)
		}
	}
	task, err := createTask(ctx, s.db, in, beforeInsert)
	if err != nil && registeredTaskID != "" {
		s.unregisterRunningExecutionLocked(registeredTaskID)
	}
	return task, err
}

func (s *Service) CreateTx(ctx context.Context, tx *sql.Tx, in CreateInput) (Task, error) {
	return s.createTx(ctx, tx, in, true)
}

func (s *Service) createTx(ctx context.Context, tx *sql.Tx, in CreateInput, validate bool) (Task, error) {
	def, ok := s.Registry().Definition(in.Type)
	if !ok {
		return Task{}, panelerr.Validation("task_type_unregistered", "Task type is not registered")
	}
	if validate && def.Validate != nil {
		if err := def.Validate(ctx, in); err != nil {
			return Task{}, err
		}
	}
	if in.MaxRetries == 0 && def.DefaultMaxRetries > 0 {
		in.MaxRetries = def.DefaultMaxRetries
	}
	in.ConcurrencyKey = ConcurrencyKeyFor(def, in)
	if in.ExecutionMode == "" {
		in.ExecutionMode = ExecutionModeSingle
	}
	if strings.TrimSpace(in.ParamsJSON) == "" {
		in.ParamsJSON = "{}"
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	if in.Status == StatusRunning {
		return Task{}, errors.New("running tasks cannot be created inside a transaction")
	}
	return createTask(ctx, tx, in, nil)
}

type taskExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func createTask(ctx context.Context, exec taskExecer, in CreateInput, beforeInsert func(Task)) (Task, error) {
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
		ParentTaskID:        in.ParentTaskID,
		ChildIndex:          in.ChildIndex,
		ChildCount:          in.ChildCount,
		ExecutionMode:       in.ExecutionMode,
		ConcurrencyKey:      in.ConcurrencyKey,
		ScheduleKey:         in.ScheduleKey,
		ServerID:            in.ServerID,
		NodeID:              firstNonEmpty(in.NodeID, in.ServerID),
		ResourceType:        in.ResourceType,
		ResourceID:          in.ResourceID,
		TriggerType:         in.TriggerType,
		TriggerResourceType: in.TriggerResourceType,
		TriggerResourceID:   in.TriggerResourceID,
		TriggerTaskID:       in.TriggerTaskID,
		TriggeredBy:         in.TriggeredBy,
		ParamsJSON:          firstNonEmpty(strings.TrimSpace(in.ParamsJSON), "{}"),
		MetadataJSON:        firstNonEmpty(strings.TrimSpace(in.MetadataJSON), "{}"),
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
	if beforeInsert != nil {
		beforeInsert(t)
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO tasks(id,operation_id,type,parent_task_id,child_index,child_count,execution_mode,concurrency_key,schedule_key,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,params_json,metadata_json,status,stage,percentage,summary,retry_count,max_retries,next_run_at,created_at,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.OperationID, t.Type, t.ParentTaskID, t.ChildIndex, t.ChildCount, t.ExecutionMode, t.ConcurrencyKey, t.ScheduleKey, t.ServerID, t.NodeID, t.ResourceType, t.ResourceID, t.TriggerType, t.TriggerResourceType, t.TriggerResourceID, t.TriggerTaskID, t.TriggeredBy, t.ParamsJSON, t.MetadataJSON, t.Status, t.Stage, percentage, t.Summary, t.RetryCount, t.MaxRetries, nullString(nextRunAt), now.Format(time.RFC3339Nano), startedAt, finishedAt)
	return t, err
}

func (s *Service) Start(ctx context.Context, taskID string) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	s.registerRunningExecutionLocked(taskID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error='', next_run_at=NULL, percentage=COALESCE(percentage, 0), started_at=COALESCE(started_at, ?), finished_at=NULL WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusRunning, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err != nil {
		s.unregisterRunningExecutionLocked(taskID)
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		s.unregisterRunningExecutionLocked(taskID)
		return panelerr.Conflict("task_not_runnable", "Task is already finished")
	}
	return nil
}

func (s *Service) Advance(ctx context.Context, taskID, stage, message string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET stage=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{stage, taskID}, stringArgs(terminalStatuses)...)...)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if message != "" {
		if affected == 0 {
			return nil
		}
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
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=?, percentage=100, summary=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusCompleted, "completed", summary, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
		}
	}
	return err
}

func (s *Service) Fail(ctx context.Context, taskID string, err error) error {
	msg := Redact(err.Error())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, updateErr := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusFailed, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
	if updateErr == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
		}
	}
	return updateErr
}

func (s *Service) FailRetryable(ctx context.Context, taskID string, cause error) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if isTerminalStatus(task.Status) {
		return nil
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
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, retry_count=?, next_run_at=?, finished_at=NULL WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
		append([]any{StatusFailedRetryable, msg, nextRetry, nextRun.Format(time.RFC3339Nano), taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
		}
	}
	return err
}

func (s *Service) Block(ctx context.Context, taskID string, cause error) error {
	msg := Redact(cause.Error())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusBlocked, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
		}
	}
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

func (s *Service) Children(ctx context.Context, parentTaskID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskColumns+`
		FROM tasks
		WHERE parent_task_id=?
		ORDER BY child_index ASC, created_at ASC`, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	children := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		children = append(children, task)
	}
	return children, rows.Err()
}

func (s *Service) ExistingActiveByConcurrencyKey(ctx context.Context, concurrencyKey string) (Task, bool, error) {
	concurrencyKey = strings.TrimSpace(concurrencyKey)
	if concurrencyKey == "" {
		return Task{}, false, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+taskColumns+`
		FROM tasks
		WHERE concurrency_key=? AND status IN (?,?,?,?)
		ORDER BY created_at DESC LIMIT 1`, concurrencyKey, StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable)
	task, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) CountByServerStatuses(ctx context.Context, serverID string, statuses ...string) (int, error) {
	statuses = cleanFilterValues(statuses...)
	if len(statuses) == 0 {
		return 0, nil
	}
	args := []any{serverID}
	for _, status := range statuses {
		args = append(args, status)
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE server_id=? AND status IN (`+placeholders(len(statuses))+`)`, args...).Scan(&count)
	return count, err
}

func (s *Service) CancelByServer(ctx context.Context, serverID, message string) (int, error) {
	activeStatuses := []string{StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable}
	args := append([]any{serverID}, stringArgs(activeStatuses)...)
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE server_id=? AND status IN (`+placeholders(len(activeStatuses))+`)`, args...)
	if err != nil {
		return 0, err
	}
	taskIDs := []string{}
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(taskIDs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(message) == "" {
		message = "Task cancelled because the server was removed"
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	updateArgs := []any{StatusCancelled, "cancelled", Redact(message), finishedAt, serverID}
	updateArgs = append(updateArgs, stringArgs(activeStatuses)...)
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=?, error=?, next_run_at=NULL, finished_at=? WHERE server_id=? AND status IN (`+placeholders(len(activeStatuses))+`)`, updateArgs...)
	if err != nil {
		return 0, err
	}
	s.runningMu.Lock()
	for _, taskID := range taskIDs {
		if execution, ok := s.runningExecutions[taskID]; ok && execution.Cancel != nil {
			execution.Cancel()
		}
		delete(s.runningExecutions, taskID)
	}
	s.runningMu.Unlock()
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
}

func (s *Service) Cancel(ctx context.Context, taskID, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "Task cancelled"
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
		append([]any{StatusCancelled, "cancelled", Redact(message), finishedAt, taskID}, stringArgs(terminalStatuses)...)...)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil
	}
	s.runningMu.Lock()
	if execution, ok := s.runningExecutions[taskID]; ok && execution.Cancel != nil {
		execution.Cancel()
	}
	delete(s.runningExecutions, taskID)
	s.runningMu.Unlock()
	return nil
}

func (s *Service) SetTriggeredBy(ctx context.Context, taskID, triggeredBy string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET triggered_by=? WHERE id=?`, triggeredBy, taskID)
	return err
}

func (s *Service) CountFailuresSinceLastSuccess(ctx context.Context, taskType, resourceType, resourceID string, failureStatuses []string, excludeTriggeredBy string) (int, error) {
	var lastSuccess sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM tasks WHERE type=? AND resource_type=? AND resource_id=? AND status=?`,
		taskType, resourceType, resourceID, StatusCompleted).Scan(&lastSuccess); err != nil {
		return 0, err
	}
	failureStatuses = cleanFilterValues(failureStatuses...)
	if len(failureStatuses) == 0 {
		return 0, nil
	}
	args := []any{taskType, resourceType, resourceID}
	where := `type=? AND resource_type=? AND resource_id=? AND status IN (` + placeholders(len(failureStatuses)) + `)`
	for _, status := range failureStatuses {
		args = append(args, status)
	}
	if strings.TrimSpace(excludeTriggeredBy) != "" {
		where += ` AND COALESCE(triggered_by,'') <> ?`
		args = append(args, excludeTriggeredBy)
	}
	if lastSuccess.Valid && strings.TrimSpace(lastSuccess.String) != "" {
		where += ` AND created_at > ?`
		args = append(args, lastSuccess.String)
	}
	var failures int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE `+where, args...).Scan(&failures); err != nil {
		return 0, err
	}
	return failures, nil
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
		hidden := s.hiddenTaskTypes()
		if len(hidden) > 0 {
			conditions = append(conditions, `type NOT IN (`+placeholders(len(hidden))+`)`)
		}
		for _, taskType := range hidden {
			args = append(args, taskType)
		}
	}
	if filter.ExcludeScheduled && len(types) == 0 {
		conditions = append(conditions, `(trigger_type='' OR trigger_type<>?)`)
		args = append(args, "scheduler")
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

func (s *Service) hiddenTaskTypes() []string {
	hidden := append([]string{}, defaultHiddenTaskTypes...)
	if s.registry == nil {
		return hidden
	}
	for _, taskType := range s.registry.Types() {
		def, ok := s.registry.Definition(taskType)
		if ok && def.Hidden {
			hidden = append(hidden, taskType)
		}
	}
	return cleanFilterValues(hidden...)
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

func stringArgs(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func isTerminalStatus(status string) bool {
	for _, terminal := range terminalStatuses {
		if status == terminal {
			return true
		}
	}
	return false
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

func (s *Service) FailRunningWithoutExecution(ctx context.Context, now time.Time) (int, error) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE status=?`, StatusRunning)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	taskIDs := []string{}
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return 0, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	failed := 0
	finishedAt := now.UTC().Format(time.RFC3339Nano)
	const message = "Task was marked running but no active execution exists in this Panel process"
	for _, taskID := range taskIDs {
		if _, ok := s.runningExecutions[taskID]; ok {
			continue
		}
		res, err := s.db.ExecContext(ctx, `UPDATE tasks SET status=?, stage=CASE WHEN stage='' THEN 'orphaned' ELSE stage END, error=CASE WHEN error='' THEN ? ELSE error END, next_run_at=NULL, finished_at=? WHERE id=? AND status=?`,
			StatusFailed, message, finishedAt, taskID, StatusRunning)
		if err != nil {
			return failed, err
		}
		affected, err := res.RowsAffected()
		if err == nil {
			failed += int(affected)
		}
	}
	return failed, nil
}

func (s *Service) HasRunningExecution(taskID string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	_, ok := s.runningExecutions[taskID]
	return ok
}

func (s *Service) ExecutionContext(taskID string) context.Context {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if execution, ok := s.runningExecutions[taskID]; ok && execution.Context != nil {
		return execution.Context
	}
	return context.Background()
}

func (s *Service) FinishExecution(taskID string) {
	var status string
	err := s.db.QueryRowContext(context.Background(), `SELECT status FROM tasks WHERE id=?`, taskID).Scan(&status)
	if err != nil && err != sql.ErrNoRows {
		return
	}
	if err == nil && status == StatusRunning {
		return
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	s.unregisterRunningExecutionLocked(taskID)
}

func (s *Service) registerRunningExecutionLocked(taskID string) *RunningExecution {
	if existing, ok := s.runningExecutions[taskID]; ok {
		return existing
	}
	ctx, cancel := context.WithCancel(context.Background())
	execution := &RunningExecution{TaskID: taskID, StartedAt: time.Now().UTC(), Context: ctx, Cancel: cancel}
	s.runningExecutions[taskID] = execution
	return execution
}

func (s *Service) unregisterRunningExecutionLocked(taskID string) {
	delete(s.runningExecutions, taskID)
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
		MetadataJSON:        old.MetadataJSON,
		Summary:             "Retrying " + old.Summary,
		MaxRetries:          old.MaxRetries,
	})
}

type scanner interface{ Scan(dest ...any) error }

const taskColumns = `id,operation_id,type,parent_task_id,child_index,child_count,execution_mode,concurrency_key,schedule_key,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,params_json,metadata_json,status,stage,percentage,summary,error,retry_count,max_retries,next_run_at,created_at,started_at,finished_at`

func scanTask(row scanner) (Task, error) {
	var t Task
	var pct sql.NullFloat64
	var created string
	var startedNS, finishedNS, nextRunNS sql.NullString
	err := row.Scan(&t.ID, &t.OperationID, &t.Type, &t.ParentTaskID, &t.ChildIndex, &t.ChildCount, &t.ExecutionMode, &t.ConcurrencyKey, &t.ScheduleKey, &t.ServerID, &t.NodeID, &t.ResourceType, &t.ResourceID, &t.TriggerType, &t.TriggerResourceType, &t.TriggerResourceID, &t.TriggerTaskID, &t.TriggeredBy, &t.ParamsJSON, &t.MetadataJSON, &t.Status, &t.Stage, &pct, &t.Summary, &t.Error, &t.RetryCount, &t.MaxRetries, &nextRunNS, &created, &startedNS, &finishedNS)
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
