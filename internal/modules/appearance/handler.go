package appearance

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

const (
	// maxBackgroundUploadBodyBytes 是上传请求体的显式上限：8 MiB 文件 + 1 MiB
	// multipart 边界/表单开销，避免依赖全局 10 MiB JSON 上限。
	maxBackgroundUploadBodyBytes = MaxBackgroundUploadBytes + (1 << 20)
	// maxBackgroundUploadFormMemory 是 ParseMultipartForm 允许驻留内存的上限，
	// 超过部分落临时文件并由 RemoveAll 清理。
	maxBackgroundUploadFormMemory = 1 << 20
)

// service 是 Handler 依赖的外观服务面。
type service interface {
	Get(ctx context.Context) (Appearance, error)
	Update(ctx context.Context, in AppearanceUpdate) (Appearance, error)
	UploadBackground(ctx context.Context, content []byte) (Appearance, error)
	RemoveBackground(ctx context.Context) (Appearance, error)
	BackgroundContent(ctx context.Context, variant string) (BackgroundContent, error)
	PublicAppearance(ctx context.Context) (PublicAppearance, error)
}

// Handler 暴露外观设置的 HTTP 入口。
type Handler struct {
	service service
}

// NewHandler 创建外观设置 Handler。
func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

// GetAppearance 返回完整外观：GET /api/v1/settings/appearance。
func (h *Handler) GetAppearance(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Get(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// UpdateAppearance 整体替换主题/Mica/强调色与可选背景：
// PUT /api/v1/settings/appearance。校验失败零写入。
func (h *Handler) UpdateAppearance(w http.ResponseWriter, r *http.Request) {
	var input AppearanceUpdate
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := h.service.Update(r.Context(), input)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// UploadBackground 上传背景图片：POST /api/v1/settings/appearance/background
// （multipart 字段全名固定为 file；不使用用户文件名）。
func (h *Handler) UploadBackground(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackgroundUploadBodyBytes)
	if err := r.ParseMultipartForm(maxBackgroundUploadFormMemory); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			httpx.Error(w, backgroundTooLargeError())
			return
		}
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request body"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, backgroundRequiredError())
		return
	}
	defer file.Close()
	// 读取上限 +1 字节以区分“恰好 8 MiB”与“超限”，避免超大文件整体进内存。
	content, err := io.ReadAll(io.LimitReader(file, MaxBackgroundUploadBytes+1))
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Failed to read background upload"))
		return
	}
	if len(content) == 0 {
		httpx.Error(w, backgroundRequiredError())
		return
	}
	if len(content) > MaxBackgroundUploadBytes {
		httpx.Error(w, backgroundTooLargeError())
		return
	}
	result, err := h.service.UploadBackground(r.Context(), content)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// DeleteBackground 删除背景资产：DELETE /api/v1/settings/appearance/background。
// 重复删除幂等返回当前外观。
func (h *Handler) DeleteBackground(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RemoveBackground(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// BackgroundContent 返回背景原图/缩略图字节：
// GET /api/v1/settings/appearance/background/content?variant=full|thumb。
// 内容按资产 id 与 variant 生成 ETag 并长期 immutable 缓存（必须携带认证）。
func (h *Handler) BackgroundContent(w http.ResponseWriter, r *http.Request) {
	variant := strings.TrimSpace(r.URL.Query().Get("variant"))
	content, err := h.service.BackgroundContent(r.Context(), variant)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", content.ContentType)
	w.Header().Set("ETag", content.ETag)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, "", content.ModTime, bytes.NewReader(content.Content))
}

// PublicAppearance 返回登录页可用的公开外观（skipAuth）：
// GET /api/v1/settings/public-appearance，只含模糊缩略图，不含原图。
func (h *Handler) PublicAppearance(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.PublicAppearance(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
