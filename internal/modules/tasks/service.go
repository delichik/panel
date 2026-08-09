package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/modules/runtimeevents"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Service struct {
	db                *sql.DB
	registry          *Registry
	runningMu         sync.Mutex
	runningExecutions map[string]*RunningExecution
	events            runtimeevents.EventWriter
	queueMu           sync.RWMutex
	firstActiveByKey  map[string]string
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
	OperationPage    bool
	Limit            int
	Offset           int
}

var terminalStatuses = []string{StatusCompleted, StatusFailed, StatusBlocked, StatusCancelled}

type ListResult struct {
	Items    []Task `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, registry: NewRegistry(), runningExecutions: map[string]*RunningExecution{}, firstActiveByKey: map[string]string{}}
}

func (s *Service) SetRuntimeEvents(events runtimeevents.EventWriter) {
	s.events = events
}

// invalidateFirstActiveByKey removes the cached first active task for a
// concurrency key. It is safe to call for keys that are not cached.
func (s *Service) invalidateFirstActiveByKey(concurrencyKey string) {
	concurrencyKey = strings.TrimSpace(concurrencyKey)
	if concurrencyKey == "" {
		return
	}
	s.queueMu.Lock()
	delete(s.firstActiveByKey, concurrencyKey)
	s.queueMu.Unlock()
}

// invalidateAllFirstActiveKeys clears the whole first-active cache. It is used
// by bulk raw-SQL transitions that cannot know which concurrency keys changed.
func (s *Service) invalidateAllFirstActiveKeys() {
	s.queueMu.Lock()
	clear(s.firstActiveByKey)
	s.queueMu.Unlock()
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
	if err == nil {
		err = s.writeTaskEvent(ctx, runtimeevents.EventTaskCreated, task, task.Summary, runtimeevents.SeverityInfo)
		if err == nil && task.Status == StatusRunning {
			err = s.writeTaskEvent(ctx, runtimeevents.EventTaskStarted, task, task.Summary, runtimeevents.SeverityInfo)
		}
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

func createTask(ctx context.Context, exec orm.Executor, in CreateInput, beforeInsert func(Task)) (Task, error) {
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
	switch status {
	case StatusCompleted:
		done := float64(100)
		t.Percentage = &done
		t.FinishedAt = &now
		if t.Stage == "" {
			t.Stage = "completed"
		}
	case StatusRunning:
		t.StartedAt = &now
	}
	if beforeInsert != nil {
		beforeInsert(t)
	}
	err := orm.New(exec).From("tasks").Insert(ctx, &taskRow{
		ID:                  t.ID,
		OperationID:         t.OperationID,
		Type:                t.Type,
		ParentTaskID:        t.ParentTaskID,
		ChildIndex:          t.ChildIndex,
		ChildCount:          t.ChildCount,
		ExecutionMode:       t.ExecutionMode,
		ConcurrencyKey:      t.ConcurrencyKey,
		ScheduleKey:         t.ScheduleKey,
		ServerID:            t.ServerID,
		NodeID:              t.NodeID,
		ResourceType:        t.ResourceType,
		ResourceID:          t.ResourceID,
		TriggerType:         t.TriggerType,
		TriggerResourceType: t.TriggerResourceType,
		TriggerResourceID:   t.TriggerResourceID,
		TriggerTaskID:       t.TriggerTaskID,
		TriggeredBy:         t.TriggeredBy,
		ParamsJSON:          t.ParamsJSON,
		MetadataJSON:        t.MetadataJSON,
		Status:              t.Status,
		Stage:               t.Stage,
		Percentage:          t.Percentage,
		Summary:             t.Summary,
		Error:               t.Error,
		RetryCount:          t.RetryCount,
		MaxRetries:          t.MaxRetries,
		NextRunAt:           t.NextRunAt,
		CreatedAt:           now,
		StartedAt:           t.StartedAt,
		FinishedAt:          t.FinishedAt,
	})
	return t, err
}

func (s *Service) Start(ctx context.Context, taskID string) error {
	_, err := s.startExecution(ctx, taskID)
	return err
}

func (s *Service) claimExecution(ctx context.Context, taskID string) (bool, error) {
	return s.startExecution(ctx, taskID)
}

func (s *Service) startExecution(ctx context.Context, taskID string) (bool, error) {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if _, exists := s.runningExecutions[taskID]; exists {
		return false, nil
	}
	s.registerRunningExecutionLocked(taskID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error='', next_run_at=NULL, percentage=COALESCE(percentage, 0), started_at=COALESCE(started_at, ?), finished_at=NULL WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusRunning, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err != nil {
		s.unregisterRunningExecutionLocked(taskID)
		return false, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		s.unregisterRunningExecutionLocked(taskID)
		return false, panelerr.Conflict("task_not_runnable", "Task is already finished")
	}
	if task, getErr := s.Get(ctx, taskID); getErr == nil {
		if err := s.writeTaskEvent(ctx, runtimeevents.EventTaskStarted, task, task.Summary, runtimeevents.SeverityInfo); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) Advance(ctx context.Context, taskID, stage, message string) error {
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET stage=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{stage, taskID}, stringArgs(terminalStatuses)...)...)
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
	log := models.TaskLog{TaskID: taskID, Time: time.Now().UTC(), Stream: stream, Line: line}
	err := orm.Insert(ctx, s.db, &log)
	return err
}

func (s *Service) Complete(ctx context.Context, taskID, summary string) error {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=?, percentage=100, summary=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusCompleted, "completed", summary, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
			if task, getErr := s.Get(ctx, taskID); getErr == nil {
				s.invalidateFirstActiveByKey(task.ConcurrencyKey)
				err = s.writeTaskEvent(ctx, runtimeevents.EventTaskCompleted, task, summary, runtimeevents.SeverityInfo)
			}
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
	res, updateErr := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusFailed, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
	if updateErr == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
			if task, getErr := s.Get(ctx, taskID); getErr == nil {
				s.invalidateFirstActiveByKey(task.ConcurrencyKey)
				updateErr = s.writeTaskEvent(ctx, runtimeevents.EventTaskFailed, task, msg, runtimeevents.SeverityError)
			}
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
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, retry_count=?, next_run_at=?, finished_at=NULL WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
		append([]any{StatusFailedRetryable, msg, nextRetry, nextRun.Format(time.RFC3339Nano), taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
			if task, getErr := s.Get(ctx, taskID); getErr == nil {
				err = s.writeTaskEvent(ctx, runtimeevents.EventTaskFailed, task, msg, runtimeevents.SeverityWarning)
			}
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
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusBlocked, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
	if err == nil {
		if affected, _ := res.RowsAffected(); affected > 0 {
			s.unregisterRunningExecutionLocked(taskID)
			if task, getErr := s.Get(ctx, taskID); getErr == nil {
				s.invalidateFirstActiveByKey(task.ConcurrencyKey)
				err = s.writeTaskEvent(ctx, runtimeevents.EventTaskFailed, task, msg, runtimeevents.SeverityError)
			}
		}
	}
	return err
}

func (s *Service) RunNow(ctx context.Context, taskID string) (Task, error) {
	q := orm.New(s.db).From("tasks").Where("id = ?", taskID)
	q.AndIn("status", []string{StatusQueued, StatusScheduled, StatusFailedRetryable})
	if err := q.UpdateColumns(ctx, map[string]any{"status": StatusQueued, "next_run_at": nil, "finished_at": nil}); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, taskID)
}

func (s *Service) FirstRunnable(ctx context.Context, taskType, resourceType, resourceID string) (Task, bool, error) {
	var row taskRow
	err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).
		Where("type = ?", taskType).
		And("resource_type = ?", resourceType).
		And("resource_id = ?", resourceID).
		AndIn("status", []string{StatusQueued, StatusScheduled, StatusFailedRetryable}).
		And("(next_run_at IS NULL OR next_run_at='' OR next_run_at<=?)", time.Now().UTC().Format(time.RFC3339Nano)).
		OrderBy("created_at ASC").
		First(ctx, &row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return row.toTask(), true, nil
}

func (s *Service) Children(ctx context.Context, parentTaskID string) ([]Task, error) {
	rows := []taskRow{}
	if err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).
		Where("parent_task_id = ?", parentTaskID).
		OrderBy("child_index ASC", "created_at ASC").
		All(ctx, &rows); err != nil {
		return nil, err
	}
	children := make([]Task, 0, len(rows))
	for i := range rows {
		children = append(children, rows[i].toTask())
	}
	return children, nil
}

func (s *Service) ExistingActiveByConcurrencyKey(ctx context.Context, concurrencyKey string) (Task, bool, error) {
	concurrencyKey = strings.TrimSpace(concurrencyKey)
	if concurrencyKey == "" {
		return Task{}, false, nil
	}
	var row taskRow
	err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).
		Where("concurrency_key = ?", concurrencyKey).
		AndIn("status", []string{StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable}).
		OrderBy("created_at DESC").
		First(ctx, &row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return row.toTask(), true, nil
}

func (s *Service) ExistingActiveByConcurrencyKeyAndType(ctx context.Context, concurrencyKey, taskType string) (Task, bool, error) {
	concurrencyKey = strings.TrimSpace(concurrencyKey)
	taskType = strings.TrimSpace(taskType)
	if concurrencyKey == "" || taskType == "" {
		return Task{}, false, nil
	}
	var row taskRow
	err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).
		Where("concurrency_key = ?", concurrencyKey).
		And("type = ?", taskType).
		AndIn("status", []string{StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable}).
		OrderBy("created_at DESC").
		First(ctx, &row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	return row.toTask(), true, nil
}

// FirstActiveByConcurrencyKey returns the earliest active task for a
// concurrency key. Results are cached in memory per key: while the cached
// first task stays active, callers (the resource-queue wait loop) avoid the
// database entirely. The cache is invalidated whenever a task with the key
// reaches a terminal state or is deleted, so a cache hit is only ever one
// queue handoff behind the database. On a cache hit the returned Task carries
// only its ID; callers that need the full row must use Get.
func (s *Service) FirstActiveByConcurrencyKey(ctx context.Context, concurrencyKey string) (Task, bool, error) {
	concurrencyKey = strings.TrimSpace(concurrencyKey)
	if concurrencyKey == "" {
		return Task{}, false, nil
	}
	s.queueMu.RLock()
	if taskID, ok := s.firstActiveByKey[concurrencyKey]; ok {
		s.queueMu.RUnlock()
		return Task{ID: taskID}, true, nil
	}
	s.queueMu.RUnlock()

	// Cache miss. Refill under the write lock so a concurrent terminal
	// transition can never interleave between the query and the cache write:
	// it either invalidates before us (and the query already sees the new
	// state) or after us (and removes the entry we just wrote).
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if taskID, ok := s.firstActiveByKey[concurrencyKey]; ok {
		return Task{ID: taskID}, true, nil
	}
	var row taskRow
	err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).
		Where("concurrency_key = ?", concurrencyKey).
		AndIn("status", []string{StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable}).
		OrderBy("created_at ASC").
		First(ctx, &row)
	if err == sql.ErrNoRows {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	s.firstActiveByKey[concurrencyKey] = row.ID
	return row.toTask(), true, nil
}

func (s *Service) CountByServerStatuses(ctx context.Context, serverID string, statuses ...string) (int, error) {
	statuses = cleanFilterValues(statuses...)
	if len(statuses) == 0 {
		return 0, nil
	}
	q := orm.New(s.db).From("tasks").Where("server_id = ?", serverID)
	q.AndIn("status", statuses)
	count, err := q.Count(ctx)
	return int(count), err
}

func (s *Service) CancelByServer(ctx context.Context, serverID, message string) (int, error) {
	activeStatuses := []string{StatusQueued, StatusScheduled, StatusRunning, StatusFailedRetryable}
	rows := []string{}
	if err := orm.New(s.db).From("tasks").Where("server_id = ?", serverID).
		AndIn("status", activeStatuses).Pluck(ctx, "id", &rows); err != nil {
		return 0, err
	}
	taskIDs := []string{}
	keys := []string{}
	for _, taskID := range rows {
		task, getErr := s.Get(ctx, taskID)
		if getErr == nil && s.isCancellationBlocked(task.Type) {
			continue
		}
		taskIDs = append(taskIDs, taskID)
		if getErr == nil {
			keys = append(keys, task.ConcurrencyKey)
		}
	}
	if len(taskIDs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(message) == "" {
		message = "Task cancelled because the server was removed"
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	updateArgs := []any{StatusCancelled, "cancelled", Redact(message), finishedAt}
	updateArgs = append(updateArgs, stringArgs(taskIDs)...)
	updateArgs = append(updateArgs, stringArgs(activeStatuses)...)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=?, error=?, next_run_at=NULL, finished_at=? WHERE id IN (`+placeholders(len(taskIDs))+`) AND status IN (`+placeholders(len(activeStatuses))+`)`, updateArgs...)
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
	for _, key := range keys {
		s.invalidateFirstActiveByKey(key)
	}
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
	if task, getErr := s.Get(ctx, taskID); getErr == nil && s.isCancellationBlocked(task.Type) {
		return panelerr.Validation("task_cancel_unsupported", "This task type cannot be cancelled")
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
		append([]any{StatusCancelled, "cancelled", Redact(message), finishedAt, taskID}, stringArgs(terminalStatuses)...)...)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil
	}
	if task, getErr := s.Get(ctx, taskID); getErr == nil {
		s.invalidateFirstActiveByKey(task.ConcurrencyKey)
		if err := s.writeTaskEvent(ctx, runtimeevents.EventTaskCancelled, task, message, runtimeevents.SeverityWarning); err != nil {
			return err
		}
	}
	s.runningMu.Lock()
	if execution, ok := s.runningExecutions[taskID]; ok && execution.Cancel != nil {
		execution.Cancel()
	}
	delete(s.runningExecutions, taskID)
	s.runningMu.Unlock()
	return nil
}

