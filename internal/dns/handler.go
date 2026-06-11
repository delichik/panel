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
	ListRecords(ctx context.Context, domainID string) ([]Record, error)
	CreateRecord(ctx context.Context, domainID string, in RecordInput) (Record, error)
	UpdateRecord(ctx context.Context, domainID, recordID string, in RecordInput) (Record, error)
	DeleteRecord(ctx context.Context, domainID, recordID string) error
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

func (h *Handler) ListRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.service.ListRecords(r.Context(), domainID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, records)
}

func (h *Handler) CreateRecord(w http.ResponseWriter, r *http.Request) {
	var in RecordInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	record, err := h.service.CreateRecord(r.Context(), domainID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, record)
}

func (h *Handler) UpdateRecord(w http.ResponseWriter, r *http.Request) {
	var in RecordInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	record, err := h.service.UpdateRecord(r.Context(), domainID(r.URL.Path), recordID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, record)
}

func (h *Handler) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRecord(r.Context(), domainID(r.URL.Path), recordID(r.URL.Path)); err != nil {
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

func recordID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 7 {
		return parts[6]
	}
	return ""
}
