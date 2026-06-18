package server

import (
	"net/http"
	"strconv"
	"strings"

	"panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	servers, err := h.service.List(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, servers)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	srv, err := h.service.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, srv)
}

func (h *Handler) Probe(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	result, err := h.service.ProbeConnectivity(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req SaveRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	srv, err := h.service.Update(r.Context(), serverIDFromRequest(r), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, srv)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), serverIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.TestConnectivity(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) InstallUFW(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.InstallUFW(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Restart(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) IssueAgentCertificate(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.service.IssueAgentCertificate(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, bundle)
}

func (h *Handler) DeployAgent(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.DeployAgent(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) SystemCertificates(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.SystemCertificates(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) ResetSystemCertificate(w http.ResponseWriter, r *http.Request) {
	certificateID := r.PathValue("id")
	if certificateID == "" {
		certificateID = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/key-assets/system/"), "/reset")
	}
	task, err := h.service.ResetSystemCertificate(r.Context(), certificateID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UFWState(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.UFWState(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

func (h *Handler) AllowUFW(w http.ResponseWriter, r *http.Request) {
	var req UFWAllowRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	state, err := h.service.AllowUFW(r.Context(), serverIDFromRequest(r), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

func (h *Handler) EnableUFW(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.EnableUFW(r.Context(), serverIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) DeleteUFWRule(w http.ResponseWriter, r *http.Request) {
	serverID, number := ufwRuleTargetFromRequest(r)
	state, err := h.service.DeleteUFWRule(r.Context(), serverID, number)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

func serverIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func ufwRuleTargetFromRequest(r *http.Request) (string, int) {
	id := strings.TrimSpace(r.PathValue("id"))
	numberRaw := strings.TrimSpace(r.PathValue("number"))
	number, _ := strconv.Atoi(numberRaw)
	return id, number
}
