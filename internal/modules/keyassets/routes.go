package keyassets

import (
	"net/http"
	"strings"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/key-assets", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/key-assets/tls", auth(http.HandlerFunc(h.ListTLSCertificates)))
	mux.Handle("POST /api/v1/key-assets/ca", auth(http.HandlerFunc(h.CreateCA)))
	mux.Handle("POST /api/v1/key-assets/tls", auth(http.HandlerFunc(h.CreateTLS)))
	mux.Handle("POST /api/v1/key-assets/ssh/generate", auth(http.HandlerFunc(h.GenerateSSH)))
	mux.Handle("POST /api/v1/key-assets/import", auth(http.HandlerFunc(h.Import)))
	mux.Handle("POST /api/v1/key-assets/exports", auth(http.HandlerFunc(h.CreateExport)))
	mux.Handle("POST /api/v1/key-assets/imports/preflight", auth(http.HandlerFunc(h.PreflightImport)))
	mux.Handle("POST /api/v1/key-assets/imports/{planId}/execute", auth(http.HandlerFunc(h.ExecuteImport)))
	mux.Handle("GET /api/v1/key-assets/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("DELETE /api/v1/key-assets/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/v1/key-assets/{downloadPath...}", auth(http.HandlerFunc(h.Download)))
	mux.Handle("POST /api/v1/key-assets/{id}/reissue", auth(http.HandlerFunc(h.Reissue)))
	mux.Handle("POST /api/v1/key-assets/{id}/regenerate", auth(http.HandlerFunc(h.Regenerate)))
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	route, first, second := keyAssetDownloadRoute(r.PathValue("downloadPath"))
	switch route {
	case keyAssetExportDownload:
		r.SetPathValue("taskId", first)
		h.DownloadExport(w, r)
	case keyAssetFileDownload:
		r.SetPathValue("id", first)
		r.SetPathValue("kind", second)
		h.DownloadFile(w, r)
	default:
		http.NotFound(w, r)
	}
}

type keyAssetDownloadKind uint8

const (
	keyAssetDownloadNotFound keyAssetDownloadKind = iota
	keyAssetFileDownload
	keyAssetExportDownload
)

func keyAssetDownloadRoute(path string) (keyAssetDownloadKind, string, string) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	switch {
	case len(parts) == 3 && parts[0] == "exports" && parts[1] != "" && parts[2] == "download":
		return keyAssetExportDownload, parts[1], ""
	case len(parts) == 3 && parts[0] != "" && parts[1] == "files" && parts[2] != "":
		return keyAssetFileDownload, parts[0], parts[2]
	default:
		return keyAssetDownloadNotFound, "", ""
	}
}
