package certs

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/certificates", auth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/certificates", auth(http.HandlerFunc(h.Issue)))
	mux.Handle("POST /api/v1/certificates/{id}/renew", auth(http.HandlerFunc(h.Renew)))
	mux.Handle("DELETE /api/v1/certificates/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/v1/self-signed-certificates", auth(http.HandlerFunc(h.ListSelfSigned)))
	mux.Handle("POST /api/v1/self-signed-cas", auth(http.HandlerFunc(h.CreateSelfSignedCA)))
	mux.Handle("POST /api/v1/self-signed-certificates", auth(http.HandlerFunc(h.CreateSelfSignedLeaf)))
	mux.Handle("POST /api/v1/self-signed-certificates/{id}/renew", auth(http.HandlerFunc(h.RenewSelfSignedLeaf)))
	mux.Handle("DELETE /api/v1/self-signed-certificates/{id}", auth(http.HandlerFunc(h.DeleteSelfSigned)))
}
