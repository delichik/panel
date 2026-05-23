package docker

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"panel/internal/httpx"
	"panel/internal/panelerr"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Capability(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Capability(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	task, err := h.service.Refresh(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) Projects(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListProjects(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) ProjectStatus(w http.ResponseWriter, r *http.Request) {
	serverID, project := serverAndProject(r.URL.Path)
	out, err := h.service.ComposeStatus(r.Context(), serverID, project)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Services(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListServices(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Networks(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListNetworks(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Volumes(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListVolumes(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Images(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListImages(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) RuntimeExplorer(w http.ResponseWriter, r *http.Request) {
	nodeID := runtimeExplorerNodeID(r.URL.Path)
	if r.Method == http.MethodGet {
		capability, _ := h.service.Capability(r.Context(), nodeID)
		containers, _ := h.service.ListServices(r.Context(), nodeID)
		networks, _ := h.service.ListNetworks(r.Context(), nodeID)
		volumes, _ := h.service.ListVolumes(r.Context(), nodeID)
		images, _ := h.service.ListImages(r.Context(), nodeID)
		httpx.JSON(w, http.StatusOK, map[string]any{
			"nodeId":     nodeID,
			"capability": capability,
			"containers": containers.Items,
			"networks":   networks.Items,
			"volumes":    volumes.Items,
			"images":     images.Items,
		})
		return
	}
	if op, ok := runtimeExplorerOperation(r); ok {
		task, err := h.service.RuntimeExplorerResourceTask(r.Context(), nodeID, op)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID, "operationId": task.OperationID})
		return
	}
	httpx.Error(w, panelerr.NotFound("route"))
}

func (h *Handler) NotImplemented(w http.ResponseWriter, r *http.Request) {
	if op, ok, err := imageUpdateOperation(r); err != nil {
		httpx.Error(w, err)
		return
	} else if ok {
		task, err := h.service.ImageUpdateTask(r.Context(), serverID(r.URL.Path), op)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
		return
	}
	if op, ok := resourceOperation(r); ok {
		task, err := h.service.ResourceTask(r.Context(), serverID(r.URL.Path), op)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
		return
	}
	taskType, summary, err := notImplementedOperation(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	task, err := h.service.NotImplementedTask(r.Context(), serverID(r.URL.Path), taskType, summary)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func imageUpdateOperation(r *http.Request) (ImageUpdateOperation, bool, error) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/images/check-updates"):
		return ImageUpdateOperation{Action: "check_updates", Summary: "Checking Docker image updates"}, true, nil
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/images/update-all"):
		return ImageUpdateOperation{Action: "update_all", Summary: "Updating all Docker images"}, true, nil
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/docker/images/update-selected"):
		defer r.Body.Close()
		var req struct {
			ImageIDs []string `json:"imageIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return ImageUpdateOperation{}, true, panelerr.BadRequest("bad_request", "Invalid JSON request body")
		}
		return ImageUpdateOperation{Action: "update_selected", ImageIDs: req.ImageIDs, Summary: "Updating selected Docker images"}, true, nil
	default:
		return ImageUpdateOperation{}, false, nil
	}
}

func resourceOperation(r *http.Request) (ResourceOperation, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		return ResourceOperation{}, false
	}
	resource := parts[5]
	if resource == "containers" {
		if len(parts) < 7 {
			return ResourceOperation{}, false
		}
		id, err := url.PathUnescape(parts[6])
		if err != nil || strings.TrimSpace(id) == "" {
			return ResourceOperation{}, false
		}
		if r.Method == http.MethodDelete && len(parts) == 7 {
			return ResourceOperation{Kind: "container", Action: "delete", ID: id, Summary: "Deleting Docker container " + id}, true
		}
		if r.Method == http.MethodPost && len(parts) == 8 && (parts[7] == "start" || parts[7] == "stop" || parts[7] == "restart") {
			return ResourceOperation{Kind: "container", Action: parts[7], ID: id, Summary: strings.Title(parts[7]) + "ing Docker container " + id}, true
		}
		return ResourceOperation{}, false
	}
	switch resource {
	case "networks", "volumes", "images":
	default:
		return ResourceOperation{}, false
	}
	kind := strings.TrimSuffix(resource, "s")
	if r.Method == http.MethodPost && len(parts) == 7 && parts[6] == "prune" {
		return ResourceOperation{Kind: kind, Action: "prune", Summary: "Pruning unused Docker " + resource}, true
	}
	if r.Method == http.MethodDelete && len(parts) == 7 {
		id, err := url.PathUnescape(parts[6])
		if err != nil || strings.TrimSpace(id) == "" {
			return ResourceOperation{}, false
		}
		return ResourceOperation{Kind: kind, Action: "delete", ID: id, Summary: "Deleting Docker " + kind + " " + id}, true
	}
	return ResourceOperation{}, false
}

func notImplementedOperation(r *http.Request) (string, string, error) {
	path := r.URL.Path
	switch {
	case strings.Contains(path, "/docker/networks/"):
		if strings.HasSuffix(path, "/prune") {
			return "docker_network_prune", "Pruning unused Docker networks", nil
		}
		return "docker_network_delete", "Deleting Docker network", nil
	case strings.Contains(path, "/docker/volumes/"):
		if strings.HasSuffix(path, "/prune") {
			return "docker_volume_prune", "Pruning unused Docker volumes", nil
		}
		return "docker_volume_delete", "Deleting Docker volume", nil
	case strings.Contains(path, "/docker/images/"):
		switch {
		case strings.HasSuffix(path, "/check-updates"):
			return "docker_image_update_check", "Checking Docker image updates", nil
		case strings.HasSuffix(path, "/update-selected"):
			return "docker_image_update_selected", "Updating selected Docker images", nil
		case strings.HasSuffix(path, "/update-all"):
			return "docker_image_update_all", "Updating all Docker images", nil
		case strings.HasSuffix(path, "/prune"):
			return "docker_image_prune", "Pruning unused Docker images", nil
		default:
			return "docker_image_delete", "Deleting Docker image", nil
		}
	default:
		return "", "", panelerr.NotFound("route")
	}
}

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func runtimeExplorerNodeID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}

func runtimeExplorerOperation(r *http.Request) (ResourceOperation, bool) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 7 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "runtime-explorer" || parts[3] != "nodes" {
		return ResourceOperation{}, false
	}
	if len(parts) >= 7 && parts[5] == "containers" {
		id, err := url.PathUnescape(parts[6])
		if err != nil || strings.TrimSpace(id) == "" {
			return ResourceOperation{}, false
		}
		if r.Method == http.MethodDelete && len(parts) == 7 {
			return ResourceOperation{Kind: "container", Action: "delete", ID: id, Summary: "Deleting Docker container " + id}, true
		}
		if r.Method == http.MethodPost && len(parts) == 8 && (parts[7] == "restart" || parts[7] == "stop") {
			return ResourceOperation{Kind: "container", Action: parts[7], ID: id, Summary: strings.Title(parts[7]) + "ing Docker container " + id}, true
		}
	}
	if r.Method == http.MethodPost && len(parts) == 6 && parts[5] == "prune" {
		return ResourceOperation{Kind: "image", Action: "prune", Summary: "Pruning unused Docker resources"}, true
	}
	return ResourceOperation{}, false
}

func serverAndProject(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 7 {
		return parts[3], parts[6]
	}
	return serverID(path), ""
}
