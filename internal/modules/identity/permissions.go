package auth

import (
	"net/http"
	"strings"

	"panel/internal/modules/identity/permissioncatalog"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

// universal routes are authenticated but do not require a permission key.
var universalRoutes = map[string]struct{}{
	"GET /api/v1/notifications":   {},
	"GET /api/v1/system/version":  {},
	"GET /api/v1/permissions":     {},
	"GET /api/v1/auth/session":    {},
	"POST /api/v1/auth/logout":    {},
	"POST /api/v1/auth/account":   {},
	"POST /api/v1/auth/jwt-secret": {},
}

// RequiredPermission 返回路由所需的权限键。第二个返回值表示路由是否已被
// 权限目录覆盖；false 时调用方必须 fail-closed。
//
// 规则按“更具体的路径优先”排列。这里是 API 到权限的唯一后端映射，目录测试
// 会枚举所有已注册路由并断言它们都能解析。
func RequiredPermission(method, path string) (string, bool) {
	key := method + " " + path
	if _, ok := universalRoutes[key]; ok {
		return "", true
	}
	// 自助访问凭证：创建/列表/撤销只作用于本人，由 handler 再做归属校验。
	if strings.HasPrefix(path, "/api/v1/access-credentials") {
		return "", true
	}
	if strings.HasPrefix(path, "/api/v1/setup/") || path == "/api/v1/setup" {
		return "", true
	}

	if path == "/api/v1/overview" || strings.HasPrefix(path, "/api/v1/overview/") {
		return "overview.view", true
	}

	if strings.HasPrefix(path, "/api/v1/servers/") {
		switch {
		case strings.Contains(path, "/containers"):
			if method == "GET" {
				return "containers.view", true
			}
			return "containers.manage", true
		case strings.Contains(path, "/images"):
			if method == "GET" {
				return "containers.view", true
			}
			return "containers.manage", true
		case strings.Contains(path, "/networks"):
			if method == "GET" {
				return "containers.view", true
			}
			return "containers.manage", true
		case strings.Contains(path, "/volumes"):
			if method == "GET" {
				return "containers.view", true
			}
			return "containers.manage", true
		case strings.HasSuffix(path, "/ufw/rules") || strings.Contains(path, "/ufw/rules/"):
			return "servers.execute", true
		case strings.HasSuffix(path, "/ufw"):
			if method == "GET" {
				return "servers.view", true
			}
			return "servers.execute", true
		case strings.HasSuffix(path, "/fail2ban"):
			if method == "GET" {
				return "servers.view", true
			}
			return "servers.execute", true
		case strings.Contains(path, "/fail2ban/"):
			return "servers.execute", true
		case strings.Contains(path, "/packages/"):
			if method == "GET" {
				return "servers.view", true
			}
			return "servers.execute", true
		case strings.Contains(path, "/agent/"):
			return "servers.execute", true
		case strings.HasSuffix(path, "/restart"):
			return "servers.execute", true
		case strings.HasSuffix(path, "/test") || strings.HasSuffix(path, "/trust-host-key"):
			return "servers.manage", true
		case strings.HasSuffix(path, "/metrics") || strings.HasSuffix(path, "/network-probes"):
			return "servers.view", true
		case method == "GET":
			return "servers.view", true
		default:
			return "servers.manage", true
		}
	}
	if path == "/api/v1/servers" || path == "/api/v1/servers/probe" {
		if method == "GET" {
			return "servers.view", true
		}
		return "servers.manage", true
	}
	if strings.HasPrefix(path, "/api/v1/images/") {
		return "containers.manage", true
	}

	if strings.HasPrefix(path, "/api/v1/application-edit-sessions") {
		return "applications.edit", true
	}
	if strings.HasPrefix(path, "/api/v1/applications") {
		if method == "GET" {
			return "applications.view", true
		}
		if strings.Contains(path, "/deploy") || strings.Contains(path, "/restart") ||
			strings.Contains(path, "/stop") || strings.Contains(path, "/image/") {
			return "applications.deploy", true
		}
		return "applications.edit", true
	}

	if strings.HasPrefix(path, "/api/v1/facility-apps") {
		if method == "GET" {
			return "facilities.view", true
		}
		return "facilities.manage", true
	}

	if path == "/api/v1/credentials" || strings.HasPrefix(path, "/api/v1/credentials/") {
		if method == "GET" {
			return "servers.view", true
		}
		return "servers.credentials.manage", true
	}

	if strings.HasPrefix(path, "/api/v1/dns/") {
		if method == "GET" {
			return "dns.view", true
		}
		return "dns.manage", true
	}
	if strings.HasPrefix(path, "/api/v1/certificates") {
		if method == "GET" {
			return "certificates.view", true
		}
		return "certificates.manage", true
	}
	if strings.HasPrefix(path, "/api/v1/self-signed-") {
		if method == "GET" {
			return "certificates.view", true
		}
		return "certificates.manage", true
	}

	if strings.HasPrefix(path, "/api/v1/key-assets") {
		rest := strings.TrimPrefix(path, "/api/v1/key-assets")
		rest = strings.Trim(rest, "/")
		segments := 0
		if rest != "" {
			segments = strings.Count(rest, "/") + 1
		}
		switch {
		case path == "/api/v1/key-assets/exports":
			return "keyassets.export", true
		case method == "GET" && segments > 1:
			return "keyassets.export", true
		case method == "GET":
			return "keyassets.view", true
		default:
			return "keyassets.manage", true
		}
	}

	if strings.HasPrefix(path, "/api/v1/events") || strings.HasPrefix(path, "/api/v1/log-contexts") ||
		strings.HasPrefix(path, "/api/v1/logs") {
		if method == "GET" && strings.Contains(path, "/export") {
			return "events.export", true
		}
		if method == "GET" {
			return "events.view", true
		}
		return "events.manage", true
	}

	if strings.HasPrefix(path, "/api/v1/executions") {
		if method == "GET" {
			return "tasks.view", true
		}
		return "tasks.manage", true
	}

	if strings.HasPrefix(path, "/api/v1/backups/") {
		if strings.Contains(path, "/restore/") {
			return "backups.restore", true
		}
		return "backups.export", true
	}

	if strings.HasPrefix(path, "/api/v1/settings/") {
		switch {
		case strings.HasSuffix(path, "/appearance") || strings.HasPrefix(path, "/api/v1/settings/appearance"):
			if method == "GET" {
				return "settings.view", true
			}
			return "settings.appearance", true
		case strings.HasSuffix(path, "/security") || strings.HasSuffix(path, "/certificates"):
			return "settings.security", true
		case method == "GET":
			return "settings.view", true
		default:
			return "settings.manage", true
		}
	}

	if strings.HasPrefix(path, "/api/v1/debug/") {
		if method == "GET" {
			return "debug.view", true
		}
		return "debug.manage", true
	}

	// Account/role management endpoints (registered in this module).
	if strings.HasPrefix(path, "/api/v1/users") {
		return "access.users.manage", true
	}
	if strings.HasPrefix(path, "/api/v1/roles") {
		return "access.roles.manage", true
	}

	return "", false
}

// HasPermission 报告会话是否持有权限键。
func HasPermission(permissions []string, key string) bool {
	for _, permission := range permissions {
		if permission == key {
			return true
		}
	}
	return false
}

func panelErrCredentialRestricted() error {
	return panelerr.Forbidden("credential_scope_restricted", "Access credential cannot perform this action")
}

// CredentialRequestBlocked 返回访问凭证的硬禁止清单。
func CredentialRequestBlocked(sess Session, method, path string) (string, bool) {
	if sess.AuthKind != "credential" {
		return "", false
	}
	switch {
	case path == "/api/v1/auth/login", path == "/api/v1/auth/account",
		path == "/api/v1/auth/jwt-secret", path == "/api/v1/auth/logout":
		return "credential_scope_restricted", true
	case method == "POST" && path == "/api/v1/access-credentials":
		return "credential_scope_restricted", true
	case !sess.AllowManagement && (strings.HasPrefix(path, "/api/v1/users") || strings.HasPrefix(path, "/api/v1/roles")):
		return "credential_scope_restricted", true
	}
	return "", false
}

// PermissionIsReadOnly 报告权限是否只读。
func PermissionIsReadOnly(key string) bool { return permissioncatalog.IsReadOnly(key) }

// RequirePermission 在已认证会话上追加一项权限校验。
func (s *Service) RequirePermission(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := FromContext(r.Context())
		if !HasPermission(sess.Permissions, key) {
			httpx.Error(w, panelerr.Forbidden("permission_denied", "Permission denied"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
