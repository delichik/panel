package server

import (
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
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
	srv, err := h.service.Update(r.Context(), serverID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, srv)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), serverID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.TestConnectivity(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/test")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) InstallUFW(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.InstallUFW(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/ufw/install")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Restart(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/restart")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UFWState(w http.ResponseWriter, r *http.Request) {
	state, err := h.service.UFWState(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/ufw")))
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
	state, err := h.service.AllowUFW(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/ufw/rules")), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

func (h *Handler) EnableUFW(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.EnableUFW(r.Context(), serverID(strings.TrimSuffix(r.URL.Path, "/ufw/enable")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) DeleteUFWRule(w http.ResponseWriter, r *http.Request) {
	serverID, number := ufwRuleTarget(r.URL.Path)
	state, err := h.service.DeleteUFWRule(r.Context(), serverID, number)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, state)
}

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func ufwRuleTarget(path string) (string, int) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 7 {
		return "", 0
	}
	number, _ := strconv.Atoi(parts[6])
	return parts[3], number
}
