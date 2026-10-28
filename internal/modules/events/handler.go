package events

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

// Handler serves product HTTP routes for events and logs.
type Handler struct{ service *Service }

// NewHandler binds a service.
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, err := listQuery(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	page, err := h.service.List(r.Context(), f)
	respond(w, page, err)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Get(r.Context(), r.PathValue("eventId"))
	respond(w, dto, err)
}

func (h *Handler) GetLegacy(w http.ResponseWriter, r *http.Request) {
	dto, err := h.service.Get(r.Context(), r.PathValue("streamId"))
	respond(w, dto, err)
}

func (h *Handler) EventLogs(w http.ResponseWriter, r *http.Request) {
	f, err := linesQuery(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	page, err := h.service.Lines(r.Context(), r.PathValue("eventId"), f)
	respond(w, page, err)
}

func (h *Handler) LinesLegacy(w http.ResponseWriter, r *http.Request) {
	f, err := linesQuery(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	page, err := h.service.Lines(r.Context(), r.PathValue("streamId"), f)
	respond(w, page, err)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	f, err := logsQuery(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	page, err := h.service.Lines(r.Context(), f.EventID, f)
	respond(w, page, err)
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	f, err := listQuery(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	f.Limit = 500
	page, err := h.service.List(r.Context(), f)
	if err != nil {
		respond(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="events.jsonl"`)
	enc := json.NewEncoder(w)
	if enc.Encode(map[string]any{
		"type":          "manifest",
		"formatVersion": 1,
		"totalHint":     len(page.Items),
		"filters":       r.URL.Query(),
	}) != nil {
		return
	}
	for {
		for _, item := range page.Items {
			if enc.Encode(item) != nil {
				return
			}
		}
		if !page.HasMore {
			break
		}
		f.Cursor = page.NextCursor
		page, err = h.service.List(r.Context(), f)
		if err != nil {
			_ = enc.Encode(map[string]any{"type": "export.error", "message": err.Error()})
			return
		}
	}
}

func listQuery(r *http.Request) (ListFilter, error) {
	q := r.URL.Query()
	allowed := map[string]bool{}
	for _, k := range []string{"origin", "operationId", "executionId", "module", "state", "level", "from", "to", "hadError", "limit", "cursor"} {
		allowed[k] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 {
			return ListFilter{}, panelerr.BadRequest("events_query_invalid", "Unknown or repeated events query parameter: "+k)
		}
	}
	f := ListFilter{
		Origin:      q.Get("origin"),
		OperationID: q.Get("operationId"),
		ExecutionID: q.Get("executionId"),
		Module:      q.Get("module"),
		State:       q.Get("state"),
		Level:       q.Get("level"),
		Cursor:      q.Get("cursor"),
	}
	if f.Origin != "" && f.Origin != "user" && f.Origin != "system" && f.Origin != "process" {
		return ListFilter{}, panelerr.BadRequest("events_origin_invalid", "origin must be user, system, or process")
	}
	limit, err := parseInt(q.Get("limit"), 100)
	if err != nil || limit < 1 || limit > 500 {
		return f, panelerr.BadRequest("events_limit_invalid", "limit must be between 1 and 500")
	}
	f.Limit = limit
	for _, v := range []struct {
		k  string
		to *string
	}{{"from", &f.From}, {"to", &f.To}} {
		raw := q.Get(v.k)
		if raw == "" {
			continue
		}
		if _, e := time.Parse(time.RFC3339Nano, raw); e != nil {
			if _, e2 := time.Parse(time.RFC3339, raw); e2 != nil {
				return f, panelerr.BadRequest("events_time_invalid", "Time must use RFC3339")
			}
		}
		*v.to = raw
	}
	if f.From != "" && f.To != "" && f.From > f.To {
		return f, panelerr.BadRequest("events_time_range_invalid", "from must not be after to")
	}
	if raw := q.Get("hadError"); raw != "" {
		if raw != "true" && raw != "false" {
			return f, panelerr.BadRequest("events_boolean_invalid", "hadError must be true or false")
		}
		v := raw == "true"
		f.HadError = &v
	}
	return f, nil
}

func linesQuery(r *http.Request) (LinesFilter, error) {
	q := r.URL.Query()
	allowed := map[string]bool{}
	for _, k := range []string{"level", "limit", "after"} {
		allowed[k] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 {
			return LinesFilter{}, panelerr.BadRequest("events_query_invalid", "Unknown or repeated events query parameter: "+k)
		}
	}
	f := LinesFilter{Level: strings.TrimSpace(q.Get("level")), After: q.Get("after")}
	limit, err := parseInt(q.Get("limit"), 200)
	if err != nil || limit < 1 || limit > 2000 {
		return f, panelerr.BadRequest("events_limit_invalid", "limit must be between 1 and 2000")
	}
	f.Limit = limit
	return f, nil
}

func logsQuery(r *http.Request) (LinesFilter, error) {
	q := r.URL.Query()
	allowed := map[string]bool{}
	for _, k := range []string{"eventId", "level", "limit", "after", "from", "to"} {
		allowed[k] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 {
			return LinesFilter{}, panelerr.BadRequest("logs_query_invalid", "Unknown or repeated logs query parameter: "+k)
		}
	}
	// eventId is optional: empty means a global JSONL scan (newest files first).
	f := LinesFilter{
		EventID: strings.TrimSpace(q.Get("eventId")),
		Level:   strings.TrimSpace(q.Get("level")),
		After:   q.Get("after"),
	}
	limit, err := parseInt(q.Get("limit"), 200)
	if err != nil || limit < 1 || limit > 2000 {
		return f, panelerr.BadRequest("logs_limit_invalid", "limit must be between 1 and 2000")
	}
	f.Limit = limit
	return f, nil
}

func parseInt(s string, def int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}
