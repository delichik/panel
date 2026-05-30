package auth

import (
	"net/http"

	"panel/internal/httpx"
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
	sess, err := h.service.Login(req.Username, req.Password)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true, "token": sess.Token, "username": sess.Username})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	sess, ok := h.service.Validate(token)
	if !ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": sess.Username})
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) || header[:len(bearerPrefix)] != bearerPrefix {
		return ""
	}
	return header[len(bearerPrefix):]
}
