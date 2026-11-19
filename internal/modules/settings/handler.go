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

func (h *Handler) UpdateImageRegistry(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateImageRegistry)
}

func (h *Handler) UpdateBranding(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateBranding)
}

func (h *Handler) UpdateAutoUpdate(w http.ResponseWriter, r *http.Request) {
	updatePartition(h, w, r, h.service.UpdateAutoUpdate)
}

// RuntimeAutoUpdateRunResult 是手动「立即运行一次」端点的响应（202 Accepted）。
// 已有活跃 auto_update_run 时返回其 taskId（幂等，不重复派发）。
type RuntimeAutoUpdateRunResult struct {
	TaskID string `json:"taskId"`
}

// RunAutoUpdateNow 手动立即运行一次自动更新（SET-RUN-023）。payload 可选：
// 省略时使用当前设置；只接受 auto-update 分区三个可编辑字段的临时覆盖。
// 两开关（含覆盖后）都关时返回 422 auto_update_disabled。
func (h *Handler) RunAutoUpdateNow(w http.ResponseWriter, r *http.Request) {
	var input RuntimeAutoUpdateRunRequest
	// payload 可选：无请求体（Content-Length 0）按「全部沿用当前设置」处理。
	if r.Body != nil && r.ContentLength != 0 {
		if !httpx.Decode(w, r, &input) {
			return
		}
	}
	taskID, err := h.service.RunAutoUpdateNow(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, RuntimeAutoUpdateRunResult{TaskID: taskID})
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
