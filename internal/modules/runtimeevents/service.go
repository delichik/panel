package runtimeevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Write(ctx context.Context, in WriteEventInput) (Event, bool, error) {
	if s == nil || s.db == nil {
		return Event{}, false, panelerr.Validation("runtime_event_service_unavailable", "Runtime event service is unavailable")
	}
	if err := validateEventInput(in); err != nil {
		return Event{}, false, err
	}
	now := time.Now().UTC()
	occurredAt := in.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = now
	}
	eventID := strings.TrimSpace(in.ID)
	if eventID == "" {
		eventID = id.New("revt")
	}
	severity := firstNonEmpty(in.Severity, SeverityInfo)
	detailAvailable := in.Detail != nil
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO runtime_events(id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,dedupe_key,summary,occurred_at,detail_available,detail_pruned_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		eventID, strings.TrimSpace(in.EventType), strings.TrimSpace(in.Category), strings.TrimSpace(in.SubjectType), strings.TrimSpace(in.SubjectID), strings.TrimSpace(in.OperationID),
		severity, strings.TrimSpace(in.Source), strings.TrimSpace(in.SourceModule), strings.TrimSpace(in.SourceType), strings.TrimSpace(in.SourceID), strings.TrimSpace(in.DedupeKey),
		strings.TrimSpace(in.Summary), formatTime(occurredAt), boolInt(detailAvailable), nil, formatTime(now))
	if err != nil {
		return Event{}, false, err
	}
	affected, _ := res.RowsAffected()
	inserted := affected > 0
	if inserted && in.Detail != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_event_details(event_id,payload,error,log_refs,task_refs,target_refs,created_at,pruned_at) VALUES(?,?,?,?,?,?,?,?)`,
			eventID, jsonOrDefault(in.Detail.PayloadJSON, "{}"), strings.TrimSpace(in.Detail.Error), jsonOrDefault(in.Detail.LogRefsJSON, "[]"), jsonOrDefault(in.Detail.TaskRefsJSON, "[]"), jsonOrDefault(in.Detail.TargetRefsJSON, "[]"), formatTime(now), nil); err != nil {
			return Event{}, false, err
		}
	}
	if inserted && in.Application != nil {
		if err := upsertApplicationOperation(ctx, tx, in.OperationID, occurredAt, detailAvailable, *in.Application); err != nil {
			return Event{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, err
	}
	if !inserted && strings.TrimSpace(in.DedupeKey) != "" {
		event, err := s.eventByDedupeKey(ctx, in.DedupeKey)
		return event, false, err
	}
	event, err := s.GetEvent(ctx, eventID)
	return event, inserted, err
}

func validateEventInput(in WriteEventInput) error {
	if strings.TrimSpace(in.EventType) == "" {
		return panelerr.Validation("runtime_event_type_required", "Runtime event type is required")
	}
	switch strings.TrimSpace(in.Category) {
	case CategoryApplication, CategoryTask, CategoryAlert, CategoryLog, CategoryRuntime, CategorySystem:
	default:
		return panelerr.Validation("runtime_event_category_invalid", "Runtime event category must be application, task, alert, log, runtime, or system")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return panelerr.Validation("runtime_event_summary_required", "Runtime event summary is required")
	}
	if in.Application != nil && strings.TrimSpace(in.OperationID) == "" {
		return panelerr.Validation("runtime_event_operation_required", "Application operation event requires an operation id")
	}
	return nil
}

func upsertApplicationOperation(ctx context.Context, tx *sql.Tx, operationID string, eventAt time.Time, detailAvailable bool, in ApplicationOperationInput) error {
	status := firstNonEmpty(in.Status, "running")
	source := firstNonEmpty(in.Source, "system")
	now := formatTime(time.Now().UTC())
	latest := formatTime(eventAt)
	_, err := tx.ExecContext(ctx, `INSERT INTO application_operation_records(operation_id,application_id,application_name_snapshot,action,source,triggered_by,trigger_reason,status,started_at,finished_at,target_total,target_succeeded,target_failed,latest_event_at,detail_available,failure_summary,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(operation_id) DO UPDATE SET
			application_id=COALESCE(NULLIF(excluded.application_id,''), application_operation_records.application_id),
			application_name_snapshot=COALESCE(NULLIF(excluded.application_name_snapshot,''), application_operation_records.application_name_snapshot),
			action=COALESCE(NULLIF(excluded.action,''), application_operation_records.action),
			source=COALESCE(NULLIF(excluded.source,''), application_operation_records.source),
			triggered_by=COALESCE(NULLIF(excluded.triggered_by,''), application_operation_records.triggered_by),
			trigger_reason=COALESCE(NULLIF(excluded.trigger_reason,''), application_operation_records.trigger_reason),
			status=excluded.status,
			started_at=COALESCE(application_operation_records.started_at, excluded.started_at),
			finished_at=excluded.finished_at,
			target_total=CASE WHEN excluded.target_total > 0 THEN excluded.target_total ELSE application_operation_records.target_total END,
			target_succeeded=CASE WHEN excluded.target_succeeded > 0 OR excluded.status IN ('succeeded','failed','partial_failed') THEN excluded.target_succeeded ELSE application_operation_records.target_succeeded END,
			target_failed=CASE WHEN excluded.target_failed > 0 OR excluded.status IN ('failed','partial_failed') THEN excluded.target_failed ELSE application_operation_records.target_failed END,
			latest_event_at=excluded.latest_event_at,
			detail_available=CASE WHEN excluded.detail_available=1 THEN 1 ELSE application_operation_records.detail_available END,
			failure_summary=COALESCE(NULLIF(excluded.failure_summary,''), application_operation_records.failure_summary),
			updated_at=excluded.updated_at`,
		strings.TrimSpace(operationID), strings.TrimSpace(in.ApplicationID), strings.TrimSpace(in.ApplicationNameSnapshot), strings.TrimSpace(in.Action), source, strings.TrimSpace(in.TriggeredBy), strings.TrimSpace(in.TriggerReason),
		status, nullableTime(in.StartedAt), nullableTime(in.FinishedAt), in.TargetTotal, in.TargetSucceeded, in.TargetFailed, latest, boolInt(detailAvailable), strings.TrimSpace(in.FailureSummary), now, now)
	return err
}

func (s *Service) ListApplicationOperations(ctx context.Context, filter ListFilter) (ListResult[OperationRecord], error) {
	filter = normalizeFilter(filter)
	conditions := []string{}
	args := []any{}
	appendCondition(&conditions, &args, "application_id", filter.ApplicationID)
	appendCondition(&conditions, &args, "action", filter.Action)
	appendCondition(&conditions, &args, "source", filter.Source)
	appendCondition(&conditions, &args, "status", filter.Status)
	appendTimeRange(&conditions, &args, "latest_event_at", filter.From, filter.To)
	where := whereClause(conditions)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_operation_records`+where, args...).Scan(&total); err != nil {
		return ListResult[OperationRecord]{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT operation_id,application_id,application_name_snapshot,action,source,triggered_by,trigger_reason,status,started_at,finished_at,target_total,target_succeeded,target_failed,latest_event_at,detail_available,failure_summary,created_at,updated_at FROM application_operation_records`+where+` ORDER BY latest_event_at DESC, operation_id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return ListResult[OperationRecord]{}, err
	}
	defer rows.Close()
	items := []OperationRecord{}
	for rows.Next() {
		item, err := scanOperation(rows)
		if err != nil {
			return ListResult[OperationRecord]{}, err
		}
		items = append(items, item)
	}
	return ListResult[OperationRecord]{Items: items, Total: total, PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, rows.Err()
}

func (s *Service) GetApplicationOperation(ctx context.Context, operationID string) (ApplicationOperationDetail, error) {
	op, err := s.operation(ctx, operationID)
	if err != nil {
		return ApplicationOperationDetail{}, err
	}
	events, err := s.eventsByOperation(ctx, operationID)
	if err != nil {
		return ApplicationOperationDetail{}, err
	}
	return ApplicationOperationDetail{Operation: op, Events: events, Targets: []any{}}, nil
}

func (s *Service) ListSystemEvents(ctx context.Context, filter ListFilter) (ListResult[Event], error) {
	filter = normalizeFilter(filter)
	filter.ApplicationID = ""
	return s.listEvents(ctx, filter)
}

func (s *Service) GetEvent(ctx context.Context, eventID string) (Event, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,summary,occurred_at,detail_available,detail_pruned_at,created_at FROM runtime_events WHERE id=?`, strings.TrimSpace(eventID))
	event, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return Event{}, panelerr.NotFound("runtime_event")
	}
	return event, err
}

func (s *Service) GetEventDetail(ctx context.Context, eventID string) (EventDetail, error) {
	event, err := s.GetEvent(ctx, eventID)
	if err != nil {
		return EventDetail{}, err
	}
	if !event.DetailAvailable {
		return EventDetail{Event: event, PayloadJSON: "{}", LogRefsJSON: "[]", TaskRefsJSON: "[]", TargetRefsJSON: "[]"}, nil
	}
	row := s.db.QueryRowContext(ctx, `SELECT payload,error,log_refs,task_refs,target_refs FROM runtime_event_details WHERE event_id=? AND pruned_at IS NULL`, event.ID)
	detail := EventDetail{Event: event}
	err = row.Scan(&detail.PayloadJSON, &detail.Error, &detail.LogRefsJSON, &detail.TaskRefsJSON, &detail.TargetRefsJSON)
	if err == sql.ErrNoRows {
		event.DetailAvailable = false
		detail.Event = event
		detail.PayloadJSON = "{}"
		detail.LogRefsJSON = "[]"
		detail.TaskRefsJSON = "[]"
		detail.TargetRefsJSON = "[]"
		return detail, nil
	}
	return detail, err
}

func (s *Service) GetSystemEventDetail(ctx context.Context, eventID string) (SystemEventDetail, error) {
	detail, err := s.GetEventDetail(ctx, eventID)
	if err != nil {
		return SystemEventDetail{}, err
	}
	return SystemEventDetail{
		Event:      detail.Event,
		Payload:    parsePayload(detail.PayloadJSON),
		LogRefs:    parseJSONArray(detail.LogRefsJSON),
		TaskRefs:   parseJSONArray(detail.TaskRefsJSON),
		TargetRefs: parseJSONArray(detail.TargetRefsJSON),
	}, nil
}

func (s *Service) Cleanup(ctx context.Context, retentionDays, detailRetentionDays int) (CleanupResult, error) {
	if retentionDays < 1 || detailRetentionDays < 1 {
		return CleanupResult{}, panelerr.Validation("runtime_event_retention_invalid", "Runtime event retention must be at least 1 day")
	}
	if retentionDays < detailRetentionDays {
		return CleanupResult{}, panelerr.Validation("runtime_event_retention_order_invalid", "Runtime event retention must be greater than or equal to detail retention")
	}
	now := time.Now().UTC()
	detailCutoff := formatTime(now.AddDate(0, 0, -detailRetentionDays))
	recordCutoff := formatTime(now.AddDate(0, 0, -retentionDays))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CleanupResult{}, err
	}
	defer tx.Rollback()
	prunedAt := formatTime(now)
	res, err := tx.ExecContext(ctx, `UPDATE runtime_event_details SET payload='{}', error='', log_refs='[]', task_refs='[]', target_refs='[]', pruned_at=?
		WHERE pruned_at IS NULL
		  AND event_id IN (SELECT id FROM runtime_events WHERE occurred_at<?)`, prunedAt, detailCutoff)
	if err != nil {
		return CleanupResult{}, err
	}
	detailsPruned, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `UPDATE runtime_events SET detail_available=0, detail_pruned_at=? WHERE detail_available=1 AND id IN (SELECT event_id FROM runtime_event_details WHERE pruned_at=?)`, prunedAt, prunedAt); err != nil {
		return CleanupResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE application_operation_records SET detail_available=0 WHERE detail_available=1 AND operation_id NOT IN (SELECT DISTINCT operation_id FROM runtime_events WHERE operation_id<>'' AND detail_available=1)`); err != nil {
		return CleanupResult{}, err
	}
	res, err = tx.ExecContext(ctx, `DELETE FROM application_operation_records WHERE latest_event_at<?`, recordCutoff)
	if err != nil {
		return CleanupResult{}, err
	}
	operationsDeleted, _ := res.RowsAffected()
	res, err = tx.ExecContext(ctx, `DELETE FROM runtime_events WHERE occurred_at<?`, recordCutoff)
	if err != nil {
		return CleanupResult{}, err
	}
	eventsDeleted, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{DetailsPruned: int(detailsPruned), EventsDeleted: int(eventsDeleted), OperationsDeleted: int(operationsDeleted)}, nil
}

