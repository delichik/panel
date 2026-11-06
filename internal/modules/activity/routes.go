package activity

import "net/http"

// RegisterRoutes previously mounted /api/v1/activity/* product routes.
// Those endpoints are retired; process log streams are served by
// /api/v1/events and /api/v1/logs (see internal/modules/events). The Activity
// Service remains for append/audit and internal projection use.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	_ = mux
	_ = auth
	_ = h
}
