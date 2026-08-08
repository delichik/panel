package runtimeevents

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Service struct {
	db           *sql.DB
	subjectNames SubjectNameResolver
}

// facilityReverseProxyApplicationID is the hidden facility reverse-proxy
// application identity. Its operation projection must not appear in the
// user-facing operation records list; the facility page renders its own
// lifecycle operation instead.
const facilityReverseProxyApplicationID = "facility-reverse-proxy"

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// SetSubjectNameResolver installs a resolver used to enrich system events
// with the display name of their related object at read time.
func (s *Service) SetSubjectNameResolver(resolver SubjectNameResolver) {
	s.subjectNames = resolver
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
	var inserted bool
	err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := orm.RawExec(ctx, tx, `INSERT OR IGNORE INTO runtime_events(id,event_type,category,subject_type,subject_id,operation_id,severity,source,source_module,source_type,source_id,dedupe_key,summary,occurred_at,detail_available,detail_pruned_at,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			eventID, strings.TrimSpace(in.EventType), strings.TrimSpace(in.Category), strings.TrimSpace(in.SubjectType), strings.TrimSpace(in.SubjectID), strings.TrimSpace(in.OperationID),
			severity, strings.TrimSpace(in.Source), strings.TrimSpace(in.SourceModule), strings.TrimSpace(in.SourceType), strings.TrimSpace(in.SourceID), strings.TrimSpace(in.DedupeKey),
			strings.TrimSpace(in.Summary), formatTime(occurredAt), boolInt(detailAvailable), nil, formatTime(now))
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		inserted = affected > 0
		if inserted && in.Detail != nil {
			detail := &eventDetailRow{
				EventID:    eventID,
				Payload:    jsonOrDefault(in.Detail.PayloadJSON, "{}"),
				Error:      strings.TrimSpace(in.Detail.Error),
				LogRefs:    jsonOrDefault(in.Detail.LogRefsJSON, "[]"),
				TaskRefs:   jsonOrDefault(in.Detail.TaskRefsJSON, "[]"),
				TargetRefs: jsonOrDefault(in.Detail.TargetRefsJSON, "[]"),
				CreatedAt:  now,
			}
			if err := orm.New(tx).From("runtime_event_details").Insert(ctx, detail); err != nil {
				return err
			}
		}
		if inserted && in.Application != nil {
			if err := upsertApplicationOperation(ctx, tx, in.OperationID, occurredAt, detailAvailable, *in.Application); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
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
	_, err := orm.RawExec(ctx, tx, `INSERT INTO application_operation_records(operation_id,application_id,application_name_snapshot,action,source,triggered_by,trigger_reason,status,started_at,finished_at,target_total,target_succeeded,target_failed,latest_event_at,detail_available,failure_summary,created_at,updated_at)
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
	q := orm.New(s.db).From("application_operation_records").Where("application_id<>?", facilityReverseProxyApplicationID)
	appendFilter(q, "application_id", filter.ApplicationID)
	appendFilter(q, "action", filter.Action)
	appendFilter(q, "source", filter.Source)
	appendFilter(q, "status", filter.Status)
	appendTimeFilter(q, "latest_event_at", filter.From, filter.To)
	total, err := q.Count(ctx)
	if err != nil {
		return ListResult[OperationRecord]{}, err
	}
	q = orm.New(s.db).From("application_operation_records").Where("application_id<>?", facilityReverseProxyApplicationID)
	appendFilter(q, "application_id", filter.ApplicationID)
	appendFilter(q, "action", filter.Action)
	appendFilter(q, "source", filter.Source)
	appendFilter(q, "status", filter.Status)
	appendTimeFilter(q, "latest_event_at", filter.From, filter.To)
	q.OrderBy("latest_event_at DESC", "operation_id DESC").Limit(filter.Limit).Offset(filter.Offset)
	items := []OperationRecord{}
	if err := q.All(ctx, &items); err != nil {
		return ListResult[OperationRecord]{}, err
	}
	return ListResult[OperationRecord]{Items: items, Total: int(total), PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
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
	result, err := s.listEvents(ctx, filter)
	if err != nil {
		return ListResult[Event]{}, err
	}
	s.resolveSubjectNames(ctx, result.Items)
	return result, nil
}

func (s *Service) GetEvent(ctx context.Context, eventID string) (Event, error) {
	var event Event
	err := orm.New(s.db).From("runtime_events").Where("id = ?", strings.TrimSpace(eventID)).First(ctx, &event)
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
	var row eventDetailRow
	err = orm.New(s.db).From("runtime_event_details").Select("payload", "error", "log_refs", "task_refs", "target_refs").
		Where("event_id = ?", event.ID).AndNull("pruned_at").First(ctx, &row)
	detail := EventDetail{Event: event}
	detail.PayloadJSON = row.Payload
	detail.Error = row.Error
	detail.LogRefsJSON = row.LogRefs
	detail.TaskRefsJSON = row.TaskRefs
	detail.TargetRefsJSON = row.TargetRefs
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
	events := []Event{detail.Event}
	s.resolveSubjectNames(ctx, events)
	return SystemEventDetail{
		Event:      events[0],
		Payload:    parsePayload(detail.PayloadJSON),
		Error:      detail.Error,
		LogRefs:    parseJSONArray(detail.LogRefsJSON),
		TaskRefs:   parseJSONArray(detail.TaskRefsJSON),
		TargetRefs: parseJSONArray(detail.TargetRefsJSON),
	}, nil
}

// resolveSubjectNames fills SubjectName for events whose subject can be
// resolved by the installed resolver. Lookups are cached per subject so a
// page only queries each distinct (type, id) once.
func (s *Service) resolveSubjectNames(ctx context.Context, events []Event) {
	if s == nil || s.subjectNames == nil || len(events) == 0 {
		return
	}
	cache := map[string]string{}
	for i := range events {
		event := &events[i]
		if strings.TrimSpace(event.SubjectType) == "" || strings.TrimSpace(event.SubjectID) == "" {
			continue
		}
		key := event.SubjectType + "\x00" + event.SubjectID
		name, ok := cache[key]
		if !ok {
			name = s.subjectNames(ctx, event.SubjectType, event.SubjectID)
			cache[key] = name
		}
		event.SubjectName = name
	}
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
	prunedAt := formatTime(now)
	var detailsPruned, operationsDeleted, eventsDeleted int64
	err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := orm.RawExec(ctx, tx, `UPDATE runtime_event_details SET payload='{}', error='', log_refs='[]', task_refs='[]', target_refs='[]', pruned_at=?
			WHERE pruned_at IS NULL
			  AND event_id IN (SELECT id FROM runtime_events WHERE occurred_at<?)`, prunedAt, detailCutoff)
		if err != nil {
			return err
		}
		detailsPruned, _ = res.RowsAffected()
		if _, err := orm.RawExec(ctx, tx, `UPDATE runtime_events SET detail_available=0, detail_pruned_at=? WHERE detail_available=1 AND id IN (SELECT event_id FROM runtime_event_details WHERE pruned_at=?)`, prunedAt, prunedAt); err != nil {
			return err
		}
		if _, err := orm.RawExec(ctx, tx, `UPDATE application_operation_records SET detail_available=0 WHERE detail_available=1 AND operation_id NOT IN (SELECT DISTINCT operation_id FROM runtime_events WHERE operation_id<>'' AND detail_available=1)`); err != nil {
			return err
		}
		res, err = orm.RawExec(ctx, tx, `DELETE FROM application_operation_records WHERE latest_event_at<?`, recordCutoff)
		if err != nil {
			return err
		}
		operationsDeleted, _ = res.RowsAffected()
		res, err = orm.RawExec(ctx, tx, `DELETE FROM runtime_events WHERE occurred_at<?`, recordCutoff)
		if err != nil {
			return err
		}
		eventsDeleted, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{DetailsPruned: int(detailsPruned), EventsDeleted: int(eventsDeleted), OperationsDeleted: int(operationsDeleted)}, nil
}

func (s *Service) listEvents(ctx context.Context, filter ListFilter) (ListResult[Event], error) {
	q := orm.New(s.db).From("runtime_events")
	appendFilter(q, "category", filter.Category)
	appendFilter(q, "subject_type", filter.SubjectType)
	appendFilter(q, "subject_id", filter.SubjectID)
	appendFilter(q, "source", filter.Source)
	appendFilter(q, "severity", filter.Severity)
	appendFilter(q, "event_type", filter.EventType)
	appendTimeFilter(q, "occurred_at", filter.From, filter.To)
	total, err := q.Count(ctx)
	if err != nil {
		return ListResult[Event]{}, err
	}
	q = orm.New(s.db).From("runtime_events")
	appendFilter(q, "category", filter.Category)
	appendFilter(q, "subject_type", filter.SubjectType)
	appendFilter(q, "subject_id", filter.SubjectID)
	appendFilter(q, "source", filter.Source)
	appendFilter(q, "severity", filter.Severity)
	appendFilter(q, "event_type", filter.EventType)
	appendTimeFilter(q, "occurred_at", filter.From, filter.To)
	q.OrderBy("occurred_at DESC", "id DESC").Limit(filter.Limit).Offset(filter.Offset)
	items := []Event{}
	if err := q.All(ctx, &items); err != nil {
		return ListResult[Event]{}, err
	}
	return ListResult[Event]{Items: items, Total: int(total), PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
}

func (s *Service) eventsByOperation(ctx context.Context, operationID string) ([]Event, error) {
	items := []Event{}
	if err := orm.New(s.db).From("runtime_events").
		Where("operation_id = ?", strings.TrimSpace(operationID)).
		OrderBy("occurred_at ASC", "id ASC").
		All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) eventByDedupeKey(ctx context.Context, dedupeKey string) (Event, error) {
	var event Event
	err := orm.New(s.db).From("runtime_events").Where("dedupe_key = ? AND dedupe_key <> ''", strings.TrimSpace(dedupeKey)).First(ctx, &event)
	if err == sql.ErrNoRows {
		return Event{}, panelerr.NotFound("runtime_event")
	}
	return event, err
}

func (s *Service) operation(ctx context.Context, operationID string) (OperationRecord, error) {
	var op OperationRecord
	err := orm.New(s.db).From("application_operation_records").Where("operation_id = ?", strings.TrimSpace(operationID)).First(ctx, &op)
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

func appendFilter(q *orm.Query, column, value string) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return
	}
	q.And(column+" = ?", value)
}

func appendTimeFilter(q *orm.Query, column string, from, to *time.Time) {
	if from != nil {
		q.And(column+" >= ?", formatTime(from.UTC()))
	}
	if to != nil {
		q.And(column+" <= ?", formatTime(to.UTC()))
	}
}

// eventDetailRow 是 runtime_event_details 的本地行映射：payload/refs 列必须按
// 原始文本往返（写入端可能收到非法 JSON，models.RuntimeEventDetail 的 map
// JSON 语义无法承载）。
type eventDetailRow struct {
	EventID    string
	Payload    string
	Error      string
	LogRefs    string
	TaskRefs   string
	TargetRefs string
	CreatedAt  time.Time
	PrunedAt   *time.Time
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
