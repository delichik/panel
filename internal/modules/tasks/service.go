package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"panel/internal/modules/runtimeevents"
	"panel/internal/platform/activitylog"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
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
	Q                string
	SortOldestFirst  bool
	Limit            int
	Offset           int
}

var terminalStatuses = []string{StatusCompleted, StatusFailed, StatusBlocked, StatusCancelled}

const (
	baseTaskRetryDelay = 30 * time.Second
	maxTaskRetryDelay  = 10 * time.Minute
)

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
	return s.create(ctx, in)
}

func (s *Service) create(ctx context.Context, in CreateInput) (Task, error) {
	def, ok := s.Registry().Definition(in.Type)
	if !ok {
		return Task{}, panelerr.Validation("task_type_unregistered", "Task type is not registered")
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

func (s *Service) createTx(ctx context.Context, tx *sql.Tx, in CreateInput) (Task, error) {
	def, ok := s.Registry().Definition(in.Type)
	if !ok {
		return Task{}, panelerr.Validation("task_type_unregistered", "Task type is not registered")
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
	if err := activitylog.CheckAdmission(ctx, exec); err != nil {
		return Task{}, err
	}
	if actor := activitylog.ActorFromContext(ctx); actor.Kind != "" {
		in.TriggeredBy = firstNonEmpty(actor.ID, actor.Name)
		if in.TriggerType == "" {
			in.TriggerType = "user"
		}
	}
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
		Summary:             Redact(in.Summary),
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
	if err == nil {
		err = recordTaskReceipt(ctx, exec, t)
	}
	return t, err
}

func recordTaskReceipt(ctx context.Context, exec orm.Executor, task Task) error {
	var eventID string
	var seq int64
	err := exec.QueryRowContext(ctx, `SELECT event_id,seq FROM activity_events WHERE operation_id=? AND run_id=? AND event_type IN ('operation.requested','retry.requested') ORDER BY seq DESC LIMIT 1`, task.OperationID, task.ID).Scan(&eventID, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	activitylog.RecordReceipt(ctx, task.OperationID, eventID, seq)
	return nil
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
	if err := activitylog.CheckAdmission(ctx, s.db); err != nil {
		return false, err
	}
	s.registerRunningExecutionLocked(taskID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error='', next_run_at=NULL, percentage=COALESCE(percentage, 0), started_at=COALESCE(started_at, ?), finished_at=NULL WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusRunning, now, taskID}, stringArgs(terminalStatuses)...)...)
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
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET stage=? WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{stage, taskID}, stringArgs(terminalStatuses)...)...)
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

// AppendLog accepts the complete redacted output. Retention and line-length
// limits must never turn an accepted execution fact into missing evidence.
func (s *Service) AppendLog(ctx context.Context, taskID, stream, line string) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	level := "info"
	if stream == "stderr" {
		level = "error"
	}
	_, err = activitylog.Append(ctx, s.db, []activitylog.EventInput{{
		EventType: "output.chunk", Kind: "output", Level: level, Domain: firstNonEmpty(task.ResourceType, "system"), Action: task.Type,
		OperationID: task.OperationID, RunID: task.ID, ExecutionID: task.ID + ":attempt:" + fmt.Sprint(task.RetryCount),
		SourceStreamID: task.ID, Stream: stream, Text: Redact(line),
		Resources: []activitylog.Resource{{Type: task.ResourceType, ID: task.ResourceID, Role: "target"}},
		Data:      map[string]any{"taskId": task.ID},
	}})
	return err
}

func (s *Service) Complete(ctx context.Context, taskID, summary string) error {
	summary = Redact(summary)
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=?, percentage=100, summary=?, next_run_at=NULL, finished_at=? WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusCompleted, "completed", summary, now, taskID}, stringArgs(terminalStatuses)...)...)
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
	msg := taskErrorText(err)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, updateErr := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, finished_at=? WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusFailed, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
	if updateErr != nil {
		return updateErr
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// 任务已是终态：直接短路，避免在已完成任务的日志流中追加误导性行。
		return nil
	}
	s.unregisterRunningExecutionLocked(taskID)
	if task, getErr := s.Get(ctx, taskID); getErr == nil {
		s.invalidateFirstActiveByKey(task.ConcurrencyKey)
		if eventErr := s.writeTaskEvent(ctx, runtimeevents.EventTaskFailed, task, msg, runtimeevents.SeverityError); eventErr != nil {
			return eventErr
		}
	}
	return s.AppendLog(ctx, taskID, "stderr", msg)
}

