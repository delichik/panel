package diagnostics

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/debug/runtime", auth(http.HandlerFunc(h.Runtime)))
	mux.Handle("GET /api/v1/debug/tasks", auth(http.HandlerFunc(h.Tasks)))
	mux.Handle("GET /api/v1/debug/databases", auth(http.HandlerFunc(h.Databases)))
	mux.Handle("GET /api/v1/debug/pprof", auth(http.HandlerFunc(h.PprofStatus)))
	mux.Handle("PUT /api/v1/debug/pprof", auth(http.HandlerFunc(h.UpdatePprof)))
	mux.Handle("POST /api/v1/debug/clear-runtime-data", auth(http.HandlerFunc(h.ClearRuntimeData)))
	mux.Handle("GET /api/v1/debug/clear-runtime-data", auth(http.HandlerFunc(h.ClearRuntimeDataStatus)))
}
