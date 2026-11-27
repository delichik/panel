package auth

import (
	"net/http"
	"strings"

	httpx "panel/internal/platform/http"
)

func (h *Handler) ListAccessCredentials(w http.ResponseWriter, r *http.Request) {
	sess := FromContext(r.Context())
	items, err := h.service.ListAccessCredentials(r.Context(), sess.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreateAccessCredential(w http.ResponseWriter, r *http.Request) {
	sess := FromContext(r.Context())
	if sess.AuthKind != "session" {
		httpx.Error(w, panelErrCredentialRestricted())
		return
	}
	var req AccessCredentialCreateInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	created, err := h.service.CreateAccessCredential(r.Context(), sess.UserID, req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusCreated, created)
}

func (h *Handler) RevokeAccessCredential(w http.ResponseWriter, r *http.Request) {
	sess := FromContext(r.Context())
	canManageOthers := HasPermission(sess.Permissions, "access.credentials.manage")
	if err := h.service.RevokeAccessCredential(r.Context(), sess.UserID, canManageOthers, strings.TrimSpace(r.PathValue("id"))); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}
