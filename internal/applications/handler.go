package applications

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
)

type applicationService interface {
	List(ctx context.Context) ([]Application, error)
	Get(ctx context.Context, id string) (Application, error)
	Create(ctx context.Context, in SaveInput) (Application, error)
	Update(ctx context.Context, id string, in SaveInput) (Application, error)
	Delete(ctx context.Context, id string) error
	Validate(ctx context.Context, id string) (ValidationResult, error)
	Plan(ctx context.Context, id string) (PlanResult, error)
	Deploy(ctx context.Context, id string) (OperationResult, error)
	Stop(ctx context.Context, id string, purge bool) (OperationResult, error)
	Restart(ctx context.Context, id string) (OperationResult, error)
	Runtime(ctx context.Context, id string) (ApplicationRuntime, error)
	Logs(ctx context.Context, id string, in LogInput) (LogResult, error)
}

type Handler struct {
	service applicationService
}

func NewHandler(service applicationService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	apps, err := h.service.List(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, apps)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in SaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	app, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, app)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	app, err := h.service.Get(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in SaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	app, err := h.service.Update(r.Context(), applicationID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), applicationID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Validate(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Plan(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Plan(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Deploy(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Stop(r.Context(), applicationID(r.URL.Path), r.URL.Query().Get("purge") == "true")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Restart(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Runtime(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	result, err := h.service.Logs(r.Context(), applicationID(r.URL.Path), LogInput{
		AllocID: r.URL.Query().Get("allocId"),
		Task:    r.URL.Query().Get("task"),
		Type:    r.URL.Query().Get("type"),
		Tail:    tail,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func applicationID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
