package containerization

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/servers/{serverId}/containers", auth(http.HandlerFunc(h.Containers)))
	mux.Handle("GET /api/v1/servers/{serverId}/containers/{resourceId}/logs", auth(http.HandlerFunc(h.ContainerLogs)))
	mux.Handle("POST /api/v1/servers/{serverId}/containers/{resourceId}/{action}", auth(http.HandlerFunc(h.ContainerAction)))
	mux.Handle("DELETE /api/v1/servers/{serverId}/containers/{resourceId}", auth(http.HandlerFunc(h.DeleteContainer)))

	mux.Handle("GET /api/v1/servers/{serverId}/images", auth(http.HandlerFunc(h.Images)))
	mux.Handle("POST /api/v1/servers/{serverId}/images/pull", auth(http.HandlerFunc(h.PullImage)))
	mux.Handle("POST /api/v1/servers/{serverId}/images/refresh", auth(http.HandlerFunc(h.RefreshImages)))
	mux.Handle("POST /api/v1/servers/{serverId}/images/delete-unused", auth(http.HandlerFunc(h.DeleteUnusedImages)))
	mux.Handle("DELETE /api/v1/servers/{serverId}/images/{resourceId}", auth(http.HandlerFunc(h.DeleteImage)))
	mux.Handle("POST /api/v1/images/upgrade-selected", auth(http.HandlerFunc(h.UpgradeSelected)))
	mux.Handle("POST /api/v1/images/upgrade-all", auth(http.HandlerFunc(h.UpgradeAll)))

	mux.Handle("GET /api/v1/servers/{serverId}/networks", auth(http.HandlerFunc(h.Networks)))
	mux.Handle("POST /api/v1/servers/{serverId}/networks/refresh", auth(http.HandlerFunc(h.RefreshNetworks)))
	mux.Handle("GET /api/v1/servers/{serverId}/volumes", auth(http.HandlerFunc(h.Volumes)))
	mux.Handle("POST /api/v1/servers/{serverId}/volumes/refresh", auth(http.HandlerFunc(h.RefreshVolumes)))
	mux.Handle("POST /api/v1/servers/{serverId}/volumes/delete-unused", auth(http.HandlerFunc(h.DeleteUnusedVolumes)))
	mux.Handle("DELETE /api/v1/servers/{serverId}/volumes/{resourceId}", auth(http.HandlerFunc(h.DeleteVolume)))
}
