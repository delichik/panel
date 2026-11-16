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
	ServerResultsHistory(ctx context.Context, serverID string) (ServerResults, error)
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
// 可选 history 查询参数（1 或 true）表示额外返回每个目标的最近 24h 历史曲线；
// 缺省/其它值保持只返回最新一次结果的既有响应。
func (h *Handler) ServerResults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var (
		result ServerResults
		err    error
	)
	if historyEnabled(r) {
		result, err = h.service.ServerResultsHistory(ctx, id)
	} else {
		result, err = h.service.ServerResults(ctx, id)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// historyEnabled 解析 history 查询参数：只接受 "1" 与 "true"（忽略大小写）。
func historyEnabled(r *http.Request) bool {
	raw := strings.TrimSpace(r.URL.Query().Get("history"))
	return raw == "1" || strings.EqualFold(raw, "true")
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
