package tasks

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
)

type RunNowRunner interface {
	RunNow(ctx context.Context, task Task) error
}

type Handler struct {
	service *Service
	runner  RunNowRunner
}

func NewHandler(service *Service, runners ...RunNowRunner) *Handler {
	var runner RunNowRunner
	if len(runners) > 0 {
		runner = runners[0]
	}
	return &Handler{service: service, runner: runner}
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
		Status:      r.URL.Query().Get("status"),
		ServerID:    r.URL.Query().Get("serverId"),
		Type:        r.URL.Query().Get("type"),
		OperationID: r.URL.Query().Get("operation_id"),
		Limit:       limit,
		Offset:      offset,
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

func (h *Handler) Steps(w http.ResponseWriter, r *http.Request) {
	steps, err := h.service.Steps(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/steps")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, steps)
}

func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Retry(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/retry")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, task)
}

func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.RunNow(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/run-now")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if h.runner != nil {
		if err := h.runner.RunNow(r.Context(), task); err != nil {
			httpx.Error(w, err)
			return
		}
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
