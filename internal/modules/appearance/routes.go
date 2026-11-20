package appearance

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

// RegisterPublicRoutes 注册公开外观路由（登录页使用，main API manifest 仍前缀
// /api/，不计入公开产物路由清单）。
func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/public-appearance", h.PublicAppearance)
}

// RegisterRoutes 注册认证外观路由（5 条，与公开接口合计 6 条）。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/settings/appearance", auth(http.HandlerFunc(h.GetAppearance)))
	mux.Handle("PUT /api/v1/settings/appearance", auth(http.HandlerFunc(h.UpdateAppearance)))
	mux.Handle("POST /api/v1/settings/appearance/background", auth(http.HandlerFunc(h.UploadBackground)))
	mux.Handle("DELETE /api/v1/settings/appearance/background", auth(http.HandlerFunc(h.DeleteBackground)))
	mux.Handle("GET /api/v1/settings/appearance/background/content", auth(http.HandlerFunc(h.BackgroundContent)))
}
