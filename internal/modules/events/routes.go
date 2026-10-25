package events

import "net/http"

// RegisterRoutes mounts authenticated product routes for events and logs.
// Legacy /api/v1/log-contexts* paths remain as short-lived aliases.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/events", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/events/export", auth(http.HandlerFunc(h.Export)))
	mux.Handle("GET /api/v1/events/{eventId}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("GET /api/v1/events/{eventId}/logs", auth(http.HandlerFunc(h.EventLogs)))
	mux.Handle("GET /api/v1/events/{eventId}/lines", auth(http.HandlerFunc(h.EventLogs))) // alias
	mux.Handle("GET /api/v1/logs", auth(http.HandlerFunc(h.Logs)))

	mux.Handle("GET /api/v1/log-contexts", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/log-contexts/export", auth(http.HandlerFunc(h.Export)))
	mux.Handle("GET /api/v1/log-contexts/{streamId}", auth(http.HandlerFunc(h.GetLegacy)))
	mux.Handle("GET /api/v1/log-contexts/{streamId}/lines", auth(http.HandlerFunc(h.LinesLegacy)))
}
