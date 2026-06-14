package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	collector LocalCollector
}

func NewHandler() *Handler {
	return &Handler{collector: LocalCollector{}}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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