func (s *Service) isCancellationBlocked(taskType string) bool {
	def, ok := s.Registry().Definition(taskType)
	return ok && def.DisallowCancel
}

func (s *Service) SetTriggeredBy(ctx context.Context, taskID, triggeredBy string) error {
	return orm.New(s.db).From("tasks").Where("id = ?", taskID).
		UpdateColumns(ctx, map[string]any{"triggered_by": triggeredBy})
}

func (s *Service) CountFailuresSinceLastSuccess(ctx context.Context, taskType, resourceType, resourceID string, failureStatuses []string, excludeTriggeredBy string) (int, error) {
	var lastSuccess *string
	if err := orm.New(s.db).From("tasks").
		Where("type = ?", taskType).
		And("resource_type = ?", resourceType).
		And("resource_id = ?", resourceID).
		And("status = ?", StatusCompleted).
		SelectExpr("MAX(created_at)").
		ScanValue(ctx, &lastSuccess); err != nil {
		return 0, err
	}
	failureStatuses = cleanFilterValues(failureStatuses...)
	if len(failureStatuses) == 0 {
		return 0, nil
	}
	q := orm.New(s.db).From("tasks").
		Where("type = ?", taskType).
		And("resource_type = ?", resourceType).
		And("resource_id = ?", resourceID).
		AndIn("status", failureStatuses)
	if strings.TrimSpace(excludeTriggeredBy) != "" {
		q.And("COALESCE(triggered_by,'') <> ?", excludeTriggeredBy)
	}
	if lastSuccess != nil && strings.TrimSpace(*lastSuccess) != "" {
		q.And("created_at > ?", *lastSuccess)
	}
	failures, err := q.Count(ctx)
	if err != nil {
		return 0, err
	}
	return int(failures), nil
}

