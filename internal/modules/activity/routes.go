package activity

import "net/http"

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/activity/events", auth(http.HandlerFunc(h.Events)))
	mux.Handle("GET /api/v1/activity/events/{id}", auth(http.HandlerFunc(h.Event)))
	mux.Handle("GET /api/v1/activity/events/{id}/context", auth(http.HandlerFunc(h.Context)))
	mux.Handle("GET /api/v1/activity/operations", auth(http.HandlerFunc(h.Operations)))
	mux.Handle("GET /api/v1/activity/operations/{id}", auth(http.HandlerFunc(h.Operation)))
	mux.Handle("GET /api/v1/activity/tail", auth(http.HandlerFunc(h.Tail)))
	mux.Handle("GET /api/v1/activity/summary", auth(http.HandlerFunc(h.Summary)))
	mux.Handle("GET /api/v1/activity/export", auth(http.HandlerFunc(h.Export)))
	mux.Handle("GET /api/v1/activity/evidence/{id}", auth(http.HandlerFunc(h.Evidence)))
}
