package metrics

import (
	"net/http"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

// bucketQuerySeconds 是 bucket 查询参数的合法取值范围（秒）。
const (
	minBucketSeconds = 60
	maxBucketSeconds = 86400
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// Query 返回服务器指标序列：GET /api/v1/servers/{id}/metrics?range=<rng>。
// 可选 bucket 查询参数（60..86400 秒）请求服务端按时间桶聚合；未传、空值或
// 非正数保持既有的原始点查询。
func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	serverID := serverIDFromRequest(r)
	rng := r.URL.Query().Get("range")
	bucketSeconds, bucketed, err := bucketSecondsFromQuery(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var series Series
	if bucketed {
		series, err = h.service.QueryBucketed(r.Context(), serverID, rng, bucketSeconds)
	} else {
		series, err = h.service.Query(r.Context(), serverID, rng)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, series)
}

// bucketSecondsFromQuery 解析可选 bucket 参数。返回 bucketed=false 表示走原始
// 查询：缺省、空值与非正数都属于“未请求降采样”。非整数或超出
// [60, 86400] 返回稳定校验错误 bucket_invalid。
func bucketSecondsFromQuery(r *http.Request) (int, bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("bucket"))
	if raw == "" {
		return 0, false, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, panelerr.Validation("bucket_invalid", "bucket must be an integer number of seconds")
	}
	if seconds <= 0 {
		return 0, false, nil
	}
	if seconds < minBucketSeconds || seconds > maxBucketSeconds {
		return 0, false, panelerr.Validation("bucket_invalid", "bucket must be between 60 and 86400 seconds")
	}
	return seconds, true, nil
}

func serverIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
