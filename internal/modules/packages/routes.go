package packages

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/servers/{id}/packages/updates", auth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/servers/{id}/packages/refresh", auth(http.HandlerFunc(h.Refresh)))
	mux.Handle("POST /api/v1/servers/{id}/packages/upgrade-selected", auth(http.HandlerFunc(h.UpgradeSelected)))
	mux.Handle("POST /api/v1/servers/{id}/packages/upgrade-all", auth(http.HandlerFunc(h.UpgradeAll)))
}
