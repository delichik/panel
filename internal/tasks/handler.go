package tasks

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
	"panel/internal/panelerr"
)

type RunNowRunner interface {
	RunNow(ctx context.Context, task Task) error
}

type RunNowCapability interface {
	CanRun(task Task) bool
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
		Statuses:        queryList(r, "status"),
		ServerID:        r.URL.Query().Get("serverId"),
		Types:           queryList(r, "type"),
		IncludeInternal: truthyQuery(r, "includeInternal") || truthyQuery(r, "include_internal"),
		OperationID:     r.URL.Query().Get("operation_id"),
		Limit:           limit,
		Offset:          offset,
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
	old, err := h.service.Get(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/retry")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if !canRetryStatus(old.Status) {
		httpx.Error(w, panelerr.Validation("task_retry_status_invalid", "Only failed, retryable, or blocked tasks can be retried"))
		return
	}
	if !h.canRun(old) {
		httpx.Error(w, panelerr.Validation("task_retry_unsupported", "This task type cannot be retried from the task center"))
		return
	}
	task, err := h.service.Retry(r.Context(), old.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if h.runner != nil {
		if err := h.runner.RunNow(r.Context(), task); err != nil {
			h.failIfUnfinished(r.Context(), task.ID, err)
			httpx.Error(w, err)
			return
		}
	}
	httpx.JSON(w, http.StatusAccepted, task)
}

func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Get(r.Context(), taskID(strings.TrimSuffix(r.URL.Path, "/run-now")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if !canRunNowStatus(current.Status) {
		httpx.Error(w, panelerr.Validation("task_run_now_status_invalid", "Only queued, scheduled, or retryable tasks can be run now"))
		return
	}
	if !h.canRun(current) {
		httpx.Error(w, panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center"))
		return
	}
	task, err := h.service.RunNow(r.Context(), current.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if h.runner != nil {
		if err := h.runner.RunNow(r.Context(), task); err != nil {
			h.failIfUnfinished(r.Context(), task.ID, err)
			httpx.Error(w, err)
			return
		}
	}
	httpx.JSON(w, http.StatusAccepted, task)
}

func (h *Handler) canRun(task Task) bool {
	if h.runner == nil {
		return true
	}
	checker, ok := h.runner.(RunNowCapability)
	if !ok {
		return true
	}
	return checker.CanRun(task)
}

func canRunNowStatus(status string) bool {
	switch status {
	case StatusQueued, StatusScheduled, StatusFailedRetryable:
		return true
	default:
		return false
	}
}

func canRetryStatus(status string) bool {
	switch status {
	case StatusFailed, StatusFailedRetryable, StatusBlocked:
		return true
	default:
		return false
	}
}

func (h *Handler) failIfUnfinished(ctx context.Context, taskID string, cause error) {
	latest, err := h.service.Get(ctx, taskID)
	if err != nil {
		return
	}
	switch latest.Status {
	case StatusCompleted, StatusFailed, StatusBlocked, StatusCancelled:
		return
	default:
		_ = h.service.Fail(ctx, taskID, cause)
	}
}

func taskID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func queryList(r *http.Request, key string) []string {
	values := []string{}
	for _, raw := range r.URL.Query()[key] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
	}
	return values
}

func truthyQuery(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
