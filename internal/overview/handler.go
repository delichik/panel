package overview

import (
	"net/http"

	"panel/internal/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Get(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