func (s *Service) listEvents(ctx context.Context, filter ListFilter) (ListResult[Event], error) {
	conditions := []string{}
	args := []any{}
	appendCondition(&conditions, &args, "category", filter.Category)
	appendCondition(&conditions, &args, "subject_type", filter.SubjectType)
	appendCondition(&conditions, &args, "subject_id", filter.SubjectID)
	appendCondition(&conditions, &args, "source", filter.Source)
	appendCondition(&conditions, &args, "severity", filter.Severity)
	appendCondition(&conditions, &args, "event_type", filter.EventType)
	appendTimeRange(&conditions, &args, "occurred_at", filter.From, filter.To)
	where := whereClause(conditions)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_events`+where, args...).Scan(&total); err != nil {
		return ListResult[Event]{}, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,summary,occurred_at,detail_available,detail_pruned_at,created_at FROM runtime_events`+where+` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return ListResult[Event]{}, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return ListResult[Event]{}, err
		}
		items = append(items, event)
	}
	return ListResult[Event]{Items: items, Total: total, PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, rows.Err()
}

func (s *Service) eventsByOperation(ctx context.Context, operationID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,summary,occurred_at,detail_available,detail_pruned_at,created_at FROM runtime_events WHERE operation_id=? ORDER BY occurred_at ASC, id ASC`, strings.TrimSpace(operationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Event{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *Service) eventByDedupeKey(ctx context.Context, dedupeKey string) (Event, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,summary,occurred_at,detail_available,detail_pruned_at,created_at FROM runtime_events WHERE dedupe_key=?`, strings.TrimSpace(dedupeKey))
	event, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return Event{}, panelerr.NotFound("runtime_event")
	}
	return event, err
}

func (s *Service) operation(ctx context.Context, operationID string) (OperationRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT operation_id,application_id,application_name_snapshot,action,source,triggered_by,trigger_reason,status,started_at,finished_at,target_total,target_succeeded,target_failed,latest_event_at,detail_available,failure_summary,created_at,updated_at FROM application_operation_records WHERE operation_id=?`, strings.TrimSpace(operationID))
	op, err := scanOperation(row)
	if err == sql.ErrNoRows {
		return OperationRecord{}, panelerr.NotFound("application_operation")
	}
	return op, err
}

func normalizeFilter(filter ListFilter) ListFilter {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func appendCondition(conditions *[]string, args *[]any, column, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return
	}
	*conditions = append(*conditions, column+"=?")
	*args = append(*args, value)
}

func appendTimeRange(conditions *[]string, args *[]any, column string, from, to *time.Time) {
	if from != nil {
		*conditions = append(*conditions, column+">=?")
		*args = append(*args, formatTime(from.UTC()))
	}
	if to != nil {
		*conditions = append(*conditions, column+"<=?")
		*args = append(*args, formatTime(to.UTC()))
	}
}

func whereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conditions, " AND ")
}

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var event Event
	var detailAvailable int
	var occurredAt, createdAt string
	var detailPruned sql.NullString
	if err := row.Scan(&event.ID, &event.EventType, &event.Category, &event.SubjectType, &event.SubjectID, &event.OperationID, &event.Severity, &event.Source, &event.SourceModule, &event.SourceType, &event.SourceID, &event.Summary, &occurredAt, &detailAvailable, &detailPruned, &createdAt); err != nil {
		return Event{}, err
	}
	event.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurredAt)
	event.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	event.DetailAvailable = detailAvailable == 1
	if detailPruned.Valid && strings.TrimSpace(detailPruned.String) != "" {
		t, _ := time.Parse(time.RFC3339Nano, detailPruned.String)
		event.DetailPrunedAt = &t
	}
	return event, nil
}

