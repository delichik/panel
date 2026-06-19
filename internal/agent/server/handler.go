package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	agentcontract "panel/internal/agent/contract"
	agentdocker "panel/internal/agent/docker"
	agentsystem "panel/internal/agent/system"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	collector agentsystem.LocalCollector
	runtime   *agentdocker.LocalRuntime
}

type HandlerConfig struct {
	DockerHost string
}

func NewHandler(cfg ...HandlerConfig) *Handler {
	dockerHost := agentcontract.DefaultDockerHost
	if len(cfg) > 0 && strings.TrimSpace(cfg[0].DockerHost) != "" {
		dockerHost = cfg[0].DockerHost
	}
	runtime, _ := agentdocker.NewLocalRuntime(dockerHost)
	return &Handler{collector: agentsystem.LocalCollector{}, runtime: runtime}
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
		docker := agentcontract.DockerHealth{Host: agentcontract.DefaultDockerHost, Status: agentcontract.StatusUnavailable, Error: "runtime is not configured"}
		if h.runtime != nil {
			docker = h.runtime.DockerHealth(r.Context())
		}
		writeJSON(w, agentcontract.HealthResponse{Status: "ok", Time: time.Now().UTC().Format(time.RFC3339Nano), Version: agentcontract.Version, Capabilities: agentcontract.RequiredCapabilities, Contract: agentcontract.CurrentContract(), Docker: docker})
	case r.Method == http.MethodGet && path == "/v1/system/os-release":
		info, err := h.collector.OSRelease(r.Context())
		writeResult(w, agentcontract.OSReleaseResponse{OSRelease: info}, err)
	case r.Method == http.MethodGet && path == "/v1/system/traits":
		traits, err := h.collector.SystemTraits(r.Context())
		writeResult(w, agentcontract.SystemTraitsResponse{Traits: traits}, err)
	case r.Method == http.MethodGet && path == "/v1/metrics/snapshot":
		serverID := strings.TrimSpace(r.URL.Query().Get("serverId"))
		snap, err := h.collector.MetricsSnapshot(r.Context(), serverID)
		writeResult(w, agentcontract.SnapshotResponse(snap), err)
	case r.Method == http.MethodGet && path == "/v1/system/packages/updates":
		items, err := h.collector.PackageUpdates(r.Context())
		writeResult(w, agentcontract.PackageUpdatesResponse{Items: items}, err)
	case r.Method == http.MethodPost && path == "/v1/system/packages/upgrade":
		var req agentcontract.PackageUpgradeRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		output, err := h.collector.UpgradePackages(r.Context(), req)
		writeResult(w, agentcontract.CommandResponse{Output: output}, err)
	case r.Method == http.MethodGet && path == "/v1/ufw/status":
		status, err := h.collector.UFWStatus(r.Context())
		writeResult(w, agentcontract.UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodPost && path == "/v1/ufw/install":
		var req agentcontract.UFWInstallRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		status, err := h.collector.InstallUFW(r.Context(), req)
		writeResult(w, agentcontract.UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodPost && path == "/v1/ufw/enable":
		var req agentcontract.UFWEnableRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		status, err := h.collector.EnableUFW(r.Context(), req)
		writeResult(w, agentcontract.UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodPost && path == "/v1/ufw/rules":
		var req agentcontract.UFWAllowRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		status, err := h.collector.AllowUFW(r.Context(), req)
		writeResult(w, agentcontract.UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodPost && path == "/v1/ufw/rules/delete":
		var req agentcontract.UFWDeleteRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		status, err := h.collector.DeleteUFW(r.Context(), req)
		writeResult(w, agentcontract.UFWStatusResponseFromStatus(status), err)
	case r.Method == http.MethodPost && path == "/v1/system/restart":
		err := h.collector.RestartSystem(r.Context())
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodGet && path == "/v1/docker/containers":
		items, err := h.runtime.Containers(r.Context())
		writeResult(w, agentcontract.DockerContainersResponse{Items: items}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/docker/containers/") && strings.HasSuffix(path, "/logs"):
		id := dockerContainerLogsID(path)
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		logs, err := h.runtime.ContainerLogs(r.Context(), id, tail)
		writeResult(w, agentcontract.DockerContainerLogsResponse{ContainerID: id, Logs: logs}, err)
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
		writeResult(w, agentcontract.DockerImagesResponse{Items: items}, err)
	case r.Method == http.MethodPost && path == "/v1/docker/images/pull":
		var req agentcontract.DockerImagePullRequest
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
		writeResult(w, agentcontract.DockerNetworksResponse{Items: items}, err)
	case r.Method == http.MethodGet && path == "/v1/docker/volumes":
		items, err := h.runtime.Volumes(r.Context())
		writeResult(w, agentcontract.DockerVolumesResponse{Items: items}, err)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/docker/volumes/"):
		err := h.runtime.DeleteVolume(r.Context(), strings.TrimPrefix(path, "/v1/docker/volumes/"))
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/files":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req agentcontract.RuntimeWriteFilesRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		err := h.runtime.WriteManagedFiles(r.Context(), req.Spec)
		writeResult(w, map[string]bool{"ok": err == nil}, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/containers/create":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req agentcontract.RuntimeCreateContainerRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		id, err := h.runtime.CreateContainer(r.Context(), req.Spec)
		writeResult(w, agentcontract.RuntimeCreateContainerResponse{ContainerID: id}, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/stop":
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		var req agentcontract.RuntimeStopRequest
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
		var req agentcontract.RuntimeRestartRequest
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
		writeResult(w, agentcontract.RuntimeStatusResponse{InstanceStatus: status}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/logs"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		instanceID := runtimePathInstanceID(path, "/logs")
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		logs, err := h.runtime.Logs(r.Context(), instanceID, r.URL.Query().Get("containerName"), tail)
		writeResult(w, agentcontract.RuntimeLogsResponse{InstanceID: instanceID, Logs: logs}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/persistent/archive"):
		if h.runtime == nil {
			writeError(w, http.StatusBadGateway, "runtime is not configured")
			return
		}
		applicationID := runtimePathInstanceID(path, "/persistent/archive")
		content, err := h.runtime.PersistentArchive(r.Context(), applicationID)
		writeResult(w, agentcontract.RuntimePersistentArchiveResponse{
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
		var req agentcontract.RuntimePersistentRestoreRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.ContentBase64))
		if err == nil {
			err = h.runtime.RestorePersistentArchive(r.Context(), applicationID, content)
		}
		writeResult(w, agentcontract.RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: err == nil}, err)
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
	_ = json.NewEncoder(w).Encode(agentcontract.ErrorResponse{Error: message})
}
