package diagnostics

import (
	"encoding/json"
	"net/http"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

const clearRuntimeDataConfirmation = "CLEAR RUNTIME DATA"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Runtime(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Runtime())
}

func (h *Handler) Tasks(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Tasks())
}

func (h *Handler) Databases(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.service.Databases(r.Context()))
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

func (h *Handler) ClearRuntimeData(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Confirmation != clearRuntimeDataConfirmation {
		httpx.Error(w, panelerr.Validation("clear_runtime_data_confirmation_required", "Type the required confirmation to clear runtime data"))
		return
	}
	result, err := h.service.ClearRuntimeData(r.Context())
	if err != nil {
		httpx.Error(w, panelerr.New(http.StatusInternalServerError, "clear_runtime_data_failed", "Unable to clear runtime data"))
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
