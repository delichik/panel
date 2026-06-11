package nomad

import (
	"context"
	"net/http"

	"panel/internal/httpx"
	"panel/internal/server"
	"panel/internal/tasks"
)

type inventoryClient interface {
	Status(ctx context.Context) (StatusResponse, error)
	Nodes(ctx context.Context) ([]NodeListItem, error)
}

type Handler struct {
	client inventoryClient
	join   joinService
}

type joinService interface {
	ControlPlane(ctx context.Context) (ControlPlane, error)
	Candidates(ctx context.Context) ([]server.Server, error)
	JoinClient(ctx context.Context, serverID string) (tasks.Task, error)
	BootstrapServer(ctx context.Context, in BootstrapServerInput) (tasks.Task, error)
	RedeployNode(ctx context.Context, in RedeployNodeInput) (tasks.Task, error)
	RebuildCluster(ctx context.Context, in RebuildClusterInput) (tasks.Task, error)
	SwitchServer(ctx context.Context, in SwitchServerInput) (tasks.Task, error)
	RemoveNode(ctx context.Context, in RemoveNodeInput) (tasks.Task, error)
	UpdateReverseProxy(ctx context.Context, in ReverseProxyInput) (ReverseProxyUpdateResult, error)
}

func NewHandler(client inventoryClient, join ...joinService) *Handler {
	var joinSvc joinService
	if len(join) > 0 {
		joinSvc = join[0]
	}
	return &Handler{client: client, join: joinSvc}
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.Status(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.Nodes(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ControlPlane(w http.ResponseWriter, r *http.Request) {
	result, err := h.join.ControlPlane(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) JoinCandidates(w http.ResponseWriter, r *http.Request) {
	result, err := h.join.Candidates(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) JoinClient(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServerID string `json:"serverId"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.JoinClient(r.Context(), req.ServerID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) BootstrapServer(w http.ResponseWriter, r *http.Request) {
	var req BootstrapServerInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.BootstrapServer(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) RedeployNode(w http.ResponseWriter, r *http.Request) {
	var req RedeployNodeInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.RedeployNode(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) RebuildCluster(w http.ResponseWriter, r *http.Request) {
	var req RebuildClusterInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.RebuildCluster(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) SwitchServer(w http.ResponseWriter, r *http.Request) {
	var req SwitchServerInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.SwitchServer(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) RemoveNode(w http.ResponseWriter, r *http.Request) {
	var req RemoveNodeInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.RemoveNode(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func (h *Handler) UpdateReverseProxy(w http.ResponseWriter, r *http.Request) {
	var req ReverseProxyInput
	if !httpx.Decode(w, r, &req) {
		return
	}
	result, err := h.join.UpdateReverseProxy(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
