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
	result, err := service.GetEditSessionFile(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), strings.TrimSpace(r.PathValue("fileKey")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
