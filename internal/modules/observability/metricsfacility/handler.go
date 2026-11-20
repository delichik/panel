package metricsfacility

import (
	"context"
	"net/http"

	httpx "panel/internal/platform/http"
)

// service 是 Handler 依赖的指标设施服务面。
type service interface {
	Get(ctx context.Context) (Config, error)
	Save(ctx context.Context, in SaveInput) (Config, error)
}

// Handler 暴露指标设施的 HTTP 入口。
type Handler struct {
	service service
}

// NewHandler 创建指标设施 Handler。
func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

// Config 返回设施配置：GET /api/v1/facility-apps/metrics。
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Get(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// SaveConfig 保存设施配置：PUT /api/v1/facility-apps/metrics。
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	var in SaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.Save(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
