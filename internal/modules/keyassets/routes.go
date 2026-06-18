package keyassets

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/key-assets", auth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/key-assets/ca", auth(http.HandlerFunc(h.CreateCA)))
	mux.Handle("POST /api/v1/key-assets/tls", auth(http.HandlerFunc(h.CreateTLS)))
	mux.Handle("POST /api/v1/key-assets/ssh/generate", auth(http.HandlerFunc(h.GenerateSSH)))
	mux.Handle("POST /api/v1/key-assets/import", auth(http.HandlerFunc(h.Import)))
	mux.Handle("POST /api/v1/key-assets/exports", auth(http.HandlerFunc(h.CreateExport)))
	mux.Handle("GET /api/v1/key-assets/exports/{taskId}/download", auth(http.HandlerFunc(h.DownloadExport)))
	mux.Handle("POST /api/v1/key-assets/imports/preflight", auth(http.HandlerFunc(h.PreflightImport)))
	mux.Handle("POST /api/v1/key-assets/imports/{planId}/execute", auth(http.HandlerFunc(h.ExecuteImport)))
	mux.Handle("GET /api/v1/key-assets/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("DELETE /api/v1/key-assets/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/v1/key-assets/{id}/files/{kind}", auth(http.HandlerFunc(h.DownloadFile)))
	mux.Handle("POST /api/v1/key-assets/{id}/reissue", auth(http.HandlerFunc(h.Reissue)))
	mux.Handle("POST /api/v1/key-assets/{id}/regenerate", auth(http.HandlerFunc(h.Regenerate)))
}
