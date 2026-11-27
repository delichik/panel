package dns

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/dns/domains", auth(http.HandlerFunc(h.ListDomains)))
	mux.Handle("POST /api/v1/dns/domains", auth(http.HandlerFunc(h.CreateDomain)))
	mux.Handle("PUT /api/v1/dns/domains/{domainId}", auth(http.HandlerFunc(h.UpdateDomain)))
	mux.Handle("DELETE /api/v1/dns/domains/{domainId}", auth(http.HandlerFunc(h.DeleteDomain)))
	mux.Handle("GET /api/v1/dns/domains/{domainId}/records", auth(http.HandlerFunc(h.ListRecords)))
	mux.Handle("POST /api/v1/dns/domains/{domainId}/records/refresh", auth(http.HandlerFunc(h.RefreshRecords)))
	mux.Handle("POST /api/v1/dns/domains/{domainId}/records", auth(http.HandlerFunc(h.CreateRecord)))
	mux.Handle("PUT /api/v1/dns/domains/{domainId}/records/{recordId}", auth(http.HandlerFunc(h.UpdateRecord)))
	mux.Handle("DELETE /api/v1/dns/domains/{domainId}/records/{recordId}", auth(http.HandlerFunc(h.DeleteRecord)))
}
