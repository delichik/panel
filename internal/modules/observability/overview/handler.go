package overview

import (
	"net/http"
	"strings"
	"time"

	"panel/internal/platform/http"
	panelerr "panel/internal/platform/errors"
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
	since, ok := sinceFromRequest(w, r)
	if !ok {
		return
	}
	out, err := h.service.GetCardDataSince(r.Context(), cardDataIDFromRequest(r), since)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func sinceFromRequest(w http.ResponseWriter, r *http.Request) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("since"))
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		httpx.Error(w, panelerr.Validation("since_invalid", "since must be an RFC3339 timestamp"))
		return nil, false
	}
	return &parsed, true
}

func cardDataIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("cardId"))
}
