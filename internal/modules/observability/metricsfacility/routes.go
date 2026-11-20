package metricsfacility

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

// RegisterRoutes 注册指标设施的 2 条 HTTP 路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/facility-apps/metrics", auth(http.HandlerFunc(h.Config)))
	mux.Handle("PUT /api/v1/facility-apps/metrics", auth(http.HandlerFunc(h.SaveConfig)))
}
