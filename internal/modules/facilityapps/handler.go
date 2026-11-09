package facilityapps

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type service interface {
	GetReverseProxy(ctx context.Context) (ReverseProxyConfig, error)
	GetProxyPlane(ctx context.Context) (ProxyPlaneState, error)
	SaveProxyPlane(ctx context.Context, in ProxyPlaneSaveInput) (ProxyPlaneState, error)
	ListConfigRecords(ctx context.Context) ([]ConfigRecord, error)
	GetConfigRecord(ctx context.Context, recordID string) (ConfigRecord, error)
	DeleteConfigRecord(ctx context.Context, recordID string, expectedVersion int) error
	ApplyConfigRecord(ctx context.Context, recordID string) error
	ReconcileReverseProxyNow(ctx context.Context, in ReconcileReverseProxyInput) (ReconcileResult, error)
	RequestDNSSync(ctx context.Context, req DNSSyncRequest) (DNSSyncResult, error)
	BeginFacilityEditSession(context.Context, BeginFacilityEditSessionInput) (FacilityEditSession, error)
	PatchFacilityEditSession(context.Context, string, PatchFacilityEditSessionInput) (FacilityEditSession, error)
	PutFacilityEditAsset(context.Context, string, string, string, FacilityEditAssetInput) (FacilityEditSession, error)
	DeleteFacilityEditAsset(context.Context, string, string, string, FacilityEditMutationInput) (FacilityEditSession, error)
	ValidateFacilityEditSession(context.Context, string, int) (FacilityEditValidationResult, error)
	PreviewFacilityEditSession(context.Context, string, int) (FacilityEditPreviewResult, error)
	CommitFacilityEditSession(context.Context, string, string, CommitFacilityEditSessionInput) (FacilityEditCommitResult, error)
	DiscardFacilityEditSession(context.Context, string) error
}

func (h *Handler) BeginFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	var in BeginFacilityEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.BeginFacilityEditSession(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) PatchFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	var in PatchFacilityEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.PatchFacilityEditSession(r.Context(), r.PathValue("id"), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) PutFacilityEditAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		httpx.Error(w, err)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	revision, _ := strconv.Atoi(r.FormValue("revision"))
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.Error(w, panelerr.Validation("idempotency_key_required", "Idempotency-Key header is required"))
		return
	}
	result, err := h.service.PutFacilityEditAsset(r.Context(), r.PathValue("id"), r.PathValue("assetName"), key, FacilityEditAssetInput{Revision: revision, ClientOperationID: r.FormValue("clientOperationId"), Name: r.FormValue("name"), Kind: r.FormValue("kind"), ContentMode: r.FormValue("contentMode"), FileName: header.Filename, Content: content})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteFacilityEditAsset(w http.ResponseWriter, r *http.Request) {
	var in FacilityEditMutationInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.Error(w, panelerr.Validation("idempotency_key_required", "Idempotency-Key header is required"))
		return
	}
	result, err := h.service.DeleteFacilityEditAsset(r.Context(), r.PathValue("id"), r.PathValue("assetName"), key, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ValidateFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	h.handleFacilityEditRevision(w, r, false)
}

func (h *Handler) PreviewFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	h.handleFacilityEditRevision(w, r, true)
}

func (h *Handler) handleFacilityEditRevision(w http.ResponseWriter, r *http.Request, preview bool) {
	var in struct {
		Revision int `json:"revision"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if preview {
		result, err := h.service.PreviewFacilityEditSession(r.Context(), r.PathValue("id"), in.Revision)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, result)
		return
	}
	result, err := h.service.ValidateFacilityEditSession(r.Context(), r.PathValue("id"), in.Revision)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CommitFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	var in CommitFacilityEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.Error(w, panelerr.Validation("idempotency_key_required", "Idempotency-Key header is required"))
		return
	}
	result, err := h.service.CommitFacilityEditSession(r.Context(), r.PathValue("id"), key, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DiscardFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DiscardFacilityEditSession(r.Context(), r.PathValue("id")); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ReverseProxy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetReverseProxy(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ProxyPlane(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetProxyPlane(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) SaveProxyPlane(w http.ResponseWriter, r *http.Request) {
	var in ProxyPlaneSaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.SaveProxyPlane(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ConfigRecords(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListConfigRecords(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if result == nil {
		result = []ConfigRecord{}
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ConfigRecord(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetConfigRecord(r.Context(), r.PathValue("recordId"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteConfigRecord(w http.ResponseWriter, r *http.Request) {
	version, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("version")))
	if err := h.service.DeleteConfigRecord(r.Context(), r.PathValue("recordId"), version); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ApplyConfigRecord(w http.ResponseWriter, r *http.Request) {
	recordID := r.PathValue("recordId")
	if err := h.service.ApplyConfigRecord(r.Context(), recordID); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]string{"recordId": recordID})
}

func (h *Handler) ReconcileReverseProxy(w http.ResponseWriter, r *http.Request) {
	var in ReconcileReverseProxyInput
	if r.Body != nil && r.ContentLength != 0 {
		if !httpx.Decode(w, r, &in) {
			return
		}
	}
	result, err := h.service.ReconcileReverseProxyNow(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) SyncReverseProxyDNS(w http.ResponseWriter, r *http.Request) {
	var in DNSSyncRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.RequestDNSSync(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, result)
}
