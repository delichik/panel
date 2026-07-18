package facilityapps

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy", auth(http.HandlerFunc(h.ReverseProxy)))
	mux.Handle("PUT /api/v1/facility-apps/reverse-proxy", auth(http.HandlerFunc(h.SaveReverseProxy)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/reconcile", auth(http.HandlerFunc(h.ReconcileReverseProxy)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/save-sessions", auth(http.HandlerFunc(h.BeginSaveSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/save-sessions/{id}/assets", auth(http.HandlerFunc(h.UploadSaveSessionAsset)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/save-sessions/{id}/assets/delete", auth(http.HandlerFunc(h.DeleteSaveSessionAsset)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/save-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitSaveSession)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/save-sessions/{id}", auth(http.HandlerFunc(h.DiscardSaveSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions", auth(http.HandlerFunc(h.BeginFacilityEditSession)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/edit-sessions/recoverable", auth(http.HandlerFunc(h.RecoverableFacilityEditSessions)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}", auth(http.HandlerFunc(h.GetFacilityEditSession)))
	mux.Handle("PATCH /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/draft", auth(http.HandlerFunc(h.PatchFacilityEditSession)))
	mux.Handle("PUT /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetKey}", auth(http.HandlerFunc(h.PutFacilityEditAsset)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetKey}", auth(http.HandlerFunc(h.DeleteFacilityEditAsset)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/validate", auth(http.HandlerFunc(h.ValidateFacilityEditSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/preview", auth(http.HandlerFunc(h.PreviewFacilityEditSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitFacilityEditSession)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}", auth(http.HandlerFunc(h.DiscardFacilityEditSession)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/static-assets", auth(http.HandlerFunc(h.StaticAssets)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/static-assets", auth(http.HandlerFunc(h.UploadStaticAsset)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/static-assets/{assetId}", auth(http.HandlerFunc(h.DeleteStaticAsset)))
}
