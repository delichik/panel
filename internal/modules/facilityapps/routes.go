package facilityapps

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy", auth(http.HandlerFunc(h.ReverseProxy)))
	mux.Handle("PUT /api/v1/facility-apps/reverse-proxy", auth(http.HandlerFunc(h.SaveReverseProxy)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/reconcile", auth(http.HandlerFunc(h.ReconcileReverseProxy)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/static-assets", auth(http.HandlerFunc(h.StaticAssets)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/static-assets", auth(http.HandlerFunc(h.UploadStaticAsset)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/static-assets/{assetId}", auth(http.HandlerFunc(h.DeleteStaticAsset)))
}
