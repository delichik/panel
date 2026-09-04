package settings

import (
	"net/http"

	"panel/internal/platform/http"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Runtime())
}

func (h *Handler) PublicBranding(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Runtime().Branding)
}

func (h *Handler) UpdateRuntime(w http.ResponseWriter, r *http.Request) {
	var input RuntimeUpdate
	if !httpx.Decode(w, r, &input) {
		return
	}
	settings, err := h.service.Update(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (h *Handler) ServerVariableDefinitions(w http.ResponseWriter, r *http.Request) {
	defs, err := h.service.ServerVariableDefinitions(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, defs)
}

func (h *Handler) UpdateServerVariableDefinitions(w http.ResponseWriter, r *http.Request) {
	var input ServerVariableDefinitionsUpdate
	if !httpx.Decode(w, r, &input) {
		return
	}
	defs, err := h.service.UpdateServerVariableDefinitions(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, defs)
}
