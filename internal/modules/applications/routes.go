package applications

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("POST /api/v1/application-edit-sessions", auth(http.HandlerFunc(h.BeginEditSession)))
	mux.Handle("PATCH /api/v1/application-edit-sessions/{id}/draft", auth(http.HandlerFunc(h.PatchEditSession)))
	mux.Handle("GET /api/v1/application-edit-sessions/{id}/files/{name}", auth(http.HandlerFunc(h.GetEditSessionFile)))
	mux.Handle("GET /api/v1/application-edit-sessions/{id}/files/{name}/content", auth(http.HandlerFunc(h.DownloadEditSessionFile)))
	mux.Handle("PUT /api/v1/application-edit-sessions/{id}/files/{name}", auth(http.HandlerFunc(h.PutEditSessionFile)))
	mux.Handle("PUT /api/v1/application-edit-sessions/{id}/uploads/{name}", auth(http.HandlerFunc(h.UploadEditSessionBinary)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/archives", auth(http.HandlerFunc(h.UploadEditSessionArchive)))
	mux.Handle("DELETE /api/v1/application-edit-sessions/{id}/files/{name}", auth(http.HandlerFunc(h.DeleteEditSessionFile)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/validate", auth(http.HandlerFunc(h.ValidateEditSession)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/preview", auth(http.HandlerFunc(h.PreviewEditSession)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitEditSession)))
	mux.Handle("DELETE /api/v1/application-edit-sessions/{id}", auth(http.HandlerFunc(h.DiscardEditSession)))

	mux.Handle("GET /api/v1/application-operations", auth(http.HandlerFunc(h.ListApplicationOperationRecords)))
	mux.Handle("GET /api/v1/application-operations/{id}", auth(http.HandlerFunc(h.GetApplicationOperationRecord)))

	mux.Handle("GET /api/v1/applications", auth(http.HandlerFunc(h.List)))
	mux.Handle("GET /api/v1/applications/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("DELETE /api/v1/applications/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/v1/applications/{id}/files", auth(http.HandlerFunc(h.ListFiles)))
	mux.Handle("GET /api/v1/applications/{id}/files/{name}/content", auth(http.HandlerFunc(h.DownloadFile)))
	mux.Handle("GET /api/v1/applications/{id}/persistent-data", auth(http.HandlerFunc(h.PersistentData)))
	mux.Handle("POST /api/v1/applications/{id}/persistent-data", auth(http.HandlerFunc(h.RestorePersistentData)))

	mux.Handle("POST /api/v1/applications/{id}/image/update", auth(http.HandlerFunc(h.UpdateImage)))
	mux.Handle("POST /api/v1/applications/{id}/deploy", auth(http.HandlerFunc(h.Deploy)))
	mux.Handle("POST /api/v1/applications/{id}/stop", auth(http.HandlerFunc(h.Stop)))
	mux.Handle("POST /api/v1/applications/{id}/restart", auth(http.HandlerFunc(h.Restart)))
	mux.Handle("GET /api/v1/applications/{id}/runtime", auth(http.HandlerFunc(h.Runtime)))
	mux.Handle("GET /api/v1/applications/{id}/logs", auth(http.HandlerFunc(h.Logs)))
}
