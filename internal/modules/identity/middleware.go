package auth

import (
	"context"
	"net/http"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

type contextKey string

const sessionKey contextKey = "session"

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return s.requireAuth(next, false)
}

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
		if sess.PasswordChangeRequired && !allowPasswordChange {
			httpx.Error(w, panelerr.Forbidden("password_change_required", "Password change required"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func FromContext(ctx context.Context) Session {
	sess, _ := ctx.Value(sessionKey).(Session)
	return sess
}
