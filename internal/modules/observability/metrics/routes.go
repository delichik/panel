package metrics

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/servers/{id}/metrics", auth(http.HandlerFunc(h.Query)))
}
