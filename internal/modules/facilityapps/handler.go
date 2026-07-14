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
	BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error)
	UploadSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetUploadInput) (StaticAsset, error)
	DeleteSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetDeleteInput) error
	CommitSaveSession(ctx context.Context, sessionID string, in CommitSaveSessionInput) (SaveSessionCommitResult, error)
	DiscardSaveSession(sessionID string)
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

func (h *Handler) BeginSaveSession(w http.ResponseWriter, r *http.Request) {
	var in BeginSaveSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.BeginSaveSession(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) UploadSaveSessionAsset(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.service.UploadSaveSessionAsset(r.Context(), r.PathValue("id"), StaticAssetUploadInput{
		AssetID:  r.FormValue("assetId"),
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

func (h *Handler) DeleteSaveSessionAsset(w http.ResponseWriter, r *http.Request) {
	var in StaticAssetDeleteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := h.service.DeleteSaveSessionAsset(r.Context(), r.PathValue("id"), in); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CommitSaveSession(w http.ResponseWriter, r *http.Request) {
	var in CommitSaveSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CommitSaveSession(r.Context(), r.PathValue("id"), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DiscardSaveSession(w http.ResponseWriter, r *http.Request) {
	h.service.DiscardSaveSession(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
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
