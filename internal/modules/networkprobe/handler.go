package networkprobe

import (
	"context"
	"net/http"
	"strings"

	httpx "panel/internal/platform/http"
)

// service 是 Handler 依赖的网络探测服务面。
type service interface {
	Get(ctx context.Context) (Config, error)
	Save(ctx context.Context, in SaveInput) (Config, error)
	Matrix(ctx context.Context, filterServerIDs []string) (Matrix, error)
	ServerResults(ctx context.Context, serverID string) (ServerResults, error)
}

// Handler 暴露网络探测设施的 HTTP 入口。
type Handler struct {
	service service
}

// NewHandler 创建网络探测设施 Handler。
func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

// Config 返回设施配置：GET /api/v1/facility-apps/network-probe。
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Get(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// SaveConfig 保存设施配置：PUT /api/v1/facility-apps/network-probe。
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

// Results 返回探测矩阵：GET /api/v1/facility-apps/network-probe/results。
// 可选 serverIds 查询参数（可重复或逗号分隔）用于只看部分探测节点。
func (h *Handler) Results(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Matrix(r.Context(), serverIDsFromQuery(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// ServerResults 返回单台服务器的探测数据：
// GET /api/v1/servers/{id}/network-probes。
func (h *Handler) ServerResults(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ServerResults(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func serverIDsFromQuery(r *http.Request) []string {
	out := []string{}
	for _, raw := range r.URL.Query()["serverIds"] {
		for _, part := range strings.Split(raw, ",") {
			if value := strings.TrimSpace(part); value != "" {
				out = append(out, value)
			}
		}
	}
	return out
}
