package containerization

import (
	"net/http"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Containers(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Containers(r.Context(), serverIDFromRequest(r))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID := resourceFromRequest(r)
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	result, err := h.service.ContainerLogs(r.Context(), serverID, resourceID, tail)
	write(w, http.StatusOK, result, err)
}

func (h *Handler) ContainerAction(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID, action := actionFromRequest(r)
	if action != "start" && action != "stop" && action != "restart" {
		httpx.Error(w, panelerr.NotFound("route"))
		return
	}
	result, err := h.service.ContainerAction(r.Context(), serverID, resourceID, action)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteContainer(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID := resourceFromRequest(r)
	result, err := h.service.DeleteContainer(r.Context(), serverID, resourceID)
	writeOperation(w, result, err)
}

func (h *Handler) Images(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Images(r.Context(), serverIDFromRequest(r))
	write(w, http.StatusOK, out, err)
}

func (h *Handler) PullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference string `json:"reference"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	result, err := h.service.PullImage(r.Context(), serverIDFromRequest(r), req.Reference)
	writeOperation(w, result, err)
}

func (h *Handler) RefreshImages(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.RefreshImages(r.Context(), serverIDFromRequest(r), "user", "")
	writeTask(w, task.ID, err)
}

func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID := resourceFromRequest(r)
	result, err := h.service.DeleteImage(r.Context(), serverID, resourceID)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteUnusedImages(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DeleteUnusedImages(r.Context(), serverIDFromRequest(r))
	writeOperation(w, result, err)
}

func (h *Handler) UpgradeSelected(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ApplicationIDs []string `json:"applicationIds"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.service.UpgradeApplications(r.Context(), req.ApplicationIDs, false)
	writeTask(w, task.ID, err)
}

func (h *Handler) UpgradeAll(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.UpgradeApplications(r.Context(), nil, true)
	writeTask(w, task.ID, err)
}

func (h *Handler) Networks(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Networks(r.Context(), serverIDFromRequest(r))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) Volumes(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Volumes(r.Context(), serverIDFromRequest(r))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	serverID, name := resourceFromRequest(r)
	result, err := h.service.DeleteVolume(r.Context(), serverID, name)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteUnusedVolumes(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DeleteUnusedVolumes(r.Context(), serverIDFromRequest(r))
	writeOperation(w, result, err)
}

func writeOperation(w http.ResponseWriter, result OperationResult, err error) {
	write(w, http.StatusOK, result, err)
}

func writeTask(w http.ResponseWriter, taskID string, err error) {
	write(w, http.StatusAccepted, map[string]string{"taskId": taskID}, err)
}

func write(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, status, value)
}

func serverIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("serverId"))
}

func resourceFromRequest(r *http.Request) (string, string) {
	return strings.TrimSpace(r.PathValue("serverId")), strings.TrimSpace(r.PathValue("resourceId"))
}

func actionFromRequest(r *http.Request) (string, string, string) {
	return strings.TrimSpace(r.PathValue("serverId")), strings.TrimSpace(r.PathValue("resourceId")), strings.TrimSpace(r.PathValue("action"))
}
