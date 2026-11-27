package applications

import (
	"context"
	"net/http"
	"strings"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type applicationEditSessionFileReader interface {
	GetEditSessionFile(context.Context, string, string, string) (EditSessionFileContent, error)
}

func (h *Handler) GetEditSessionFile(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(applicationEditSessionFileReader)
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "application_edit_sessions_unavailable", "Application edit sessions are not available"))
		return
	}
	result, err := service.GetEditSessionFile(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), editSessionAssetNameFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DownloadEditSessionFile(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(applicationEditSessionFileReader)
	if !ok {
		httpx.Error(w, panelerr.New(http.StatusNotImplemented, "application_edit_sessions_unavailable", "Application edit sessions are not available"))
		return
	}
	result, err := service.GetEditSessionFile(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), editSessionAssetNameFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	name := result.Name
	if result.Kind == ApplicationFileKindArchive && strings.TrimSpace(result.ContentType) != "" {
		name = result.ContentType
	}
	contentType := result.ContentType
	if result.Kind == ApplicationFileKindArchive {
		contentType = inferApplicationFileContentType(name, result.Content, false)
	}
	serveApplicationFileContent(w, r, name, contentType, result.Content)
}
