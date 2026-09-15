package backups

import "net/http"

func (h *Handler) RegisterRoutes(mux *http.ServeMux, authenticated func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/backups/export", authenticated(http.HandlerFunc(h.StartExport)))
	mux.Handle("POST /api/v1/backups/restore/preflight", authenticated(http.HandlerFunc(h.PreflightRestore)))
	mux.Handle("POST /api/v1/backups/restore/confirm", authenticated(http.HandlerFunc(h.ConfirmRestore)))
}
