package tasks

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/tasks", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/tasks/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("GET /api/v1/tasks/{id}/logs", auth(http.HandlerFunc(h.Logs)))
	mux.Handle("GET /api/v1/tasks/{id}/steps", auth(http.HandlerFunc(h.Steps)))
	mux.Handle("POST /api/v1/tasks/{id}/retry", auth(http.HandlerFunc(h.Retry)))
	mux.Handle("POST /api/v1/tasks/{id}/run-now", auth(http.HandlerFunc(h.RunNow)))
}