// CleanupRetained deletes terminal task history older than retention in batches.
// Child rows in task_steps/task_logs are removed explicitly so cleanup works
// even without enforced foreign key cascades.
func (s *Service) CleanupRetained(ctx context.Context, retention time.Duration) (int64, error) {
	if retention <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339Nano)
	var deleted int64
	for {
		ids := []string{}
		if err := orm.New(s.db).From("tasks").
			Where("status IN (?,?,?,?,?)", StatusCompleted, StatusFailed, StatusFailedRetryable, StatusBlocked, StatusCancelled).
			And("COALESCE(finished_at,'') <> ''").
			And("finished_at < ?", cutoff).
			Limit(500).
			Pluck(ctx, "id", &ids); err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		ph := placeholders(len(ids))
		args := stringArgs(ids)
		if _, err := orm.RawExec(ctx, s.db, `DELETE FROM task_steps WHERE task_id IN (`+ph+`)`, args...); err != nil {
			return deleted, err
		}
		if _, err := orm.RawExec(ctx, s.db, `DELETE FROM task_logs WHERE task_id IN (`+ph+`)`, args...); err != nil {
			return deleted, err
		}
		res, err := orm.RawExec(ctx, s.db, `DELETE FROM tasks WHERE id IN (`+ph+`)`, args...)
		if err != nil {
			return deleted, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += affected
		if len(ids) > 0 {
			deletedSet := make(map[string]struct{}, len(ids))
			for _, taskID := range ids {
				deletedSet[taskID] = struct{}{}
			}
			s.queueMu.Lock()
			for key, taskID := range s.firstActiveByKey {
				if _, ok := deletedSet[taskID]; ok {
					delete(s.firstActiveByKey, key)
				}
			}
			s.queueMu.Unlock()
		}
		if len(ids) < 500 {
			return deleted, nil
		}
	}
}

