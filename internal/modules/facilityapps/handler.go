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
	SaveReverseProxy(ctx context.Context, in ReverseProxySaveInput) (ReverseProxyConfig, error)
	ReconcileReverseProxyNow(ctx context.Context) (ReconcileResult, error)
	ListStaticAssets(ctx context.Context) ([]StaticAsset, error)
	UploadStaticAsset(ctx context.Context, in StaticAssetUploadInput) (StaticAsset, error)
	DeleteStaticAsset(ctx context.Context, assetID string) error
	BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error)
	UploadSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetUploadInput) (StaticAsset, error)
	DeleteSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetDeleteInput) error
	CommitSaveSession(ctx context.Context, sessionID string, in CommitSaveSessionInput) (SaveSessionCommitResult, error)
	DiscardSaveSession(sessionID string)
	BeginFacilityEditSession(context.Context, BeginFacilityEditSessionInput) (FacilityEditSession, error)
	RecoverableFacilityEditSessions(context.Context, string) ([]FacilityEditSession, error)
	GetFacilityEditSession(context.Context, string) (FacilityEditSession, error)
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

func (h *Handler) RecoverableFacilityEditSessions(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.RecoverableFacilityEditSessions(r.Context(), r.URL.Query().Get("clientDraftKey"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetFacilityEditSession(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetFacilityEditSession(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
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
	result, err := h.service.PutFacilityEditAsset(r.Context(), r.PathValue("id"), r.PathValue("assetKey"), key, FacilityEditAssetInput{Revision: revision, ClientOperationID: r.FormValue("clientOperationId"), Name: r.FormValue("name"), Kind: r.FormValue("kind"), FileName: header.Filename, Content: content})
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
	result, err := h.service.DeleteFacilityEditAsset(r.Context(), r.PathValue("id"), r.PathValue("assetKey"), key, in)
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

type facilitySummaryListService interface {
	ListSummaries(context.Context) ([]FacilityAppSummary, error)
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if summaryList, ok := h.service.(facilitySummaryListService); ok {
		summaries, err := summaryList.ListSummaries(r.Context())
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, summaries)
		return
	}
	config, err := h.service.GetReverseProxy(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, []FacilityAppSummary{facilitySummaryFromReverseProxy(config)})
}

func (h *Handler) ReverseProxy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetReverseProxy(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func facilitySummaryFromReverseProxy(config ReverseProxyConfig) FacilityAppSummary {
	status := "available"
	if config.LastError != "" {
		status = "degraded"
	}
	operationStatus := ""
	if config.Operation != nil {
		operationStatus = config.Operation.Status
	}
	return FacilityAppSummary{
		Kind:            "reverse-proxy",
		TitleKey:        "applicationsPage.entranceProxyFacility",
		DescriptionKey:  "applicationsPage.entranceProxyFacilityDescription",
		CategoryKey:     "applicationsPage.facilityCategoryTraffic",
		Status:          status,
		UpdatedAt:       config.UpdatedAt,
		OperationStatus: operationStatus,
		LastError:       config.LastError,
	}
}

func (h *Handler) SaveReverseProxy(w http.ResponseWriter, r *http.Request) {
	var in ReverseProxySaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.SaveReverseProxy(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ReconcileReverseProxy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ReconcileReverseProxyNow(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) BeginSaveSession(w http.ResponseWriter, r *http.Request) {
	var in BeginSaveSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.BeginSaveSession(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) UploadSaveSessionAsset(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.service.UploadSaveSessionAsset(r.Context(), r.PathValue("id"), StaticAssetUploadInput{
		AssetID:  r.FormValue("assetId"),
		Name:     r.FormValue("name"),
		Kind:     r.FormValue("kind"),
		FileName: header.Filename,
		Size:     header.Size,
		Content:  content,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) DeleteSaveSessionAsset(w http.ResponseWriter, r *http.Request) {
	var in StaticAssetDeleteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := h.service.DeleteSaveSessionAsset(r.Context(), r.PathValue("id"), in); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CommitSaveSession(w http.ResponseWriter, r *http.Request) {
	var in CommitSaveSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CommitSaveSession(r.Context(), r.PathValue("id"), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DiscardSaveSession(w http.ResponseWriter, r *http.Request) {
	h.service.DiscardSaveSession(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) StaticAssets(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ListStaticAssets(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) UploadStaticAsset(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.service.UploadStaticAsset(r.Context(), StaticAssetUploadInput{
		Name:     r.FormValue("name"),
		Kind:     r.FormValue("kind"),
		FileName: header.Filename,
		Size:     header.Size,
		Content:  content,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) DeleteStaticAsset(w http.ResponseWriter, r *http.Request) {
	assetID := strings.TrimSpace(r.PathValue("assetId"))
	if err := h.service.DeleteStaticAsset(r.Context(), assetID); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
