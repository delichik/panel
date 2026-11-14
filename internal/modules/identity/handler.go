package auth

import (
	"net"
	"net/http"
	"strings"

	"panel/internal/platform/http"
)

const bearerPrefix = "Bearer "

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	sess, err := h.service.LoginFrom(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessionPayload(sess, true))
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Logout(r.Context()); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	sess, ok := h.service.Validate(r.Context(), token)
	if !ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	httpx.JSON(w, http.StatusOK, sessionPayload(sess, true))
}

func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req AccountUpdate
	if !httpx.Decode(w, r, &req) {
		return
	}
	sess, err := h.service.UpdateAccount(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessionPayload(sess, true))
}

func (h *Handler) UpdateJWTSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JWTSecret string `json:"jwtSecret"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	sess, err := h.service.UpdateJWTSecret(r.Context(), req.JWTSecret)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessionPayload(sess, true))
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil || host == "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) || header[:len(bearerPrefix)] != bearerPrefix {
		return ""
	}
	return header[len(bearerPrefix):]
}

func sessionPayload(sess Session, authenticated bool) map[string]any {
	return map[string]any{
		"authenticated":          authenticated,
		"token":                  sess.Token,
		"username":               sess.Username,
		"passwordChangeRequired": sess.PasswordChangeRequired,
	}
}