func (s *Service) Get(ctx context.Context, taskID string) (Task, error) {
	var row taskRow
	err := orm.New(s.db).From("tasks").SelectExpr(taskColumns).Where("id = ?", taskID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return Task{}, panelerr.NotFound("task")
	}
	if err != nil {
		return Task{}, err
	}
	return row.toTask(), nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	return s.list(ctx, filter, taskColumns)
}

func (s *Service) ListSummaries(ctx context.Context, filter ListFilter) (ListResult, error) {
	return s.list(ctx, filter, taskListColumns)
}

func (s *Service) list(ctx context.Context, filter ListFilter, columns string) (ListResult, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	parts := s.taskListWhereParts(filter)
	if filter.OperationPage {
		return s.listOperationPage(ctx, filter, parts, columns)
	}
	total, err := s.taskListQuery(parts).Count(ctx)
	if err != nil {
		return ListResult{}, err
	}
	rows := []taskRow{}
	if err := s.taskListQuery(parts).SelectExpr(columns).
		OrderBy("created_at DESC", "id DESC").Limit(filter.Limit).Offset(filter.Offset).
		All(ctx, &rows); err != nil {
		return ListResult{}, err
	}
	out := make([]Task, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toTask())
	}
	return ListResult{Items: out, Total: int(total), PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
}

