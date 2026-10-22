package activity

import "net/http"

// RegisterRoutes previously mounted /api/v1/activity/* product routes.
// Those endpoints are retired; process log streams are served by
// /api/v1/log-contexts (see internal/modules/logcontexts). The Activity
// Service remains for append/audit and internal projection use.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	_ = mux
	_ = auth
	_ = h
}
