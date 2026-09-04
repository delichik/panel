package keyassets

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

const (
	importArchiveMaxBytes   = 64 << 20
	importArchiveFormMemory = 8 << 20
)

type service interface {
	List(ctx context.Context) ([]Asset, error)
	Get(ctx context.Context, assetID string) (Asset, error)
	CreateCA(ctx context.Context, in CreateCARequest) (Asset, error)
	CreateTLS(ctx context.Context, in CreateTLSRequest) (Asset, error)
	GenerateSSH(ctx context.Context, in GenerateSSHRequest) (Asset, error)
	Import(ctx context.Context, in ImportRequest) (Asset, error)
	ReissueTLS(ctx context.Context, assetID string) (ReissueResult, error)
	RegenerateSSH(ctx context.Context, assetID string) (RegenerateResult, error)
	ReadFile(ctx context.Context, assetID, kind string) ([]byte, string, error)
	Delete(ctx context.Context, assetID string) error
	CreateExport(ctx context.Context, in ExportRequest) (ExportResult, error)
	DownloadExport(ctx context.Context, taskID string) ([]byte, string, error)
	PreflightImport(ctx context.Context, in ImportPreflightRequest) (ImportPreflightResult, error)
	ExecuteImport(ctx context.Context, planID string, in ImportExecuteRequest) (ImportExecuteResult, error)
}

type summaryListService interface {
	ListSummaries(context.Context) ([]Asset, error)
	ListSummaryPage(context.Context, int, int, string) (httpx.ListPage[Asset], error)
}

type Handler struct {
	service service
}

type assetReferenceDTO struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	ResourceName string `json:"resourceName"`
	Relation     string `json:"relation"`
}

type assetSummaryDTO struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"`
	Name           string     `json:"name"`
	ParentAssetID  string     `json:"parentAssetId,omitempty"`
	Algorithm      string     `json:"algorithm,omitempty"`
	KeySize        int        `json:"keySize,omitempty"`
	CommonName     string     `json:"commonName,omitempty"`
	DNSNames       []string   `json:"dnsNames"`
	IPAddresses    []string   `json:"ipAddresses"`
	Fingerprint    string     `json:"fingerprint"`
	NotBefore      *time.Time `json:"notBefore,omitempty"`
	NotAfter       *time.Time `json:"notAfter,omitempty"`
	HasCertificate bool       `json:"hasCertificate"`
	HasPrivateKey  bool       `json:"hasPrivateKey"`
	HasPublicKey   bool       `json:"hasPublicKey"`
	DownloadKinds  []string   `json:"downloadKinds"`
	ChildCount     int        `json:"childCount"`
	CanReissue     bool       `json:"canReissue"`
	CanRegenerate  bool       `json:"canRegenerate"`
	CanDelete      bool       `json:"canDelete"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type assetDetailDTO struct {
	assetSummaryDTO
	Metadata map[string]any `json:"metadata,omitempty"`
}

type mutationDTO struct {
	Asset       *assetSummaryDTO `json:"asset,omitempty"`
	TaskID      string           `json:"taskId,omitempty"`
	OperationID string           `json:"operationId,omitempty"`
}

type importPlanSummaryDTO struct {
	TotalAssets        int `json:"totalAssets"`
	CACount            int `json:"caCount"`
	TLSCount           int `json:"tlsCount"`
	SSHCount           int `json:"sshCount"`
	StandaloneTLSCount int `json:"standaloneTlsCount"`
	ConflictCount      int `json:"conflictCount"`
}

type importPlanAssetDTO struct {
	AssetID       string   `json:"assetId"`
	Type          string   `json:"type"`
	Name          string   `json:"name"`
	ParentAssetID string   `json:"parentAssetId,omitempty"`
	Algorithm     string   `json:"algorithm,omitempty"`
	KeySize       int      `json:"keySize,omitempty"`
	CommonName    string   `json:"commonName,omitempty"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	Standalone    bool     `json:"standalone"`
	ConflictTypes []string `json:"conflictTypes"`
}

