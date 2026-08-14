package facilityapps

import (
	"context"
	"mime"
	"net/http"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type storageShareService interface {
	GetStorageShare(ctx context.Context) (StorageShareConfig, error)
	SaveStorageShare(ctx context.Context, in StorageShareSaveInput) (StorageShareConfig, error)
	ReconcileStorageShareNow(ctx context.Context) (StorageShareConfig, error)
	DeleteStorageShare(ctx context.Context) error
	DownloadStoragePartition(ctx context.Context, partitionID string) (StoragePartitionDownload, error)
	DeleteStoragePartition(ctx context.Context, partitionID string) error
}

func (h *Handler) storageService() (storageShareService, bool) {
	service, ok := h.service.(storageShareService)
	return service, ok
}

func (h *Handler) StorageShare(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	result, err := service.GetStorageShare(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) SaveStorageShare(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	var in StorageShareSaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := service.SaveStorageShare(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ReconcileStorageShare(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	result, err := service.ReconcileStorageShareNow(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteStorageShare(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	if err := service.DeleteStorageShare(r.Context()); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DownloadStoragePartition(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	result, err := service.DownloadStoragePartition(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	filename := result.Filename
	if filename == "" {
		filename = "partition-storage.tgz"
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	_, _ = w.Write(result.Content)
}

func (h *Handler) DeleteStoragePartition(w http.ResponseWriter, r *http.Request) {
	service, ok := h.storageService()
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "storage_share_unavailable", "Storage share facility is unavailable"))
		return
	}
	if err := service.DeleteStoragePartition(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}