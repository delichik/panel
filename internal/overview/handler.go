package overview

import (
	"net/http"
	"strings"

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
	out, err := h.service.GetCardData(r.Context(), cardDataID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func cardDataID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "overview" && parts[3] == "cards" && parts[5] == "data" {
		return parts[4]
	}
	return ""
}