type taskListCondition struct {
	sql  string
	args []any
}

func (s *Service) taskListWhereParts(filter ListFilter) []taskListCondition {
	parts := []taskListCondition{}
	statuses := cleanFilterValues(append(filter.Statuses, filter.Status)...)
	switch len(statuses) {
	case 0:
	case 1:
		parts = append(parts, taskListCondition{sql: "status = ?", args: []any{statuses[0]}})
	default:
		parts = append(parts, taskListCondition{sql: "status IN (" + placeholders(len(statuses)) + ")", args: stringArgs(statuses)})
	}
	if filter.ServerID != "" {
		parts = append(parts, taskListCondition{sql: "server_id = ?", args: []any{filter.ServerID}})
	}
	types := cleanFilterValues(append(filter.Types, filter.Type)...)
	switch len(types) {
	case 0:
		if !filter.IncludeInternal {
			hidden := s.hiddenTaskTypes()
			if len(hidden) > 0 {
				parts = append(parts, taskListCondition{sql: "type NOT IN (" + placeholders(len(hidden)) + ")", args: stringArgs(hidden)})
			}
		}
	case 1:
		parts = append(parts, taskListCondition{sql: "type = ?", args: []any{types[0]}})
	default:
		parts = append(parts, taskListCondition{sql: "type IN (" + placeholders(len(types)) + ")", args: stringArgs(types)})
	}
	if filter.ExcludeScheduled && len(types) == 0 {
		parts = append(parts, taskListCondition{sql: "(trigger_type='' OR trigger_type<>?)", args: []any{"scheduler"}})
	}
	if filter.OperationID != "" {
		parts = append(parts, taskListCondition{sql: "operation_id = ?", args: []any{filter.OperationID}})
	}
	return parts
}

func (s *Service) taskListQuery(parts []taskListCondition) *orm.Query {
	q := orm.New(s.db).From("tasks")
	for i, p := range parts {
		if i == 0 {
			q.Where(p.sql, p.args...)
		} else {
			q.And(p.sql, p.args...)
		}
	}
	return q
}

