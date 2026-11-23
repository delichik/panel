package auth

import (
	"net/http"
	"strings"

	httpx "panel/internal/platform/http"
)

func (h *Handler) PermissionCatalog(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.PermissionCatalog())
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q", "status")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.ListUsers(r.Context(), page, pageSize, r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req UserCreateInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	view, err := h.service.CreateUser(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, view)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.GetUser(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req UserUpdateInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	sess := FromContext(r.Context())
	view, err := h.service.UpdateUser(r.Context(), sess.UserID, strings.TrimSpace(r.PathValue("id")), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	sess := FromContext(r.Context())
	if err := h.service.DeleteUser(r.Context(), sess.UserID, strings.TrimSpace(r.PathValue("id"))); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ResetUserPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NewPassword string `json:"newPassword"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.service.ResetUserPassword(r.Context(), strings.TrimSpace(r.PathValue("id")), req.NewPassword); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (h *Handler) ForceUserLogout(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.PathValue("id"))
	if err := h.service.BumpUserGeneration(r.Context(), userID); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"signedOut": true})
}

func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.service.ListRoles(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": roles})
}

func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	var req RoleSaveInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	view, err := h.service.CreateRole(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, view)
}

func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.GetRole(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	var req RoleSaveInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	view, err := h.service.UpdateRole(r.Context(), strings.TrimSpace(r.PathValue("id")), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, view)
}

func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRole(r.Context(), strings.TrimSpace(r.PathValue("id"))); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}
