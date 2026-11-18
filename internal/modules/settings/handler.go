package settings

import (
	"context"
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

// updatePartition 解码分区写入负载，委托对应的 Service 方法，并统一返回完整
// RuntimeSettings。分区端点只描述本分区字段，校验、原子持久化与热更新由
// Service 在完整候选快照上完成（SET-RUN-005 / SET-RUN-022）。
func updatePartition[T any](h *Handler, w http.ResponseWriter, r *http.Request, update func(context.Context, T) (RuntimeSettings, error)) {
	var input T
	if !httpx.Decode(w, r, &input) {
		return
	}
	settings, err := update(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (h *Handler) UpdateGeneral(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateGeneral)
}

func (h *Handler) UpdateSecurity(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateSecurity)
}

func (h *Handler) UpdateCertificates(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateCertificates)
}

func (h *Handler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateAgent)
}

func (h *Handler) UpdateBranding(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateBranding)
}

func (h *Handler) UpdateTailscale(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateTailscale)
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
