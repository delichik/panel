package sshaccess

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

// RegisterRoutes 注册服务器 SSH 访问记录的 2 条只读路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/servers/{id}/ssh-access/sessions", auth(http.HandlerFunc(h.Sessions)))
	mux.Handle("GET /api/v1/servers/{id}/ssh-access/attempts", auth(http.HandlerFunc(h.Attempts)))
}
