package credential

import (
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	creds, err := h.service.List(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, creds)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	cred, err := h.service.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, cred)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	cred, err := h.service.Update(r.Context(), credentialID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cred)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), credentialID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func credentialID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
