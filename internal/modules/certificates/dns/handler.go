package dns

import (
	"context"
	"net/http"
	"strings"

	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
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

type recordRefreshService interface {
	RefreshRecords(context.Context, string) (tasks.Task, error)
}

type recordSnapshotService interface {
	ListRecordSnapshot(context.Context, string) (RecordSnapshot, error)
}

type Handler struct {
	service domainService
}

func NewHandler(service domainService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListDomains(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	service, ok := h.service.(interface {
		ListDomainPage(context.Context, int, int, string) (httpx.ListPage[Domain], error)
	})
	if !ok {
		httpx.Error(w, panelerr.BadGateway("domain_list_unavailable", "Domain summary list is unavailable"))
		return
	}
	domains, err := service.ListDomainPage(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
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
	domain, err := h.service.UpdateDomain(r.Context(), domainIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, domain)
}

func (h *Handler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteDomain(r.Context(), domainIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ListRecords(w http.ResponseWriter, r *http.Request) {
	if snapshots, ok := h.service.(recordSnapshotService); ok {
		result, err := snapshots.ListRecordSnapshot(r.Context(), domainIDFromRequest(r))
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, result)
		return
	}
	records, err := h.service.ListRecords(r.Context(), domainIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, records)
}

func (h *Handler) RefreshRecords(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(recordRefreshService)
	if !ok {
		httpx.Error(w, panelerr.Validation("task_service_unavailable", "DNS record refresh is unavailable"))
		return
	}
	task, err := service.RefreshRecords(r.Context(), domainIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"taskId": task.ID})
}

func (h *Handler) CreateRecord(w http.ResponseWriter, r *http.Request) {
	var in RecordInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	record, err := h.service.CreateRecord(r.Context(), domainIDFromRequest(r), in)
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
	record, err := h.service.UpdateRecord(r.Context(), domainIDFromRequest(r), recordIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, record)
}

func (h *Handler) DeleteRecord(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRecord(r.Context(), domainIDFromRequest(r), recordIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func domainIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("domainId"))
}

func recordIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("recordId"))
}