func scanOperation(row interface{ Scan(...any) error }) (OperationRecord, error) {
	var op OperationRecord
	var startedAt, finishedAt sql.NullString
	var latestAt, createdAt, updatedAt string
	var detailAvailable int
	if err := row.Scan(&op.OperationID, &op.ApplicationID, &op.ApplicationNameSnapshot, &op.Action, &op.Source, &op.TriggeredBy, &op.TriggerReason, &op.Status, &startedAt, &finishedAt, &op.TargetTotal, &op.TargetSucceeded, &op.TargetFailed, &latestAt, &detailAvailable, &op.FailureSummary, &createdAt, &updatedAt); err != nil {
		return OperationRecord{}, err
	}
	op.StartedAt = parseOptionalTime(startedAt)
	op.FinishedAt = parseOptionalTime(finishedAt)
	op.LatestEventAt, _ = time.Parse(time.RFC3339Nano, latestAt)
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	op.DetailAvailable = detailAvailable == 1
	return op, nil
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &t
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(value.UTC())
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func jsonOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parsePayload(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]any{}
	}
	var payload any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return value
	}
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func parseJSONArray(value string) []any {
	value = strings.TrimSpace(value)
	if value == "" {
		return []any{}
	}
	var refs []any
	if err := json.Unmarshal([]byte(value), &refs); err != nil {
		return []any{}
	}
	if refs == nil {
		return []any{}
	}
	return refs
}
