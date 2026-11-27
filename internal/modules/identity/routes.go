package auth

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

// RegisterRoutes 注册权限目录、用户与角色管理接口。调用方必须传入已认证
// 中间件；具体权限由统一 PermissionGate 按路径判定。
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/permissions", auth(http.HandlerFunc(h.PermissionCatalog)))

	mux.Handle("GET /api/v1/users", auth(http.HandlerFunc(h.ListUsers)))
	mux.Handle("POST /api/v1/users", auth(http.HandlerFunc(h.CreateUser)))
	mux.Handle("GET /api/v1/users/{id}", auth(http.HandlerFunc(h.GetUser)))
	mux.Handle("PATCH /api/v1/users/{id}", auth(http.HandlerFunc(h.UpdateUser)))
	mux.Handle("DELETE /api/v1/users/{id}", auth(http.HandlerFunc(h.DeleteUser)))
	mux.Handle("POST /api/v1/users/{id}/password", auth(http.HandlerFunc(h.ResetUserPassword)))
	mux.Handle("POST /api/v1/users/{id}/logout", auth(http.HandlerFunc(h.ForceUserLogout)))

	mux.Handle("GET /api/v1/roles", auth(http.HandlerFunc(h.ListRoles)))
	mux.Handle("POST /api/v1/roles", auth(http.HandlerFunc(h.CreateRole)))
	mux.Handle("GET /api/v1/roles/{id}", auth(http.HandlerFunc(h.GetRole)))
	mux.Handle("PATCH /api/v1/roles/{id}", auth(http.HandlerFunc(h.UpdateRole)))
	mux.Handle("DELETE /api/v1/roles/{id}", auth(http.HandlerFunc(h.DeleteRole)))

	mux.Handle("GET /api/v1/access-credentials", auth(http.HandlerFunc(h.ListAccessCredentials)))
	mux.Handle("POST /api/v1/access-credentials", auth(http.HandlerFunc(h.CreateAccessCredential)))
	mux.Handle("DELETE /api/v1/access-credentials/{id}", auth(http.HandlerFunc(h.RevokeAccessCredential)))
}