type importConflictDTO struct {
	AssetID            string              `json:"assetId"`
	AssetName          string              `json:"assetName"`
	AssetType          string              `json:"assetType"`
	ConflictType       string              `json:"conflictType"`
	ExistingAssetID    string              `json:"existingAssetId,omitempty"`
	ExistingAssetName  string              `json:"existingAssetName,omitempty"`
	AffectedReferences []assetReferenceDTO `json:"affectedReferences,omitempty"`
}

type importPreflightDTO struct {
	PlanID                string               `json:"planId"`
	ExpiresAt             time.Time            `json:"expiresAt"`
	Summary               importPlanSummaryDTO `json:"summary"`
	Assets                []importPlanAssetDTO `json:"assets"`
	Conflicts             []importConflictDTO  `json:"conflicts"`
	RequiresDangerConfirm bool                 `json:"requiresDangerConfirm"`
}

func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := httpx.ParseListPage(r, "q")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var result httpx.ListPage[Asset]
	if summaries, ok := h.service.(summaryListService); ok {
		result, err = summaries.ListSummaryPage(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
	} else {
		items, listErr := h.service.List(r.Context())
		err = listErr
		result = httpx.ListPage[Asset]{Items: items, Total: len(items), Page: 1, PageSize: len(items)}
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	items := make([]assetSummaryDTO, 0, len(result.Items))
	for _, asset := range result.Items {
		items = append(items, toAssetSummaryDTO(asset))
	}
	httpx.JSON(w, http.StatusOK, httpx.ListPage[assetSummaryDTO]{Items: items, Total: result.Total, Page: result.Page, PageSize: result.PageSize})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	assetID := keyAssetIDFromRequest(r)
	result, err := h.service.Get(r.Context(), assetID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if isSystemManagedAsset(result) || isACMEAsset(result) {
		httpx.Error(w, panelerr.NotFound("key asset"))
		return
	}
	httpx.JSON(w, http.StatusOK, toAssetDetailDTO(result))
}

func (h *Handler) CreateCA(w http.ResponseWriter, r *http.Request) {
	var in CreateCARequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CreateCA(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusCreated, result, "", "")
}

func (h *Handler) CreateTLS(w http.ResponseWriter, r *http.Request) {
	var in CreateTLSRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CreateTLS(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusCreated, result, "", "")
}

func (h *Handler) GenerateSSH(w http.ResponseWriter, r *http.Request) {
	var in GenerateSSHRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.GenerateSSH(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusCreated, result, "", "")
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	var in ImportRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.Import(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusCreated, result, "", "")
}

func (h *Handler) Reissue(w http.ResponseWriter, r *http.Request) {
	assetID := keyAssetIDFromRequest(r)
	result, err := h.service.ReissueTLS(r.Context(), assetID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusAccepted, result.Asset, result.TaskID, "")
}

func (h *Handler) Regenerate(w http.ResponseWriter, r *http.Request) {
	assetID := keyAssetIDFromRequest(r)
	result, err := h.service.RegenerateSSH(r.Context(), assetID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	writeMutation(w, http.StatusAccepted, result.Asset, result.TaskID, "")
}

func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	assetID, kind := keyAssetFileFromRequest(r)
	asset, err := h.service.Get(r.Context(), assetID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if isSystemManagedAsset(asset) || isACMEAsset(asset) {
		httpx.Error(w, panelerr.NotFound("key asset file"))
		return
	}
	content, filename, err := h.service.ReadFile(r.Context(), assetID, kind)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeContentDispositionFilename(filename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	assetID := keyAssetIDFromRequest(r)
	if err := h.service.Delete(r.Context(), assetID); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) CreateExport(w http.ResponseWriter, r *http.Request) {
	var in ExportRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.CreateExport(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, result)
}

func (h *Handler) DownloadExport(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskId"))
	content, filename, err := h.service.DownloadExport(r.Context(), taskID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeContentDispositionFilename(filename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) PreflightImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, importArchiveMaxBytes)
	if err := r.ParseMultipartForm(importArchiveFormMemory); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request body"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	defer file.Close()
	archiveBytes, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Failed to read archive upload"))
		return
	}
	if len(archiveBytes) == 0 {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	result, err := h.service.PreflightImport(r.Context(), ImportPreflightRequest{
		ArchiveBase64: base64.StdEncoding.EncodeToString(archiveBytes),
		Password:      strings.TrimSpace(r.FormValue("password")),
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, h.toImportPreflightDTO(r.Context(), result))
}

func (h *Handler) ExecuteImport(w http.ResponseWriter, r *http.Request) {
	planID := strings.TrimSpace(r.PathValue("planId"))
	var in ImportExecuteRequest
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.ExecuteImport(r.Context(), planID, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, result)
}

func writeMutation(w http.ResponseWriter, status int, asset Asset, taskID, operationID string) {
	dto := mutationDTO{
		Asset:       ptr(toAssetSummaryDTO(asset)),
		TaskID:      taskID,
		OperationID: operationID,
	}
	httpx.JSON(w, status, dto)
}

func toAssetSummaryDTO(asset Asset) assetSummaryDTO {
	return assetSummaryDTO{
		ID:             asset.ID,
		Type:           asset.Type,
		Name:           asset.Name,
		ParentAssetID:  asset.ParentAssetID,
		Algorithm:      asset.Algorithm,
		KeySize:        asset.KeySize,
		CommonName:     asset.CommonName,
		DNSNames:       append([]string(nil), asset.DNSNames...),
		IPAddresses:    append([]string(nil), asset.IPAddresses...),
		Fingerprint:    asset.Fingerprint,
		NotBefore:      optionalTime(asset.NotBefore),
		NotAfter:       optionalTime(asset.NotAfter),
		HasCertificate: asset.Type == TypeCACertificate || asset.Type == TypeTLSCertificate,
		HasPrivateKey:  true,
		HasPublicKey:   strings.TrimSpace(asset.PublicKey) != "",
		DownloadKinds:  append([]string(nil), asset.FileKinds...),
		ChildCount:     asset.ChildCount,
		CanReissue:     asset.CanReissue,
		CanRegenerate:  asset.CanRegenerate,
		CanDelete:      !asset.InUse && !asset.HasChildren,
		CreatedAt:      asset.CreatedAt,
		UpdatedAt:      asset.UpdatedAt,
	}
}

func toAssetDetailDTO(asset Asset) assetDetailDTO {
	return assetDetailDTO{
		assetSummaryDTO: toAssetSummaryDTO(asset),
		Metadata:        asset.Metadata,
	}
}

func referencesForAsset(asset Asset) []assetReferenceDTO {
	references := make([]assetReferenceDTO, 0, len(asset.References))
	for _, reference := range asset.References {
		references = append(references, assetReferenceDTO{
			ResourceType: reference.ResourceType,
			ResourceID:   reference.ResourceID,
			ResourceName: reference.ResourceName,
			Relation:     reference.Relation,
		})
	}
	return references
}

func (h *Handler) toImportPreflightDTO(ctx context.Context, result ImportPreflightResult) importPreflightDTO {
	assetByID := make(map[string]ImportAssetCheck, len(result.Assets))
	conflictTypesByAsset := map[string]map[string]struct{}{}
	for _, asset := range result.Assets {
		assetByID[asset.ID] = asset
		conflictTypesByAsset[asset.ID] = map[string]struct{}{}
	}
	conflicts := make([]importConflictDTO, 0, len(result.Conflicts)+len(result.OverwriteInUse))
	for _, conflict := range result.Conflicts {
		asset := assetByID[conflict.IncomingID]
		if conflict.ConflictByID {
			conflicts = append(conflicts, importConflictDTO{
				AssetID:           conflict.IncomingID,
				AssetName:         asset.Name,
				AssetType:         asset.Type,
				ConflictType:      "id_conflict",
				ExistingAssetID:   conflict.ExistingID,
				ExistingAssetName: conflict.ExistingName,
			})
			conflictTypesByAsset[conflict.IncomingID]["id_conflict"] = struct{}{}
		}
		if conflict.ConflictByName {
			conflicts = append(conflicts, importConflictDTO{
				AssetID:           conflict.IncomingID,
				AssetName:         asset.Name,
				AssetType:         asset.Type,
				ConflictType:      "name_conflict",
				ExistingAssetID:   conflict.ExistingID,
				ExistingAssetName: conflict.ExistingName,
			})
			conflictTypesByAsset[conflict.IncomingID]["name_conflict"] = struct{}{}
		}
	}
	for _, existingID := range result.OverwriteInUse {
		incomingID := existingID
		for _, conflict := range result.Conflicts {
			if conflict.ExistingID == existingID {
				incomingID = conflict.IncomingID
				break
			}
		}
		asset := assetByID[incomingID]
		if _, ok := conflictTypesByAsset[incomingID]; !ok {
			conflictTypesByAsset[incomingID] = map[string]struct{}{}
		}
		conflictTypesByAsset[incomingID]["overwrite_in_use"] = struct{}{}
		affected := []assetReferenceDTO{}
		if existing, err := h.service.Get(ctx, existingID); err == nil {
			affected = referencesForAsset(existing)
			if asset.Name == "" {
				asset.Name = existing.Name
				asset.Type = existing.Type
			}
		}
		conflicts = append(conflicts, importConflictDTO{
			AssetID:            incomingID,
			AssetName:          asset.Name,
			AssetType:          asset.Type,
			ConflictType:       "overwrite_in_use",
			ExistingAssetID:    existingID,
			ExistingAssetName:  asset.Name,
			AffectedReferences: affected,
		})
	}
	assets := make([]importPlanAssetDTO, 0, len(result.Assets))
	summary := importPlanSummaryDTO{TotalAssets: len(result.Assets), ConflictCount: len(conflicts)}
	for _, asset := range result.Assets {
		switch asset.Type {
		case TypeCACertificate:
			summary.CACount++
		case TypeTLSCertificate:
			summary.TLSCount++
		case TypeSSHKeyPair:
			summary.SSHCount++
		}
		if asset.StandaloneTLS {
			summary.StandaloneTLSCount++
		}
		conflictTypes := make([]string, 0, len(conflictTypesByAsset[asset.ID]))
		for conflictType := range conflictTypesByAsset[asset.ID] {
			conflictTypes = append(conflictTypes, conflictType)
		}
		assets = append(assets, importPlanAssetDTO{
			AssetID:       asset.ID,
			Type:          asset.Type,
			Name:          asset.Name,
			ParentAssetID: asset.ParentAssetID,
			Algorithm:     asset.Algorithm,
			KeySize:       asset.KeySize,
			CommonName:    asset.CommonName,
			Fingerprint:   asset.Fingerprint,
			Standalone:    asset.StandaloneTLS,
			ConflictTypes: conflictTypes,
		})
	}
	return importPreflightDTO{
		PlanID:                result.PlanID,
		ExpiresAt:             result.ExpiresAt,
		Summary:               summary,
		Assets:                assets,
		Conflicts:             conflicts,
		RequiresDangerConfirm: len(result.OverwriteInUse) > 0,
	}
}

func keyAssetIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func keyAssetFileFromRequest(r *http.Request) (string, string) {
	return strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("kind"))
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func ptr[T any](value T) *T {
	return &value
}

// safeContentDispositionFilename sanitizes a filename for use inside a
// Content-Disposition header value: quotes, backslashes and control characters
// (including CR/LF) are replaced, preventing header injection and broken
// download names. Falls back to a fixed name when nothing usable remains.
func safeContentDispositionFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '"' || r == '\'' || r == '\\' || r == '\r' || r == '\n' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}
