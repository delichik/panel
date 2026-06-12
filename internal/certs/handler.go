package certs

import (
	"context"
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type certificateService interface {
	List(ctx context.Context) ([]Certificate, error)
	Issue(ctx context.Context, in IssueRequest) (IssueResult, error)
	Delete(ctx context.Context, certID string) error
	Renew(ctx context.Context, certID string) error
	ListSelfSigned(ctx context.Context) ([]SelfSignedCertificate, error)
	CreateSelfSignedCA(ctx context.Context, in SelfSignedCARequest) (SelfSignedCertificate, error)
	CreateSelfSignedLeaf(ctx context.Context, in SelfSignedLeafRequest) (SelfSignedCertificate, error)
	RenewSelfSignedLeaf(ctx context.Context, certID string) (SelfSignedCertificate, error)
	DeleteSelfSigned(ctx context.Context, certID string) error
}

func (h *Handler) Renew(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Renew(r.Context(), certificateID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"renewed": true})
}

func (h *Handler) ListSelfSigned(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListSelfSigned(r.Context())
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
	result, err := h.service.RenewSelfSignedLeaf(r.Context(), selfSignedCertificateID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteSelfSigned(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSelfSigned(r.Context(), selfSignedCertificateID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func selfSignedCertificateID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

type Handler struct {
	service certificateService
}

func NewHandler(service certificateService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	certs, err := h.service.List(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, certs)
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

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), certificateID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func certificateID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}
