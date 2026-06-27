package facilityapps

import (
	"context"
	"io"
	"net/http"
	"strings"

	httpx "panel/internal/platform/http"
)

type service interface {
	GetReverseProxy(ctx context.Context) (ReverseProxyConfig, error)
	SaveReverseProxy(ctx context.Context, in ReverseProxySaveInput) (ReverseProxyConfig, error)
	ReconcileReverseProxyNow(ctx context.Context) (ReconcileResult, error)
	ListStaticAssets(ctx context.Context) ([]StaticAsset, error)
	UploadStaticAsset(ctx context.Context, in StaticAssetUploadInput) (StaticAsset, error)
	DeleteStaticAsset(ctx context.Context, assetID string) error
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ReverseProxy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetReverseProxy(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) SaveReverseProxy(w http.ResponseWriter, r *http.Request) {
	var in ReverseProxySaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.SaveReverseProxy(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ReconcileReverseProxy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ReconcileReverseProxyNow(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) StaticAssets(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListStaticAssets(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) UploadStaticAsset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		httpx.Error(w, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.UploadStaticAsset(r.Context(), StaticAssetUploadInput{
		Name:     r.FormValue("name"),
		Kind:     r.FormValue("kind"),
		FileName: header.Filename,
		Size:     header.Size,
		Content:  content,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) DeleteStaticAsset(w http.ResponseWriter, r *http.Request) {
	assetID := strings.TrimSpace(r.PathValue("assetId"))
	if err := h.service.DeleteStaticAsset(r.Context(), assetID); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
