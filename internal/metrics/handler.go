package metrics

import (
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	series, err := h.service.Query(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/metrics")), r.URL.Query().Get("range"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, series)
}

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
