package facilityapps

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy", auth(http.HandlerFunc(h.ReverseProxy)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/reconcile", auth(http.HandlerFunc(h.ReconcileReverseProxy)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions", auth(http.HandlerFunc(h.BeginFacilityEditSession)))
	mux.Handle("PATCH /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/draft", auth(http.HandlerFunc(h.PatchFacilityEditSession)))
	mux.Handle("PUT /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}", auth(http.HandlerFunc(h.PutFacilityEditAsset)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}/content", auth(http.HandlerFunc(h.DownloadFacilityEditAsset)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}", auth(http.HandlerFunc(h.DeleteFacilityEditAsset)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/validate", auth(http.HandlerFunc(h.ValidateFacilityEditSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/preview", auth(http.HandlerFunc(h.PreviewFacilityEditSession)))
	mux.Handle("POST /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitFacilityEditSession)))
	mux.Handle("DELETE /api/v1/facility-apps/reverse-proxy/edit-sessions/{id}", auth(http.HandlerFunc(h.DiscardFacilityEditSession)))
	mux.Handle("GET /api/v1/facility-apps/reverse-proxy/static-assets/{assetName}/content", auth(http.HandlerFunc(h.DownloadStaticAsset)))
	mux.Handle("GET /api/v1/facility-apps/storage-share", auth(http.HandlerFunc(h.StorageShare)))
	mux.Handle("PUT /api/v1/facility-apps/storage-share", auth(http.HandlerFunc(h.SaveStorageShare)))
	mux.Handle("POST /api/v1/facility-apps/storage-share/reconcile", auth(http.HandlerFunc(h.ReconcileStorageShare)))
	mux.Handle("DELETE /api/v1/facility-apps/storage-share", auth(http.HandlerFunc(h.DeleteStorageShare)))
	mux.Handle("GET /api/v1/facility-apps/storage-share/partitions/{id}/download", auth(http.HandlerFunc(h.DownloadStoragePartition)))
	mux.Handle("DELETE /api/v1/facility-apps/storage-share/partitions/{id}", auth(http.HandlerFunc(h.DeleteStoragePartition)))
}
