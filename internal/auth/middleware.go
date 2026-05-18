package auth

import (
	"context"
	"net/http"

	"panel/internal/httpx"
	"panel/internal/panelerr"
)

type contextKey string

const sessionKey contextKey = "session"

func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			httpx.Error(w, panelerr.Unauthorized("Authentication required"))
			return
		}
		sess, ok := s.Validate(c.Value)
		if !ok {
			httpx.Error(w, panelerr.Unauthorized("Authentication required"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func FromContext(ctx context.Context) Session {
	sess, _ := ctx.Value(sessionKey).(Session)
	return sess
}
