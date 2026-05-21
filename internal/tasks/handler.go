package tasks

import (
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
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
	offset := (page - 1) * limit
	tasks, err := h.service.List(r.Context(), ListFilter{
		Status:   r.URL.Query().Get("status"),
		ServerID: r.URL.Query().Get("serverId"),
		Type:     r.URL.Query().Get("type"),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, tasks)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Get(r.Context(), taskID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, task)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	logs, next, err := h.service.Logs(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/logs")), after)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"nextCursor": next, "logs": logs})
}

func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.RunNow(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/run-now")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, task)
}

func taskID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
