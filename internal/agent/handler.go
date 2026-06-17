package agent

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	collector LocalCollector
	runtime   *LocalRuntime
}

type HandlerConfig struct {
	DockerHost string
}

func NewHandler(cfg ...HandlerConfig) *Handler {
	dockerHost := DefaultDockerHost
	if len(cfg) > 0 && strings.TrimSpace(cfg[0].DockerHost) != "" {
		dockerHost = cfg[0].DockerHost
	}
	runtime, _ := NewLocalRuntime(dockerHost)
	return &Handler{collector: LocalCollector{}, runtime: runtime}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if strings.HasPrefix(path, "/v1/docker/") && h.runtime == nil {
		writeError(w, http.StatusBadGateway, "runtime is not configured")
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/v1/health":
		docker := DockerHealth{Host: DefaultDockerHost, Status: StatusUnavailable, Error: "runtime is not configured"}
		if h.runtime != nil {
			docker = h.runtime.DockerHealth(r.Context())
		}
		writeJSON(w, HealthResponse{Status: "ok", Time: time.Now().UTC().Format(time.RFC3339Nano), Version: Version, Capabilities: RequiredCapabilities, Docker: docker})
	case r.Method == http.MethodGet && path == "/v1/system/os-release":
		info, err := h.collector.OSRelease(r.Context())
		writeResult(w, OSReleaseResponse{OSRelease: info}, err)
	case r.Method == http.MethodGet && path == "/v1/system/traits":
		traits, err := h.collector.SystemTraits(r.Context())
		writeResult(w, SystemTraitsResponse{Traits: traits}, err)
	case r.Method == http.MethodGet && path == "/v1/metrics/snapshot":
		serverID := strings.TrimSpace(r.URL.Query().Get("serverId"))
		snap, err := h.collector.MetricsSnapshot(r.Context(), serverID)
		writeResult(w, SnapshotResponse(snap), err)
	case r.Method == http.MethodGet && path == "/v1/ufw/status":
		status, err := h.collector.UFWStatus(r.Context())
		writeResult(w, UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodGet && path == "/v1/docker/containers":
		items, err := h.runtime.Containers(r.Context())
		writeResult(w, DockerContainersResponse{Items: items}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/docker/containers/") && strings.HasSuffix(path, "/logs"):
		id := dockerContainerLogsID(path)
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		logs, err := h.runtime.ContainerLogs(r.Context(), id, tail)
		writeResult(w, DockerContainerLogsResponse{ContainerID: id, Logs: logs}, err)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/docker/containers/"):
		id, action := dockerContainerAction(path)
		var err error
		switch action {
		case "start":
			err = h.runtime.ContainerStart(r.Context(), id)
		case "stop":
			err = h.runtime.ContainerStop(r.Context(), id)
		case "restart":
			err = h.runtime.ContainerRestart(r.Context(), id)
		default:
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/docker/containers/"):
		err := h.runtime.ContainerDelete(r.Context(), strings.TrimPrefix(path, "/v1/docker/containers/"))
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodGet && path == "/v1/docker/images":
		items, err := h.runtime.Images(r.Context())
		writeResult(w, DockerImagesResponse{Items: items}, err)
	case r.Method == http.MethodPost && path == "/v1/docker/images/pull":
		var req DockerImagePullRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		err := h.runtime.PullImage(r.Context(), req.Reference)
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/docker/images/"):
		err := h.runtime.DeleteImage(r.Context(), strings.TrimPrefix(path, "/v1/docker/images/"))
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodGet && path == "/v1/docker/networks":
		items, err := h.runtime.Networks(r.Context())
		writeResult(w, DockerNetworksResponse{Items: items}, err)
	case r.Method == http.MethodGet && path == "/v1/docker/volumes":
		items, err := h.runtime.Volumes(r.Context())
		writeResult(w, DockerVolumesResponse{Items: items}, err)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/docker/volumes/"):
		err := h.runtime.DeleteVolume(r.Context(), strings.TrimPrefix(path, "/v1/docker/volumes/"))
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/deploy":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req RuntimeDeployRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Deploy(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/stop":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req RuntimeStopRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Stop(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/restart":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req RuntimeRestartRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Restart(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/status"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		instanceID := runtimePathInstanceID(path, "/status")
		status, err := h.runtime.Status(r.Context(), instanceID, r.URL.Query().Get("containerName"), "")
		writeResult(w, RuntimeStatusResponse{InstanceStatus: status}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/logs"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		instanceID := runtimePathInstanceID(path, "/logs")
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		logs, err := h.runtime.Logs(r.Context(), instanceID, r.URL.Query().Get("containerName"), tail)
		writeResult(w, RuntimeLogsResponse{InstanceID: instanceID, Logs: logs}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/persistent/archive"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		applicationID := runtimePathInstanceID(path, "/persistent/archive")
		content, err := h.runtime.PersistentArchive(r.Context(), applicationID)
		writeResult(w, RuntimePersistentArchiveResponse{
			ApplicationID: applicationID,
			Filename:      applicationID + "-persistent.zip",
			ContentBase64: base64.StdEncoding.EncodeToString(content),
		}, err)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/persistent/restore"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		applicationID := runtimePathInstanceID(path, "/persistent/restore")
		var req RuntimePersistentRestoreRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.ContentBase64))
		if err == nil {
			err = h.runtime.RestorePersistentArchive(r.Context(), applicationID, content)
		}
		writeResult(w, RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: err == nil}, err)
	case r.Method != http.MethodGet && r.Method != http.MethodPost:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func dockerContainerAction(path string) (string, string) {
	parts := strings.Split(strings.TrimPrefix(path, "/v1/docker/containers/"), "/")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func dockerContainerLogsID(path string) string {
	value := strings.TrimPrefix(path, "/v1/docker/containers/")
	value = strings.TrimSuffix(value, "/logs")
	return strings.Trim(value, "/")
}

func (h *Handler) oldServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimSuffix(r.URL.Path, "/") {
	case "/v1/health":
		writeJSON(w, HealthResponse{Status: "ok", Time: time.Now().UTC().Format(time.RFC3339Nano), Version: Version, Capabilities: RequiredCapabilities})
	case "/v1/system/os-release":
		info, err := h.collector.OSRelease(r.Context())
		writeResult(w, OSReleaseResponse{OSRelease: info}, err)
	case "/v1/system/traits":
		traits, err := h.collector.SystemTraits(r.Context())
		writeResult(w, SystemTraitsResponse{Traits: traits}, err)
	case "/v1/metrics/snapshot":
		serverID := strings.TrimSpace(r.URL.Query().Get("serverId"))
		snap, err := h.collector.MetricsSnapshot(r.Context(), serverID)
		writeResult(w, SnapshotResponse(snap), err)
	case "/v1/ufw/status":
		status, err := h.collector.UFWStatus(r.Context())
		writeResult(w, UFWStatusResponseFromStatus(status), err)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func runtimePathInstanceID(pathValue, suffix string) string {
	pathValue = strings.TrimSuffix(pathValue, suffix)
	pathValue = strings.TrimPrefix(pathValue, "/v1/runtime/applications/")
	return strings.Trim(pathValue, "/")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
