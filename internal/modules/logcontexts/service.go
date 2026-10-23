package logcontexts

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logcontexts"
)

// Service exposes product reads over AppDB log_contexts and the JSONL log directory.
type Service struct {
	store  *logcontexts.Store
	logDir string
}

// NewService wraps a store and the panel logs directory (<dataRoot>/logs).
func NewService(db *sql.DB, logDir string) *Service {
	return &Service{store: logcontexts.NewStore(db), logDir: strings.TrimSpace(logDir)}
}

// ListFilter is the product list query.
type ListFilter struct {
	Origin      string
	Module      string
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
	Level  string
	Limit  int
	After  string
}

func (s *Service) List(ctx context.Context, filter ListFilter) (ListPage, error) {
	offset, err := parseCursorOffset(filter.Cursor)
	if err != nil {
		return ListPage{}, panelerr.BadRequest("log_contexts_cursor_invalid", "cursor must be a non-negative integer")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.store.List(ctx, logcontexts.ListFilter{
		Origin:      filter.Origin,
		Module:      filter.Module,
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
	page := ListPage{Items: make([]ContextDTO, 0, len(rows))}
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

func (s *Service) Get(ctx context.Context, streamID string) (ContextDTO, error) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return ContextDTO{}, panelerr.BadRequest("log_contexts_stream_invalid", "streamId is required")
	}
	row, err := s.store.Get(ctx, streamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ContextDTO{}, panelerr.NotFound("log context")
		}
		return ContextDTO{}, err
	}
	return toDTO(row), nil
}

func (s *Service) Lines(ctx context.Context, streamID string, filter LinesFilter) (LinesPage, error) {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return LinesPage{}, panelerr.BadRequest("log_contexts_stream_invalid", "streamId is required")
	}
	if _, err := s.store.Get(ctx, streamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LinesPage{}, panelerr.NotFound("log context")
		}
		return LinesPage{}, err
	}
	after, err := parseCursorOffset(filter.After)
	if err != nil {
		return LinesPage{}, panelerr.BadRequest("log_contexts_after_invalid", "after must be a non-negative integer")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	lines, err := logcontexts.ReadLines(s.logDir, streamID, logcontexts.LineFilter{
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

func toDTO(row logcontexts.Record) ContextDTO {
	return ContextDTO{
		StreamID:    row.StreamID,
		Module:      row.Module,
		Origin:      row.Origin,
		OperationID: row.OperationID,
		ExecutionID: row.ExecutionID,
		RequestID:   row.RequestID,
		Title:       row.Title,
		Summary:     row.Summary,
		Result:      row.Result,
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
