package runtimeevents

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Log 实现 EventWriter：同步直写一条日志，供测试或直写场景使用。
func (s *Service) Log(ctx context.Context, in WriteEventInput) {
	_, _, _ = s.Write(ctx, in)
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
	var inserted bool
	err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := insertEvent(ctx, tx, eventID, in, occurredAt, now, severity)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		inserted = affected > 0
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

// WriteBatch 在一个事务内批量写入多条日志（INSERT OR IGNORE），供缓冲写入器使用。
func (s *Service) WriteBatch(ctx context.Context, inputs []WriteEventInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	written := 0
	err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		for _, in := range inputs {
			if err := validateEventInput(in); err != nil {
				continue
			}
			occurredAt := in.OccurredAt.UTC()
			if occurredAt.IsZero() {
				occurredAt = now
			}
			eventID := strings.TrimSpace(in.ID)
			if eventID == "" {
				eventID = id.New("revt")
			}
			res, err := insertEvent(ctx, tx, eventID, in, occurredAt, now, firstNonEmpty(in.Severity, SeverityInfo))
			if err != nil {
				return err
			}
			if affected, _ := res.RowsAffected(); affected > 0 {
				written++
			}
		}
		return nil
	})
	return written, err
}

func insertEvent(ctx context.Context, tx *sql.Tx, eventID string, in WriteEventInput, occurredAt, now time.Time, severity string) (sql.Result, error) {
	return orm.RawExec(ctx, tx, `INSERT OR IGNORE INTO runtime_events(id,event_type,category,severity,source,source_module,dedupe_key,summary,occurred_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		eventID, strings.TrimSpace(in.EventType), strings.TrimSpace(in.Category), severity,
		strings.TrimSpace(in.Source), strings.TrimSpace(in.SourceModule), strings.TrimSpace(in.DedupeKey),
		strings.TrimSpace(in.Summary), formatTime(occurredAt), formatTime(now))
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
	return s.listEvents(ctx, filter)
}

func (s *Service) GetEvent(ctx context.Context, eventID string) (Event, error) {
	var event Event
	err := orm.New(s.db).From("runtime_events").Where("id = ?", strings.TrimSpace(eventID)).First(ctx, &event)
	if err == sql.ErrNoRows {
		return Event{}, panelerr.NotFound("runtime_event")
	}
	return event, err
}

func (s *Service) Cleanup(ctx context.Context, retentionDays int) (CleanupResult, error) {
	if retentionDays < 1 {
		return CleanupResult{}, panelerr.Validation("runtime_event_retention_invalid", "Runtime event retention must be at least 1 day")
	}
	recordCutoff := formatTime(time.Now().UTC().AddDate(0, 0, -retentionDays))
	var eventsDeleted int64
	err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := orm.RawExec(ctx, tx, `DELETE FROM runtime_events WHERE occurred_at<?`, recordCutoff)
		if err != nil {
			return err
		}
		eventsDeleted, _ = res.RowsAffected()
		return nil
	})
	if err != nil {
		return CleanupResult{}, err
	}
	return CleanupResult{EventsDeleted: int(eventsDeleted)}, nil
}

func (s *Service) listEvents(ctx context.Context, filter ListFilter) (ListResult[Event], error) {
	q := orm.New(s.db).From("runtime_events")
	appendFilter(q, "category", filter.Category)
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

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// BufferedWriter 是系统日志的专用批量写入器：生产者通过 Log 非阻塞入队，
// 后台每 interval 秒 drain 一次，并以一个事务批量落库；Stop 时 flush 剩余。
type BufferedWriter struct {
	svc      *Service
	ch       chan WriteEventInput
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

func NewBufferedWriter(svc *Service, interval time.Duration) *BufferedWriter {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &BufferedWriter{
		svc:      svc,
		ch:       make(chan WriteEventInput, 1024),
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 启动后台批量落库协程。
func (w *BufferedWriter) Start(ctx context.Context) {
	if w == nil {
		return
	}
	go w.loop(ctx)
}

// Stop 停止后台协程并 flush 缓冲区内剩余日志；可重复调用。
func (w *BufferedWriter) Stop() {
	if w == nil {
		return
	}
	select {
	case <-w.done:
		return
	default:
	}
	close(w.stop)
	<-w.done
}

// Log 非阻塞入队；缓冲区满时丢弃该条日志，不阻塞业务调用方。
func (w *BufferedWriter) Log(_ context.Context, in WriteEventInput) {
	if w == nil {
		return
	}
	select {
	case w.ch <- in:
	default:
	}
}

func (w *BufferedWriter) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.flush()
			return
		case <-w.stop:
			w.flush()
			return
		case <-ticker.C:
			w.flush()
		}
	}
}

// flush 取出当前缓冲区的全部日志并批量写入；测试可在同包内直接调用。
func (w *BufferedWriter) flush() {
	if w == nil {
		return
	}
	if w.svc == nil {
		for {
			select {
			case <-w.ch:
			default:
				return
			}
		}
	}
	batch := make([]WriteEventInput, 0, 64)
	for {
		select {
		case in := <-w.ch:
			batch = append(batch, in)
		default:
			if len(batch) == 0 {
				return
			}
			if _, err := w.svc.WriteBatch(context.Background(), batch); err != nil {
				// 批量写入失败时丢弃本批日志，避免阻塞业务或无限重试。
			}
			return
		}
	}
}