func taskListWhereSQL(parts []taskListCondition) (string, []any) {
	if len(parts) == 0 {
		return "", nil
	}
	sqls := make([]string, 0, len(parts))
	var args []any
	for _, p := range parts {
		sqls = append(sqls, p.sql)
		args = append(args, p.args...)
	}
	return " WHERE " + strings.Join(sqls, " AND "), args
}

func (s *Service) listOperationPage(ctx context.Context, filter ListFilter, parts []taskListCondition, columns string) (ListResult, error) {
	const operationKey = `COALESCE(NULLIF(operation_id,''), id)`
	q := s.taskListQuery(parts)
	q.SelectExpr("COUNT(DISTINCT " + operationKey + ")")
	var total int
	if err := q.ScanValue(ctx, &total); err != nil {
		return ListResult{}, err
	}

	where, args := taskListWhereSQL(parts)
	keyArgs := append([]any{}, args...)
	keyArgs = append(keyArgs, filter.Limit, filter.Offset)
	keyRows, err := orm.Raw(ctx, s.db, `SELECT `+operationKey+` AS operation_key, MAX(created_at) AS operation_created_at FROM tasks`+where+` GROUP BY operation_key ORDER BY operation_created_at DESC, operation_key DESC LIMIT ? OFFSET ?`, keyArgs...)
	if err != nil {
		return ListResult{}, err
	}
	defer keyRows.Close()
	keys := []string{}
	for keyRows.Next() {
		var key, createdAt string
		if err := keyRows.Scan(&key, &createdAt); err != nil {
			return ListResult{}, err
		}
		keys = append(keys, key)
	}
	if err := keyRows.Err(); err != nil {
		return ListResult{}, err
	}
	if len(keys) == 0 {
		return ListResult{Items: []Task{}, Total: total, PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
	}

	itemQuery := s.taskListQuery(parts)
	itemQuery.Where(operationKey+" IN ("+placeholders(len(keys))+")", stringArgs(keys)...)
	rows := []taskRow{}
	if err := itemQuery.SelectExpr(columns).OrderBy("created_at DESC", "id DESC").All(ctx, &rows); err != nil {
		return ListResult{}, err
	}
	out := make([]Task, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toTask())
	}
	return ListResult{Items: out, Total: total, PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
}

func (s *Service) hiddenTaskTypes() []string {
	hidden := []string{}
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
	taskIDs := []string{}
	if err := orm.New(s.db).From("tasks").Where("status = ?", StatusRunning).Pluck(ctx, "id", &taskIDs); err != nil {
		return 0, err
	}

	failed := 0
	finishedAt := now.UTC().Format(time.RFC3339Nano)
	const message = "Task was marked running but no active execution exists in this Panel process"
	for _, taskID := range taskIDs {
		if _, ok := s.runningExecutions[taskID]; ok {
			continue
		}
		res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=CASE WHEN stage='' THEN 'orphaned' ELSE stage END, error=CASE WHEN error='' THEN ? ELSE error END, next_run_at=NULL, finished_at=? WHERE id=? AND status=?`,
			StatusFailed, message, finishedAt, taskID, StatusRunning)
		if err != nil {
			return failed, err
		}
		affected, err := res.RowsAffected()
		if err == nil {
			failed += int(affected)
		}
	}
	if failed > 0 {
		s.invalidateAllFirstActiveKeys()
	}
	return failed, nil
}

func (s *Service) HasRunningExecution(taskID string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	_, ok := s.runningExecutions[taskID]
	return ok
}

func (s *Service) RunningExecutionCount() int {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	return len(s.runningExecutions)
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
	err := orm.New(s.db).From("tasks").Select("status").Where("id = ?", taskID).ScanValue(context.Background(), &status)
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
	res, err := orm.RawExec(ctx, s.db, query, updateArgs...)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	if affected > 0 {
		s.invalidateAllFirstActiveKeys()
	}
	return int(affected), nil
}

func (s *Service) Logs(ctx context.Context, taskID string, after int64) ([]Log, int64, error) {
	rows := []models.TaskLog{}
	if err := orm.New(s.db).From("task_logs").Select("id", "time", "stream", "line").
		Where("task_id = ?", taskID).And("id > ?", after).OrderBy("id ASC").Limit(200).
		All(ctx, &rows); err != nil {
		return nil, after, err
	}
	logs := make([]Log, 0, len(rows))
	next := after
	for _, row := range rows {
		l := Log{Cursor: row.ID, Time: row.Time, Stream: row.Stream, Line: row.Line}
		next = l.Cursor
		logs = append(logs, l)
	}
	return logs, next, nil
}

func (s *Service) UpsertStep(ctx context.Context, taskID string, in StepInput) (Step, error) {
	if strings.TrimSpace(in.Step) == "" {
		return Step{}, panelerr.Validation("task_step_required", "Task step is required")
	}
	now := time.Now().UTC()
	var existingID string
	err := orm.New(s.db).From("task_steps").Select("id").
		Where("task_id = ?", taskID).And("step = ?", in.Step).ScanValue(ctx, &existingID)
	if err == sql.ErrNoRows {
		existingID = id.New("step")
		var startedAt, finishedAt *time.Time
		if in.Status == StatusRunning || in.Status == StatusCompleted {
			startedAt = &now
		}
		if in.Status == StatusCompleted || in.Status == StatusFailed || in.Status == StatusCancelled {
			finishedAt = &now
		}
		if err := orm.New(s.db).From("task_steps").Insert(ctx, &stepRow{
			ID: existingID, TaskID: taskID, Step: in.Step, Status: in.Status,
			Percentage: in.Percentage, MetadataJSON: in.MetadataJSON,
			StartedAt: startedAt, FinishedAt: finishedAt, Error: in.Error,
		}); err != nil {
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
		args = append(args, now.Format(time.RFC3339Nano))
	}
	if in.Status == StatusCompleted || in.Status == StatusFailed || in.Status == StatusCancelled {
		assignments += `,finished_at=?`
		args = append(args, now.Format(time.RFC3339Nano))
	}
	args = append(args, existingID)
	if _, err := orm.RawExec(ctx, s.db, `UPDATE task_steps SET `+assignments+` WHERE id=?`, args...); err != nil {
		return Step{}, err
	}
	return s.step(ctx, existingID)
}

func (s *Service) Steps(ctx context.Context, taskID string) ([]Step, error) {
	rows := []stepRow{}
	if err := orm.New(s.db).From("task_steps").
		Where("task_id = ?", taskID).OrderBy("id ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]Step, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toStep())
	}
	return out, nil
}

func (s *Service) Retry(ctx context.Context, taskID string) (Task, error) {
	old, err := s.Get(ctx, taskID)
	if err != nil {
		return Task{}, err
	}
	task, err := s.Create(ctx, CreateInput{
		OperationID:         old.OperationID,
		Type:                old.Type,
		ExecutionMode:       old.ExecutionMode,
		ScheduleKey:         old.ScheduleKey,
		ServerID:            old.ServerID,
		NodeID:              old.NodeID,
		ResourceType:        old.ResourceType,
		ResourceID:          old.ResourceID,
		TriggerType:         "retry",
		TriggerResourceType: old.ResourceType,
		TriggerResourceID:   old.ResourceID,
		TriggerTaskID:       old.ID,
		TriggeredBy:         old.TriggeredBy,
		ParamsJSON:          old.ParamsJSON,
		MetadataJSON:        old.MetadataJSON,
		Summary:             "Retrying " + old.Summary,
		MaxRetries:          old.MaxRetries,
	})
	if err == nil {
		err = s.writeTaskEvent(ctx, runtimeevents.EventTaskRetried, task, task.Summary, runtimeevents.SeverityInfo)
	}
	return task, err
}

func (s *Service) writeTaskEvent(ctx context.Context, eventType string, task Task, summary, severity string) error {
	if s == nil || s.events == nil || task.ID == "" {
		return nil
	}
	if summary == "" {
		summary = task.Type
	}
	s.events.Log(ctx, runtimeevents.WriteEventInput{
		EventType:    eventType,
		Category:     runtimeevents.CategoryTask,
		Severity:     severity,
		Source:       firstNonEmpty(task.TriggerType, "task"),
		SourceModule: "tasks",
		DedupeKey:    "task:" + task.ID + ":" + eventType + ":" + strconv.Itoa(task.RetryCount),
		Summary:      summary,
		OccurredAt:   time.Now().UTC(),
	})
	return nil
}

const taskColumns = `id,operation_id,type,parent_task_id,child_index,child_count,execution_mode,concurrency_key,schedule_key,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,params_json,metadata_json,status,stage,percentage,summary,error,retry_count,max_retries,next_run_at,created_at,started_at,finished_at`
const taskListColumns = `id,operation_id,type,parent_task_id,child_index,child_count,execution_mode,concurrency_key,schedule_key,server_id,node_id,resource_type,resource_id,trigger_type,trigger_resource_type,trigger_resource_id,trigger_task_id,triggered_by,'' AS params_json,'' AS metadata_json,status,stage,percentage,summary,error,retry_count,max_retries,next_run_at,created_at,started_at,finished_at`

// taskRow 是 tasks 表的本地行映射：params_json/metadata_json 按原始文本往返
// （models.Task 的 map JSON 语义会改写存储文本且无法承载非法 JSON）。
type taskRow struct {
	ID                  string
	OperationID         string
	Type                string
	ParentTaskID        string
	ChildIndex          int
	ChildCount          int
	ExecutionMode       string
	ConcurrencyKey      string
	ScheduleKey         string
	ServerID            string
	NodeID              string
	ResourceType        string
	ResourceID          string
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	TriggerTaskID       string
	TriggeredBy         string
	ParamsJSON          string
	MetadataJSON        string
	Status              string
	Stage               string
	Percentage          *float64
	Summary             string
	Error               string
	RetryCount          int
	MaxRetries          int
	NextRunAt           *time.Time
	CreatedAt           time.Time
	StartedAt           *time.Time
	FinishedAt          *time.Time
}

func (r taskRow) toTask() Task {
	t := Task{
		ID:                  r.ID,
		OperationID:         r.OperationID,
		Type:                r.Type,
		ParentTaskID:        r.ParentTaskID,
		ChildIndex:          r.ChildIndex,
		ChildCount:          r.ChildCount,
		ExecutionMode:       r.ExecutionMode,
		ConcurrencyKey:      r.ConcurrencyKey,
		ScheduleKey:         r.ScheduleKey,
		ServerID:            r.ServerID,
		NodeID:              r.NodeID,
		ResourceType:        r.ResourceType,
		ResourceID:          r.ResourceID,
		TriggerType:         r.TriggerType,
		TriggerResourceType: r.TriggerResourceType,
		TriggerResourceID:   r.TriggerResourceID,
		TriggerTaskID:       r.TriggerTaskID,
		TriggeredBy:         r.TriggeredBy,
		ParamsJSON:          r.ParamsJSON,
		MetadataJSON:        r.MetadataJSON,
		Status:              r.Status,
		Stage:               r.Stage,
		Summary:             r.Summary,
		Error:               r.Error,
		RetryCount:          r.RetryCount,
		MaxRetries:          r.MaxRetries,
		NextRunAt:           r.NextRunAt,
		CreatedAt:           r.CreatedAt,
		StartedAt:           r.StartedAt,
		FinishedAt:          r.FinishedAt,
	}
	if r.Percentage != nil {
		t.Percentage = r.Percentage
	} else if r.Status == StatusCompleted {
		done := float64(100)
		t.Percentage = &done
	}
	return t
}

func (s *Service) step(ctx context.Context, stepID string) (Step, error) {
	var row stepRow
	if err := orm.New(s.db).From("task_steps").Where("id = ?", stepID).First(ctx, &row); err != nil {
		return Step{}, err
	}
	return row.toStep(), nil
}

// stepRow 是 task_steps 表的本地行映射，metadata_json 按原始文本往返。
type stepRow struct {
	ID           string
	TaskID       string
	Step         string
	Status       string
	Percentage   float64
	MetadataJSON string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Error        string
}

func (r stepRow) toStep() Step {
	return Step{
		ID:           r.ID,
		TaskID:       r.TaskID,
		Step:         r.Step,
		Status:       r.Status,
		Percentage:   r.Percentage,
		MetadataJSON: r.MetadataJSON,
		StartedAt:    r.StartedAt,
		FinishedAt:   r.FinishedAt,
		Error:        r.Error,
	}
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
