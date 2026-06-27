package backups

import (
	"net/http"
	"os"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) StartExport(w http.ResponseWriter, r *http.Request) {
	var req ExportRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	resp, err := h.service.StartExport(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, resp)
}

func (h *Handler) PreflightRestore(w http.ResponseWriter, r *http.Request) {
	filePath, cleanup, password, ok := receiveArchiveUpload(w, r)
	if !ok {
		return
	}
	defer cleanup()
	resp, err := h.service.PreflightRestore(filePath, password)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) ConfirmRestore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request"))
		return
	}
	if r.FormValue("confirmOverwrite") != "true" {
		httpx.Error(w, panelerr.BadRequest("restore_confirmation_required", "Restore overwrite confirmation is required"))
		return
	}
	filePath, cleanup, password, ok := saveMultipartFile(w, r)
	if !ok {
		return
	}
	defer cleanup()
	resp, err := h.service.SavePendingRestore(filePath, password)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func receiveArchiveUpload(w http.ResponseWriter, r *http.Request) (string, func(), string, bool) {
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request"))
		return "", func() {}, "", false
	}
	return saveMultipartFile(w, r)
}

func saveMultipartFile(w http.ResponseWriter, r *http.Request) (string, func(), string, bool) {
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("restore_archive_required", "Backup archive is required"))
		return "", func() {}, "", false
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "panel-restore-upload-*.panel-backup")
	if err != nil {
		httpx.Error(w, err)
		return "", func() {}, "", false
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := tmp.ReadFrom(file); err != nil {
		_ = tmp.Close()
		cleanup()
		httpx.Error(w, err)
		return "", func() {}, "", false
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		httpx.Error(w, err)
		return "", func() {}, "", false
	}
	return path, cleanup, r.FormValue("password"), true
}
