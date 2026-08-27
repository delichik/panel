package tasks

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
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
	page, pageSize, err := httpx.ParseListPage(r, "status", "serverId", "type", "includeInternal", "commonOnly", "operationId", "operationPage", "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	offset := (page - 1) * pageSize
	tasks, err := h.service.ListSummaries(r.Context(), ListFilter{
		Statuses:         queryList(r, "status"),
		ServerID:         r.URL.Query().Get("serverId"),
		Types:            queryList(r, "type"),
		IncludeInternal:  truthyQuery(r, "includeInternal"),
		ExcludeScheduled: truthyQuery(r, "commonOnly"),
		OperationID:      r.URL.Query().Get("operationId"),
		OperationPage:    truthyQuery(r, "operationPage"),
		Q:                strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:            pageSize,
		Offset:           offset,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	h.decorateList(&tasks)
	httpx.JSON(w, http.StatusOK, tasks)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Get(r.Context(), taskIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	h.decorateTask(&task)
	items := []Task{task}
	task = items[0]
	httpx.JSON(w, http.StatusOK, task)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	logs, next, err := h.service.Logs(r.Context(), taskIDFromRequest(r), after)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"nextCursor": next, "logs": logs})
}

func (h *Handler) Steps(w http.ResponseWriter, r *http.Request) {
	steps, err := h.service.Steps(r.Context(), taskIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, steps)
}

func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	old, err := h.service.Get(r.Context(), taskIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if !canRetryStatus(old.Status) {
		httpx.Error(w, panelerr.Validation("task_retry_status_invalid", "Only failed, retryable, or blocked tasks can be retried"))
		return
	}
	if !h.canRetry(old) {
		httpx.Error(w, panelerr.Validation("task_retry_unsupported", "This task type cannot be retried from the task center"))
		return
	}
	task, err := h.service.Retry(r.Context(), old.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if h.runner != nil {
		go h.dispatchRunNow(task)
	}
	h.decorateTask(&task)
	httpx.JSON(w, http.StatusAccepted, task)
}

func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	current, err := h.service.Get(r.Context(), taskIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if !canRunNowStatus(current.Status) {
		httpx.Error(w, panelerr.Validation("task_run_now_status_invalid", "Only queued, scheduled, or retryable tasks can be run now"))
		return
	}
	if !h.canRunNow(current) {
		httpx.Error(w, panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center"))
		return
	}
	task, err := h.service.RunNow(r.Context(), current.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if h.runner != nil {
		go h.dispatchRunNow(task)
	}
	h.decorateTask(&task)
	httpx.JSON(w, http.StatusAccepted, task)
}

// dispatchRunNow 异步执行任务并立即返回，避免同步执行占用 HTTP 连接。
// 执行失败且任务仍处于非终态时标记失败；执行边界内的 panic 由
// Worker/Manager 的 recover 转换，这里只做最后的兜底。
func (h *Handler) dispatchRunNow(task Task) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("task run-now goroutine recovered from panic: %v", recovered)
		}
	}()
	if h == nil || h.runner == nil {
		return
	}
	if err := h.runner.RunNow(context.Background(), task); err != nil {
		h.failIfUnfinished(context.Background(), task.ID, err)
	}
}

func (h *Handler) decorateList(result *ListResult) {
	if result == nil {
		return
	}
	for idx := range result.Items {
		h.decorateTask(&result.Items[idx])
	}
}

func (h *Handler) decorateTask(task *Task) {
	if task == nil || h.service == nil {
		return
	}
	def, ok := h.service.Registry().Definition(task.Type)
	if !ok {
		task.AllowCancel = true
		return
	}
	task.AllowCancel = !def.DisallowCancel
	if def.Execute == nil {
		return
	}
	task.AllowRunNow = def.AllowRunNow
	task.AllowRetry = def.AllowRetry
}

func (h *Handler) canRunNow(task Task) bool {
	def, ok := h.service.Registry().Definition(task.Type)
	if !ok || !def.AllowRunNow {
		return false
	}
	return h.runner != nil
}

func (h *Handler) canRetry(task Task) bool {
	def, ok := h.service.Registry().Definition(task.Type)
	if !ok || !def.AllowRetry {
		return false
	}
	return h.runner != nil
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

func taskIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
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
