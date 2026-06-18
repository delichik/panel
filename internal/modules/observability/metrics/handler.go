package metrics

import (
	"net/http"
	"strings"

	"panel/internal/platform/http"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	series, err := h.service.Query(r.Context(), serverIDFromRequest(r), r.URL.Query().Get("range"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, series)
}

func serverIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
