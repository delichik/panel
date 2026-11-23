package auth

import (
	"net"
	"net/http"
	"strings"
	"time"

	httpx "panel/internal/platform/http"
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
	sess := FromContext(r.Context())
	if err := h.service.Logout(r.Context(), sess.UserID); err != nil {
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
	sess := FromContext(r.Context())
	result, err := h.service.UpdateAccount(r.Context(), sess.UserID, req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
		"mustRelogin":   result.MustRelogin,
	})
}

func (h *Handler) UpdateJWTSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JWTSecret string `json:"jwtSecret"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.service.UpdateJWTSecret(r.Context(), req.JWTSecret); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
		"mustRelogin":   true,
	})
}

// SetupStatus reports whether the first-run initialization UI is required.
func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	required, err := h.service.SetupRequired(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"required": required})
}

// Setup creates the single built-in administrator on a fresh installation.
func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if _, err := h.service.CreateInitialAdmin(r.Context(), req.Username, req.Password); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"created": true})
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
	roles := make([]RoleRef, 0, len(sess.Roles))
	roles = append(roles, sess.Roles...)
	permissions := make([]string, 0, len(sess.Permissions))
	permissions = append(permissions, sess.Permissions...)
	payload := map[string]any{
		"authenticated":      authenticated,
		"token":              sess.Token,
		"username":           sess.Username,
		"mustChangePassword": sess.MustChangePassword,
		"authKind":           sess.AuthKind,
		"user": map[string]any{
			"id":             sess.UserID,
			"username":       sess.Username,
			"displayName":    sess.DisplayName,
			"isBuiltinAdmin": sess.IsBuiltinAdmin,
		},
		"roles":       roles,
		"permissions": permissions,
	}
	if !sess.ExpiresAt.IsZero() {
		payload["expiresAt"] = sess.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if sess.CredentialID != "" {
		payload["credentialId"] = sess.CredentialID
		payload["credentialScope"] = sess.CredentialScope
		payload["allowManagement"] = sess.AllowManagement
	}
	return payload
}
