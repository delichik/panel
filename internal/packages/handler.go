package packages

import (
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.List(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/packages/updates")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Refresh(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/packages/refresh")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UpgradeSelected(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Packages []string `json:"packages"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.service.UpgradeSelected(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/packages/upgrade-selected")), req.Packages)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UpgradeAll(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.UpgradeAll(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/packages/upgrade-all")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
