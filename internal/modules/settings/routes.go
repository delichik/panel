package settings

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/public-branding", h.PublicBranding)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/settings/runtime", auth(http.HandlerFunc(h.Runtime)))
	// 设置写入按分区拆分为独立子资源（SET-RUN-022）。整体 PUT
	// /api/v1/settings/runtime 已删除，不保留兼容别名：自动保存只提交当前
	// 分区，避免覆盖其它分区并发修改的值。
	mux.Handle("PUT /api/v1/settings/runtime/general", auth(http.HandlerFunc(h.UpdateGeneral)))
	mux.Handle("PUT /api/v1/settings/runtime/security", auth(http.HandlerFunc(h.UpdateSecurity)))
	mux.Handle("PUT /api/v1/settings/runtime/certificates", auth(http.HandlerFunc(h.UpdateCertificates)))
	mux.Handle("PUT /api/v1/settings/runtime/agent", auth(http.HandlerFunc(h.UpdateAgent)))
	mux.Handle("PUT /api/v1/settings/runtime/branding", auth(http.HandlerFunc(h.UpdateBranding)))
	mux.Handle("GET /api/v1/settings/server-variables", auth(http.HandlerFunc(h.ServerVariableDefinitions)))
	mux.Handle("PUT /api/v1/settings/server-variables", auth(http.HandlerFunc(h.UpdateServerVariableDefinitions)))
}
