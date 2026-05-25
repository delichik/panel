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
	ListJobs(ctx context.Context, prefix string) ([]JobListItem, error)
	Deployments(ctx context.Context) ([]Deployment, error)
	Evaluations(ctx context.Context) ([]Evaluation, error)
	Services(ctx context.Context) ([]ServiceRegistration, error)
}

type Handler struct {
	client inventoryClient
	join   joinService
}

type joinService interface {
	ControlPlane(ctx context.Context) (ControlPlane, error)
	Candidates(ctx context.Context) ([]server.Server, error)
	JoinClient(ctx context.Context, serverID string) (tasks.Task, error)
	BootstrapServer(ctx context.Context, serverID string) (tasks.Task, error)
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

func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.ListJobs(r.Context(), r.URL.Query().Get("prefix"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Deployments(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.Deployments(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Evaluations(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.Evaluations(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Services(w http.ResponseWriter, r *http.Request) {
	result, err := h.client.Services(r.Context())
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
	var req struct {
		ServerID string `json:"serverId"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	task, err := h.join.BootstrapServer(r.Context(), req.ServerID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}
