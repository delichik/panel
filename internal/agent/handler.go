package agent

import (
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
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/deploy":
		var req RuntimeDeployRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Deploy(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/stop":
		var req RuntimeStopRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Stop(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodPost && path == "/v1/runtime/applications/restart":
		var req RuntimeRestartRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		result, err := h.runtime.Restart(r.Context(), req)
		writeResult(w, result, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/status"):
		instanceID := runtimePathInstanceID(path, "/status")
		status, err := h.runtime.Status(r.Context(), instanceID, "", "")
		writeResult(w, RuntimeStatusResponse{InstanceStatus: status}, err)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/v1/runtime/applications/") && strings.HasSuffix(path, "/logs"):
		instanceID := runtimePathInstanceID(path, "/logs")
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		logs, err := h.runtime.Logs(r.Context(), instanceID, tail)
		writeResult(w, RuntimeLogsResponse{InstanceID: instanceID, Logs: logs}, err)
	case r.Method != http.MethodGet && r.Method != http.MethodPost:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
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
