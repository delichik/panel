package systeminfo

import (
	"net/http"

	"panel/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Version(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Version())
}
