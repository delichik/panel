package auth

import (
	"net/http"

	"panel/internal/httpx"
)

const CookieName = "panel_session"

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
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: h.service.CookieValue(sess.ID), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: sess.ExpiresAt})
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		h.service.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	sess, ok := h.service.Validate(c.Value)
	if !ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": sess.Username})
}