func (s *Service) FailRetryable(ctx context.Context, taskID string, cause error) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if isTerminalStatus(task.Status) {
		return nil
	}
	msg := taskErrorText(cause)
	if task.MaxRetries <= 0 {
		task.MaxRetries = DefaultMaxTaskRetries
	}
	if task.RetryCount >= task.MaxRetries {
		return s.Block(ctx, taskID, cause)
	}
	nextRetry := task.RetryCount + 1
	nextRun := time.Now().UTC().Add(backoffDuration(nextRetry))
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, retry_count=?, next_run_at=?, finished_at=NULL WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
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
	msg := taskErrorText(cause)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if logErr := s.AppendLog(ctx, taskID, "stderr", msg); logErr != nil {
		return logErr
	}
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`, append([]any{StatusBlocked, msg, now, taskID}, stringArgs(terminalStatuses)...)...)
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
		if getErr == nil && (s.isCancellationBlocked(task.Type) || task.Stage == "uncertain") {
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
		return 0, err
	}
	// 逐任务写 task.cancelled 事件；只对确实被本次批量取消的任务写，
	// DedupeKey 由 writeTaskEvent 统一生成，重复写入会被 INSERT OR IGNORE 忽略。
	for _, taskID := range taskIDs {
		task, getErr := s.Get(ctx, taskID)
		if getErr == nil && task.Status == StatusCancelled {
			_ = s.writeTaskEvent(ctx, runtimeevents.EventTaskCancelled, task, message, runtimeevents.SeverityWarning)
		}
	}
	return int(affected), nil
}

func (s *Service) Cancel(ctx context.Context, taskID, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "Task cancelled"
	}
	if task, getErr := s.Get(ctx, taskID); getErr == nil && (s.isCancellationBlocked(task.Type) || task.Stage == "uncertain") {
		return panelerr.Validation("task_cancel_unsupported", "This task type cannot be cancelled")
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET status=?, stage=?, error=?, next_run_at=NULL, finished_at=? WHERE id=? AND stage<>'uncertain' AND status NOT IN (`+placeholders(len(terminalStatuses))+`)`,
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

