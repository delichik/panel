package containerization

import (
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Containers(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Containers(r.Context(), serverID(r.URL.Path))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) ContainerAction(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID, action := resourceAction(r.URL.Path, "containers")
	result, err := h.service.ContainerAction(r.Context(), serverID, resourceID, action)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteContainer(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID := resourcePath(r.URL.Path, "containers")
	result, err := h.service.DeleteContainer(r.Context(), serverID, resourceID)
	writeOperation(w, result, err)
}

func (h *Handler) Images(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Images(r.Context(), serverID(r.URL.Path))
	write(w, http.StatusOK, out, err)
}

func (h *Handler) PullImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference string `json:"reference"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	result, err := h.service.PullImage(r.Context(), serverID(r.URL.Path), req.Reference)
	writeOperation(w, result, err)
}

func (h *Handler) RefreshImages(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.RefreshImages(r.Context(), serverID(r.URL.Path), "user", "")
	writeTask(w, task.ID, err)
}

func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	serverID, resourceID := resourcePath(r.URL.Path, "images")
	result, err := h.service.DeleteImage(r.Context(), serverID, resourceID)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteUnusedImages(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DeleteUnusedImages(r.Context(), serverID(r.URL.Path))
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
	items, err := h.service.Networks(r.Context(), serverID(r.URL.Path))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) Volumes(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Volumes(r.Context(), serverID(r.URL.Path))
	write(w, http.StatusOK, items, err)
}

func (h *Handler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	serverID, name := resourcePath(r.URL.Path, "volumes")
	result, err := h.service.DeleteVolume(r.Context(), serverID, name)
	writeOperation(w, result, err)
}

func (h *Handler) DeleteUnusedVolumes(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.DeleteUnusedVolumes(r.Context(), serverID(r.URL.Path))
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

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 3 {
		return parts[3]
	}
	return ""
}

func resourcePath(path, kind string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 6 && parts[4] == kind {
		return parts[3], parts[5]
	}
	return "", ""
}

func resourceAction(path, kind string) (string, string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 7 && parts[4] == kind {
		return parts[3], parts[5], parts[6]
	}
	return "", "", ""
}
