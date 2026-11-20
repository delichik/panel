package networkprobe

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

// RegisterRoutes 注册网络探测设施的 4 条 HTTP 路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/facility-apps/network-probe", auth(http.HandlerFunc(h.Config)))
	mux.Handle("PUT /api/v1/facility-apps/network-probe", auth(http.HandlerFunc(h.SaveConfig)))
	mux.Handle("GET /api/v1/facility-apps/network-probe/results", auth(http.HandlerFunc(h.Results)))
	mux.Handle("GET /api/v1/servers/{id}/network-probes", auth(http.HandlerFunc(h.ServerResults)))
}
