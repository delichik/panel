package diagnostics

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/debug/snapshot", auth(http.HandlerFunc(h.Snapshot)))
	mux.Handle("GET /api/v1/debug/pprof", auth(http.HandlerFunc(h.PprofStatus)))
	mux.Handle("PUT /api/v1/debug/pprof", auth(http.HandlerFunc(h.UpdatePprof)))
}
