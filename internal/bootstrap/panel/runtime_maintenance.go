package panel

import (
	"context"
	"net/http"
	"strings"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type runtimeClearAuditContextKey struct{}

// Cleanup status and duplicate receipts remain available during maintenance.
// The first request's audit is drained before deletion, like other writers.
func (a *App) runtimeMaintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/debug/clear-runtime-data" {
			// The first request's audit tail must finish before deletion starts.
			// Repeated requests during cleanup only read the same run receipt;
			// they must remain available without adding audit rows mid-delete.
			done, admitted := a.runtimeWriters.Enter()
			if admitted {
				defer done()
			} else {
				r = r.WithContext(context.WithValue(r.Context(), runtimeClearAuditContextKey{}, true))
			}
			next.ServeHTTP(w, r)
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		projectionRead := strings.HasPrefix(r.URL.Path, "/api/v1/activity")
		authSession := r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/logout"
		if authSession || r.URL.Path == "/api/v1/debug/clear-runtime-data" || (!mutation && !projectionRead) {
			next.ServeHTTP(w, r)
			return
		}
		done, ok := a.runtimeWriters.Enter()
		if !ok {
			w.Header().Set("Retry-After", "5")
			httpx.Error(w, panelerr.New(http.StatusServiceUnavailable, "runtime_data_maintenance", "Runtime data is being cleared; retry after it finishes"))
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}
