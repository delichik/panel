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

// ApplyTailscale 重新下发容器内 tailscale 期望态并返回最新实际态。2xx 只代表
// panel-init 已接受收敛请求，真实结果以返回的状态对象为准。
func (h *Handler) ApplyTailscale(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.ApplyTailscaleContainer(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, state)
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
