package settings

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/public-branding", h.PublicBranding)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/settings/runtime", auth(http.HandlerFunc(h.Runtime)))
	mux.Handle("PUT /api/v1/settings/runtime", auth(http.HandlerFunc(h.UpdateRuntime)))
	mux.Handle("GET /api/v1/settings/server-variables", auth(http.HandlerFunc(h.ServerVariableDefinitions)))
	mux.Handle("PUT /api/v1/settings/server-variables", auth(http.HandlerFunc(h.UpdateServerVariableDefinitions)))
}
