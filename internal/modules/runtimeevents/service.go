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

	return nil
}

func (s *Service) ListSystemEvents(ctx context.Context, filter ListFilter) (ListResult[Event], error) {
	filter = normalizeFilter(filter)
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
	var detailsPruned, eventsDeleted int64
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
	return CleanupResult{DetailsPruned: int(detailsPruned), EventsDeleted: int(eventsDeleted)}, nil
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

func (s *Service) eventByDedupeKey(ctx context.Context, dedupeKey string) (Event, error) {
	var event Event
	err := orm.New(s.db).From("runtime_events").Where("dedupe_key = ? AND dedupe_key <> ''", strings.TrimSpace(dedupeKey)).First(ctx, &event)
	if err == sql.ErrNoRows {
		return Event{}, panelerr.NotFound("runtime_event")
	}
	return event, err
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
