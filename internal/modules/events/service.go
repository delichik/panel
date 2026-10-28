package events

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	platformevents "panel/internal/platform/events"
)

// Service exposes product reads over AppDB events and the JSONL log directory.
type Service struct {
	store  *platformevents.Store
	logDir string
}

// NewService wraps a store and the panel logs directory (<dataRoot>/logs).
func NewService(db *sql.DB, logDir string) *Service {
	return &Service{store: platformevents.NewStore(db), logDir: strings.TrimSpace(logDir)}
}

// ListFilter is the product event list query.
type ListFilter struct {
	Origin      string
	Module      string
	State       string
	Level       string
	OperationID string
	ExecutionID string
	From        string
	To          string
	HadError    *bool
	Limit       int
	Cursor      string
}

// LinesFilter is the product lines query.
type LinesFilter struct {
	EventID string
	Level   string
	Limit   int
	After   string
}

func (s *Service) List(ctx context.Context, filter ListFilter) (ListPage, error) {
	offset, err := parseCursorOffset(filter.Cursor)
	if err != nil {
		return ListPage{}, panelerr.BadRequest("events_cursor_invalid", "cursor must be a non-negative integer")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.store.List(ctx, platformevents.ListFilter{
		Origin:      filter.Origin,
		Module:      filter.Module,
		State:       filter.State,
		Level:       filter.Level,
		OperationID: filter.OperationID,
		ExecutionID: filter.ExecutionID,
		From:        filter.From,
		To:          filter.To,
		HadError:    filter.HadError,
		Limit:       limit + 1,
		Offset:      offset,
	})
	if err != nil {
		return ListPage{}, err
	}
	page := ListPage{Items: make([]EventDTO, 0, len(rows))}
	for i, row := range rows {
		if i >= limit {
			page.HasMore = true
			page.NextCursor = strconv.Itoa(offset + limit)
			break
		}
		page.Items = append(page.Items, toDTO(row))
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, eventID string) (EventDTO, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return EventDTO{}, panelerr.BadRequest("events_id_invalid", "event id is required")
	}
	row, err := s.store.Get(ctx, eventID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EventDTO{}, panelerr.NotFound("event")
		}
		return EventDTO{}, err
	}
	return toDTO(row), nil
}

func (s *Service) Lines(ctx context.Context, eventID string, filter LinesFilter) (LinesPage, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = strings.TrimSpace(filter.EventID)
	}
	requireEventRow := strings.TrimSpace(filter.EventID) == "" && eventID != ""
	if eventID != "" {
		if _, err := s.store.Get(ctx, eventID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// /events/{id}/logs requires a known events row; /logs?eventId= may scan JSONL alone.
				if requireEventRow {
					return LinesPage{}, panelerr.NotFound("event")
				}
			} else {
				return LinesPage{}, err
			}
		}
	}
	after, err := parseCursorOffset(filter.After)
	if err != nil {
		return LinesPage{}, panelerr.BadRequest("events_after_invalid", "after must be a non-negative integer")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	lines, err := platformevents.ReadLines(s.logDir, eventID, platformevents.LineFilter{
		Level: filter.Level,
		Limit: limit + 1,
		After: after,
	})
	if err != nil {
		return LinesPage{}, err
	}
	page := LinesPage{Items: make([]map[string]any, 0, len(lines))}
	for i, line := range lines {
		if i >= limit {
			page.HasMore = true
			page.NextCursor = strconv.Itoa(after + limit)
			break
		}
		page.Items = append(page.Items, line)
	}
	return page, nil
}

func toDTO(row platformevents.Record) EventDTO {
	return EventDTO{
		ID:          row.ID,
		Module:      row.Module,
		Level:       row.Level,
		Origin:      row.Origin,
		OperationID: row.OperationID,
		ExecutionID: row.ExecutionID,
		RequestID:   row.RequestID,
		Title:       row.Title,
		Summary:     row.Summary,
		State:       row.State,
		HadError:    row.HadError,
		StartedAt:   row.StartedAt,
		UpdatedAt:   row.UpdatedAt,
		FinishedAt:  row.FinishedAt,
	}
}

func parseCursorOffset(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, strconv.ErrSyntax
	}
	return n, nil
}
