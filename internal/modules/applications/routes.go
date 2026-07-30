package applications

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("POST /api/v1/application-edit-sessions", auth(http.HandlerFunc(h.BeginEditSession)))
	mux.Handle("GET /api/v1/application-edit-sessions/recoverable", auth(http.HandlerFunc(h.RecoverableEditSessions)))
	mux.Handle("GET /api/v1/application-edit-sessions/{id}", auth(http.HandlerFunc(h.GetEditSession)))
	mux.Handle("PATCH /api/v1/application-edit-sessions/{id}/draft", auth(http.HandlerFunc(h.PatchEditSession)))
	mux.Handle("GET /api/v1/application-edit-sessions/{id}/files/{fileKey}", auth(http.HandlerFunc(h.GetEditSessionFile)))
	mux.Handle("GET /api/v1/application-edit-sessions/{id}/files/{fileKey}/content", auth(http.HandlerFunc(h.DownloadEditSessionFile)))
	mux.Handle("PUT /api/v1/application-edit-sessions/{id}/files/{fileKey}", auth(http.HandlerFunc(h.PutEditSessionFile)))
	mux.Handle("PUT /api/v1/application-edit-sessions/{id}/uploads/{fileKey}", auth(http.HandlerFunc(h.UploadEditSessionBinary)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/archives", auth(http.HandlerFunc(h.UploadEditSessionArchive)))
	mux.Handle("DELETE /api/v1/application-edit-sessions/{id}/files/{fileKey}", auth(http.HandlerFunc(h.DeleteEditSessionFile)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/validate", auth(http.HandlerFunc(h.ValidateEditSession)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/preview", auth(http.HandlerFunc(h.PreviewEditSession)))
	mux.Handle("POST /api/v1/application-edit-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitEditSession)))
	mux.Handle("DELETE /api/v1/application-edit-sessions/{id}", auth(http.HandlerFunc(h.DiscardEditSession)))

	mux.Handle("POST /api/v1/application-save-sessions", auth(http.HandlerFunc(h.BeginSaveSession)))
	mux.Handle("POST /api/v1/application-save-sessions/{id}/files", auth(http.HandlerFunc(h.UploadSaveSessionFile)))
	mux.Handle("POST /api/v1/application-save-sessions/{id}/files/archive", auth(http.HandlerFunc(h.UploadSaveSessionArchive)))
	mux.Handle("POST /api/v1/application-save-sessions/{id}/files/delete", auth(http.HandlerFunc(h.DeleteSaveSessionFile)))
	mux.Handle("POST /api/v1/application-save-sessions/{id}/commit", auth(http.HandlerFunc(h.CommitSaveSession)))

	mux.Handle("GET /api/v1/applications", auth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/applications", auth(http.HandlerFunc(h.Create)))
	mux.Handle("GET /api/v1/application-template-catalog", auth(http.HandlerFunc(h.TemplateCatalog)))
	mux.Handle("GET /api/v1/applications/{id}", auth(http.HandlerFunc(h.Get)))
	mux.Handle("PUT /api/v1/applications/{id}", auth(http.HandlerFunc(h.Update)))
	mux.Handle("DELETE /api/v1/applications/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/v1/applications/{id}/files", auth(http.HandlerFunc(h.ListFiles)))
	mux.Handle("POST /api/v1/applications/{id}/files", auth(http.HandlerFunc(h.SaveFile)))
	mux.Handle("GET /api/v1/applications/{id}/files/{fileId}", auth(http.HandlerFunc(h.GetFile)))
	mux.Handle("GET /api/v1/applications/{id}/files/{fileId}/content", auth(http.HandlerFunc(h.DownloadFile)))
	mux.Handle("DELETE /api/v1/applications/{id}/files/{fileId}", auth(http.HandlerFunc(h.DeleteFile)))
	mux.Handle("GET /api/v1/applications/{id}/package", auth(http.HandlerFunc(h.Package)))
	mux.Handle("GET /api/v1/applications/{id}/persistent-data", auth(http.HandlerFunc(h.PersistentData)))
	mux.Handle("POST /api/v1/applications/{id}/persistent-data", auth(http.HandlerFunc(h.RestorePersistentData)))
	mux.Handle("POST /api/v1/applications/{id}/validate", auth(http.HandlerFunc(h.Validate)))
	mux.Handle("POST /api/v1/applications/{id}/plan", auth(http.HandlerFunc(h.Plan)))
	mux.Handle("POST /api/v1/applications/{id}/image/check", auth(http.HandlerFunc(h.CheckImageUpdate)))
	mux.Handle("POST /api/v1/applications/{id}/image/update", auth(http.HandlerFunc(h.UpdateImage)))
	mux.Handle("POST /api/v1/applications/{id}/deploy", auth(http.HandlerFunc(h.Deploy)))
	mux.Handle("POST /api/v1/applications/{id}/migrate", auth(http.HandlerFunc(h.Migrate)))
	mux.Handle("POST /api/v1/applications/{id}/stop", auth(http.HandlerFunc(h.Stop)))
	mux.Handle("POST /api/v1/applications/{id}/restart", auth(http.HandlerFunc(h.Restart)))
	mux.Handle("GET /api/v1/applications/{id}/runtime", auth(http.HandlerFunc(h.Runtime)))
	mux.Handle("GET /api/v1/applications/{id}/logs", auth(http.HandlerFunc(h.Logs)))
}
