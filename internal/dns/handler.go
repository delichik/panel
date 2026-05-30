package dns

import (
	"context"
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type domainService interface {
	ListDomains(ctx context.Context) ([]Domain, error)
	CreateDomain(ctx context.Context, in SaveDomainRequest) (Domain, error)
	UpdateDomain(ctx context.Context, domainID string, in SaveDomainRequest) (Domain, error)
	DeleteDomain(ctx context.Context, domainID string) error
}

type Handler struct {
	service domainService
}

func NewHandler(service domainService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := h.service.ListDomains(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, domains)
}

func (h *Handler) CreateDomain(w http.ResponseWriter, r *http.Request) {
	var in SaveDomainRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	domain, err := h.service.CreateDomain(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, domain)
}

func (h *Handler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	var in SaveDomainRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	domain, err := h.service.UpdateDomain(r.Context(), domainID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, domain)
}

func (h *Handler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteDomain(r.Context(), domainID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func domainID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 5 {
		return parts[4]
	}
	return ""
}
