package runtimeevents

import (
	"net/http"
	"strconv"
	"time"

	httpx "panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/application-operations", auth(http.HandlerFunc(h.ListApplicationOperations)))
	mux.Handle("GET /api/v1/application-operations/{id}", auth(http.HandlerFunc(h.GetApplicationOperation)))
	mux.Handle("GET /api/v1/system-events", auth(http.HandlerFunc(h.ListSystemEvents)))
	mux.Handle("GET /api/v1/system-events/{id}", auth(http.HandlerFunc(h.GetSystemEvent)))
}

func (h *Handler) ListApplicationOperations(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListApplicationOperations(r.Context(), listFilterFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetApplicationOperation(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetApplicationOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

func (h *Handler) ListSystemEvents(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListSystemEvents(r.Context(), listFilterFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetSystemEvent(w http.ResponseWriter, r *http.Request) {
	detail, err := h.service.GetSystemEventDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, detail)
}

func listFilterFromRequest(r *http.Request) ListFilter {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if pageSize > 0 {
		limit = pageSize
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	return ListFilter{
		ApplicationID: r.URL.Query().Get("applicationId"),
		Action:        r.URL.Query().Get("action"),
		Category:      r.URL.Query().Get("category"),
		SubjectType:   r.URL.Query().Get("subjectType"),
		SubjectID:     r.URL.Query().Get("subjectId"),
		Source:        r.URL.Query().Get("source"),
		Status:        r.URL.Query().Get("status"),
		Severity:      r.URL.Query().Get("severity"),
		EventType:     r.URL.Query().Get("eventType"),
		From:          parseQueryTime(r.URL.Query().Get("from")),
		To:            parseQueryTime(r.URL.Query().Get("to")),
		Limit:         limit,
		Offset:        (page - 1) * limit,
	}
}

func parseQueryTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &t
}
