package logcontexts

import "net/http"

// RegisterRoutes mounts authenticated product routes for log contexts.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/log-contexts", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/log-contexts/export", auth(http.HandlerFunc(h.Export)))
	mux.Handle("GET /api/v1/log-contexts/{streamId}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("GET /api/v1/log-contexts/{streamId}/lines", auth(http.HandlerFunc(h.Lines)))
}
