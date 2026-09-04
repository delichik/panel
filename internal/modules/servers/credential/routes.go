package credential

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/credentials", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/credentials/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("POST /api/v1/credentials", auth(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/credentials/{id}", auth(http.HandlerFunc(h.Update)))
	mux.Handle("DELETE /api/v1/credentials/{id}", auth(http.HandlerFunc(h.Delete)))
}
