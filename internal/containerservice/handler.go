package containerservice

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
	"panel/internal/panelerr"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.List(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid JSON request body"))
		return
	}
	out, err := h.service.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Get(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid JSON request body"))
		return
	}
	out, err := h.service.Update(r.Context(), serviceID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), serviceID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	out, err := h.service.Validate(r.Context(), serviceID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) EnablePreview(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.EnablePreview(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Enable(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, out)
}

func (h *Handler) DisablePreview(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.DisablePreview(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Disable(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, out)
}

func (h *Handler) Reconcile(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Reconcile(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID, "operationId": task.OperationID})
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Restart(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID, "operationId": task.OperationID})
}

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Runtime(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	out, err := h.service.Logs(r.Context(), serviceID(r.URL.Path), tail)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"lines": out})
}

func (h *Handler) Placeholder(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{})
}

func serviceID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
