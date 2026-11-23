package auth

import (
	"context"
	"net/http"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type contextKey string

const sessionKey contextKey = "session"

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return s.requireAuth(next, false)
}

// RequireAuthAllowPasswordChange keeps the account/logout/session surface
// reachable while the user must change their first-login password.
func (s *Service) RequireAuthAllowPasswordChange(next http.Handler) http.Handler {
	return s.requireAuth(next, true)
}

func (s *Service) requireAuth(next http.Handler, allowPasswordChange bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			httpx.Error(w, panelerr.Unauthorized("Authentication required"))
			return
		}
		sess, ok := s.Validate(r.Context(), token)
		if !ok {
			httpx.Error(w, panelerr.Unauthorized("Authentication required"))
			return
		}
		if sess.MustChangePassword && !allowPasswordChange {
			httpx.Error(w, panelerr.Forbidden("password_change_required", "Password change required"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

// FromContext returns the authenticated session without exposing the token to
// consumers.
func FromContext(ctx context.Context) Session {
	sess, _ := ctx.Value(sessionKey).(Session)
	sess.Token = ""
	return sess
}
