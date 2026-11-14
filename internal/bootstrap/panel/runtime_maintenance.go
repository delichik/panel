package panel

import (
	"context"
	"net/http"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
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
			if !admitted {
				r = r.WithContext(context.WithValue(r.Context(), runtimeClearAuditContextKey{}, true))
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
			// Reject new writers before releasing this request's Enter token so
			// there is no window between 202 and clearRuntimeData's PauseContext
			// where ordinary mutations could still write (DIAG-CLR-006).
			a.runtimeWriters.BeginPause()
			done()
			drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := a.runtimeWriters.WaitDrained(drainCtx); err != nil {
				a.runtimeWriters.Resume()
				logging.From(logging.Background("panel")).Warn("clear-runtime-data writer drain timed out after accept", zap.Error(err))
			}
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		projectionRead := strings.HasPrefix(r.URL.Path, "/api/v1/events") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/logs") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/log-contexts")
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
