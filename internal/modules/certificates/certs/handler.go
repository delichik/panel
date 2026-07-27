package certs

import (
	"context"
	"net/http"
	"strings"

	"panel/internal/platform/http"
)

type certificateService interface {
	List(ctx context.Context) ([]Certificate, error)
	ListSummaries(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[CertificateSummary], error)
	Get(ctx context.Context, certID string) (Certificate, error)
	Issue(ctx context.Context, in IssueRequest) (IssueResult, error)
	Reissue(ctx context.Context, certID string, in IssueRequest) (IssueResult, error)
	Delete(ctx context.Context, certID string) error
	Renew(ctx context.Context, certID string) error
	ListSelfSigned(ctx context.Context) ([]SelfSignedCertificate, error)
	ListSelfSignedPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[SelfSignedCertificate], error)
	CreateSelfSignedCA(ctx context.Context, in SelfSignedCARequest) (SelfSignedCertificate, error)
	CreateSelfSignedLeaf(ctx context.Context, in SelfSignedLeafRequest) (SelfSignedCertificate, error)
	RenewSelfSignedLeaf(ctx context.Context, certID string) (SelfSignedCertificate, error)
	DeleteSelfSigned(ctx context.Context, certID string) error
}

func (h *Handler) Renew(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Renew(r.Context(), certificateIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"renewed": true})
}

func (h *Handler) ListSelfSigned(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.ListSelfSignedPage(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CreateSelfSignedCA(w http.ResponseWriter, r *http.Request) {
	var in SelfSignedCARequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CreateSelfSignedCA(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) CreateSelfSignedLeaf(w http.ResponseWriter, r *http.Request) {
	var in SelfSignedLeafRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CreateSelfSignedLeaf(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) RenewSelfSignedLeaf(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RenewSelfSignedLeaf(r.Context(), certificateIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteSelfSigned(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSelfSigned(r.Context(), certificateIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

type Handler struct {
	service certificateService
}

func NewHandler(service certificateService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	certs, err := h.service.ListSummaries(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, certs)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	cert, err := h.service.Get(r.Context(), certificateIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cert)
}

func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	var in IssueRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.Issue(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) Reissue(w http.ResponseWriter, r *http.Request) {
	var in IssueRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.Reissue(r.Context(), certificateIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, result)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), certificateIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func certificateIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}