// CleanupRetained is retained for internal callers during the control-plane
// transition. Audit evidence and its execution context are never age-deleted.
func (s *Service) CleanupRetained(ctx context.Context, retention time.Duration) (int64, error) {
	return 0, nil
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
	order := []string{"created_at DESC", "id DESC"}
	if filter.SortOldestFirst {
		order = []string{"created_at ASC", "id ASC"}
	}
	if err := s.taskListQuery(parts).SelectExpr(columns).
		OrderBy(order...).Limit(filter.Limit).Offset(filter.Offset).
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
	if q := strings.TrimSpace(filter.Q); q != "" {
		term := orm.LikeEscaped(q)
		parts = append(parts, taskListCondition{
			sql:  "(id LIKE ? ESCAPE '\\' OR summary LIKE ? ESCAPE '\\' OR type LIKE ? ESCAPE '\\' OR COALESCE(error,'') LIKE ? ESCAPE '\\')",
			args: []any{term, term, term, term},
		})
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
	if err := orm.New(s.db).From("tasks").Where("status = ?", StatusRunning).And("stage <> ?", "uncertain").Pluck(ctx, "id", &taskIDs); err != nil {
		return 0, err
	}

	failed := 0
	const message = "No active execution exists in this Panel process; the remote result requires verification"
	for _, taskID := range taskIDs {
		if _, ok := s.runningExecutions[taskID]; ok {
			continue
		}
		res, err := orm.RawExec(ctx, s.db, `UPDATE tasks SET stage='uncertain', error=?, next_run_at=NULL, finished_at=NULL WHERE id=? AND status=? AND stage<>'uncertain'`,
			message, taskID, StatusRunning)
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
	execution := &RunningExecution{TaskID: taskID, Context: ctx, Cancel: cancel}
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
	message := "Task stayed queued or scheduled past the worker startup timeout and was marked failed; retry the operation if it is still needed"
	// 只淘汰真正的孤儿：队首之后有更早创建的 **running** 任务（同一并发键）
	// 时，说明该任务只是在合法排队等待一个较慢的队首（例如耗时超过
	// StaleQueuedAfter 的部署），不能按 created_at 误杀；等队首结束后它会
	// 自然被 worker 取走。队首是 queued/scheduled/failed_retryable 且长期
	// 未推进时视为整条队列停滞（worker 不可用），等待任务照常按年龄淘汰。
	query := `UPDATE tasks SET status=?, stage=CASE WHEN stage='' THEN 'expired' ELSE stage END, error=CASE WHEN error='' THEN ? ELSE error END, next_run_at=NULL, finished_at=? WHERE status IN (?,?) AND created_at<=? AND (next_run_at IS NULL OR next_run_at='' OR next_run_at<=?) AND type IN (` + strings.Join(placeholders, ",") + `) AND (tasks.concurrency_key = '' OR NOT EXISTS (
		SELECT 1 FROM tasks AS t2
		WHERE t2.concurrency_key = tasks.concurrency_key
			AND t2.id <> tasks.id
			AND t2.status = 'running'
			AND (t2.created_at < tasks.created_at OR (t2.created_at = tasks.created_at AND t2.id < tasks.id))
	))`
	updateArgs := []any{StatusFailed, message, finishedAt, StatusQueued, StatusScheduled, cutoff, now.UTC().Format(time.RFC3339Nano)}
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
	rows, err := s.db.QueryContext(ctx, `SELECT seq,occurred_at,stream,text FROM activity_events WHERE event_type='output.chunk' AND json_extract(data_json,'$.taskId')=? AND seq>? ORDER BY seq LIMIT 200`, taskID, after)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	logs := []Log{}
	next := after
	for rows.Next() {
		var l Log
		var stamp string
		if err := rows.Scan(&l.Cursor, &stamp, &l.Stream, &l.Line); err != nil {
			return nil, after, err
		}
		if l.Time, err = time.Parse(time.RFC3339Nano, stamp); err != nil {
			return nil, after, err
		}
		logs = append(logs, l)
		next = l.Cursor
	}
	return logs, next, rows.Err()
}

func (s *Service) UpsertStep(ctx context.Context, taskID string, in StepInput) (Step, error) {
	in.Error = Redact(in.Error)
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
		if in.Status == StatusRunning {
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
		assignments += `,started_at=CASE WHEN status='running' THEN COALESCE(started_at,?) ELSE ? END,finished_at=NULL`
		args = append(args, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
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
	if old.Stage == "uncertain" {
		return Task{}, panelerr.Conflict("execution_result_unverified", "Verify the execution result before retrying")
	}
	def, ok := s.Registry().Definition(old.Type)
	if !ok || !def.AllowRetry || def.Execute == nil {
		return Task{}, panelerr.Validation("task_retry_unsupported", "This task type cannot be retried from the task center")
	}
	// Manual retries are immediate, but consume the same retry budget as
	// automatic retries so a new task cannot reset the counter indefinitely.
	if old.MaxRetries <= 0 {
		old.MaxRetries = DefaultMaxTaskRetries
	}
	if old.RetryCount >= old.MaxRetries {
		return Task{}, panelerr.Conflict("task_retry_exhausted", "Task retry limit has been reached")
	}
	nextRetry := old.RetryCount + 1
	nextRun := time.Now().UTC().Add(backoffDuration(nextRetry))
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
		RetryCount:          nextRetry,
		MaxRetries:          old.MaxRetries,
		NextRunAt:           &nextRun,
	})
	if err == nil {
		err = s.writeTaskEvent(ctx, runtimeevents.EventTaskRetried, task, task.Summary, runtimeevents.SeverityInfo)
	}
	return task, err
}

// State transitions are recorded by AppDB triggers inside the control
// transaction; emitting a second post-commit runtime event would split truth.
func (s *Service) writeTaskEvent(ctx context.Context, eventType string, task Task, summary, severity string) error {
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
		return baseTaskRetryDelay
	}
	delay := baseTaskRetryDelay
	for i := 1; i < retryCount; i++ {
		delay *= 2
		if delay >= maxTaskRetryDelay {
			return maxTaskRetryDelay
		}
	}
	// Add bounded jitter to avoid synchronized retries across servers while
	// keeping the configured maximum as a hard upper bound.
	jittered := time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
	if jittered > maxTaskRetryDelay {
		return maxTaskRetryDelay
	}
	return jittered
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func taskErrorText(err error) string {
	var pe *panelerr.Error
	if errors.As(err, &pe) {
		return Redact(i18n.Translate(pe.Code, pe.Message))
	}
	return Redact(err.Error())
}

func Redact(s string) string { return activitylog.Redact(s) }
