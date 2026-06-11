package settings

import (
	"net/http"

	"panel/internal/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Runtime())
}

func (h *Handler) PublicBranding(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Runtime().Branding)
}

func (h *Handler) UpdateRuntime(w http.ResponseWriter, r *http.Request) {
	var input RuntimeUpdate
	if !httpx.Decode(w, r, &input) {
		return
	}
	settings, err := h.service.Update(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}
