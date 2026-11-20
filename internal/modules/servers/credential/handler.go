package credential

import (
	"net/http"
	"strings"

	"panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	creds, err := h.service.ListPage(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, creds)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	cred, err := h.service.GetWithSummary(r.Context(), credentialIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cred)
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
	cred, err := h.service.Update(r.Context(), credentialIDFromRequest(r), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cred)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), credentialIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func credentialIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
