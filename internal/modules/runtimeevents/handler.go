package runtimeevents

import (
	"net/http"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/system-events", auth(http.HandlerFunc(h.ListSystemEvents)))
}

func (h *Handler) ListSystemEvents(w http.ResponseWriter, r *http.Request) {
	filter, err := listFilterFromRequest(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.ListSystemEvents(r.Context(), filter)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func listFilterFromRequest(r *http.Request) (ListFilter, error) {
	allowed := []string{"category", "source", "severity", "eventType", "from", "to"}
	page, pageSize, err := httpx.ParseListPage(r, allowed...)
	if err != nil {
		return ListFilter{}, err
	}
	from, err := parseQueryTime(r.URL.Query().Get("from"))
	if err != nil {
		return ListFilter{}, err
	}
	to, err := parseQueryTime(r.URL.Query().Get("to"))
	if err != nil {
		return ListFilter{}, err
	}
	return ListFilter{
		Category:  r.URL.Query().Get("category"),
		Source:    r.URL.Query().Get("source"),
		Severity:  r.URL.Query().Get("severity"),
		EventType: r.URL.Query().Get("eventType"),
		From:      from, To: to, Limit: pageSize, Offset: (page - 1) * pageSize,
	}, nil
}

func parseQueryTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, panelerr.BadRequest("time_invalid", "from and to must use RFC3339 format")
	}
	return &t, nil
}