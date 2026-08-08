package diagnostics

import (
	"net/http"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Snapshot(r.Context()))
}

func (h *Handler) PprofStatus(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.PprofStatus())
}

func (h *Handler) UpdatePprof(w http.ResponseWriter, r *http.Request) {
	var input PprofUpdate
	if !httpx.Decode(w, r, &input) {
		return
	}
	var err error
	if input.Enabled {
		err = h.service.EnablePprof()
	} else {
		err = h.service.DisablePprof()
	}
	if err != nil {
		if input.Enabled {
			httpx.Error(w, panelerr.Conflict("pprof_start_failed", "Unable to start the pprof server"))
		} else {
			httpx.Error(w, panelerr.New(http.StatusInternalServerError, "pprof_stop_failed", "Unable to stop the pprof server"))
		}
		return
	}
	httpx.JSON(w, http.StatusOK, h.service.PprofStatus())
}
