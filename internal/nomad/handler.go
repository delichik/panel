package nomad

import (
	"context"
	"net/http"

	"panel/internal/httpx"
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
}

func NewHandler(client inventoryClient) *Handler {
	return &Handler{client: client}
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
