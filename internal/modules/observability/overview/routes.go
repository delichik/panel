package overview

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/overview", auth(http.HandlerFunc(h.Get)))
	mux.Handle("GET /api/v1/overview/cards", auth(http.HandlerFunc(h.GetCards)))
	mux.Handle("PUT /api/v1/overview/cards", auth(http.HandlerFunc(h.UpdateCards)))
	mux.Handle("GET /api/v1/overview/cards/{cardId}/data", auth(http.HandlerFunc(h.GetCardData)))
}
