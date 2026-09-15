package packages

import (
	"net/http"
	"strings"

	"panel/internal/platform/http"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.List(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Refresh(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, out)
}

func (h *Handler) UpgradeSelected(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Packages []string `json:"packages"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.service.UpgradeSelected(r.Context(), serverIDFromRequest(r), req.Packages)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UpgradeAll(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.UpgradeAll(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func serverIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
