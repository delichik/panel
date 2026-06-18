package overview

import (
	"net/http"
	"strings"

	"panel/internal/platform/http"
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

func (h *Handler) GetCards(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.GetCards(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateCards(w http.ResponseWriter, r *http.Request) {
	var input CardConfigurationSet
	if !httpx.Decode(w, r, &input) {
		return
	}
	out, err := h.service.UpdateCards(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) GetCardData(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.GetCardData(r.Context(), cardDataIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func cardDataIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("cardId"))
}
