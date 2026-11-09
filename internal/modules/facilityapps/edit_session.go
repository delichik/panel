package facilityapps

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"panel/internal/modules/applications"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/filestorage"
	"panel/internal/platform/i18n"
	id "panel/internal/platform/identity"
)

const (
	facilityEditOwner          = "panel-single-administrator"
	facilityEditIdleTTL        = 24 * time.Hour
	facilityEditAbsoluteTTL    = 7 * 24 * time.Hour
	facilityEditPreviewTTL     = 5 * time.Minute
	facilityEditCommitLease    = 2 * time.Minute
	facilityEditCleanupPeriod  = 10 * time.Minute
	facilityEditOrphanStaleAge = time.Hour
)

type facilityEditRecord struct {
	FacilityEditSession
	OwnerID            string
	PreviewRevision    int
	PreviewExpiresAt   time.Time
	CommitLeaseOwner   string
	CommitLeaseExpires time.Time
	CommitKey          string
	ManifestPath       string
}

type facilityCommitManifest struct {
	SessionID      string                  `json:"sessionId"`
	RecordID       string                  `json:"recordId"`
	BaseVersion    int                     `json:"baseVersion"`
	Domain         FacilityRouteDomain     `json:"domain"`
	PreviousDomain *FacilityRouteDomain    `json:"previousDomain,omitempty"`
	Assets         []facilityManifestAsset `json:"assets"`
	BackupDir      string                  `json:"backupDir"`
	FilesMoved     bool                    `json:"filesMoved"`
	DBCommitted    bool                    `json:"dbCommitted"`
}

type facilityManifestAsset struct {
	AssetKey      string `json:"assetKey"`
	FinalID       string `json:"finalId"`
	SourceID      string `json:"sourceId,omitempty"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	ContentMode   string `json:"contentMode"`
	Filename      string `json:"filename"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	ContentSHA256 string `json:"contentSha256"`
	BlobDir       string `json:"blobDir,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func (s *Service) BeginFacilityEditSession(ctx context.Context, in BeginFacilityEditSessionInput) (FacilityEditSession, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	recordID := strings.ToLower(strings.TrimSpace(in.RecordID))
	if recordID == "" {
		return FacilityEditSession{}, panelerr.Validation("facility_record_id_required", "recordId is required")
	}
	baseVersion := 0
	var draft FacilityRouteDomain
	row, err := s.loadRouteRow(ctx, recordID)
	if err != nil {
		if !isPanelNotFound(err) {
			return FacilityEditSession{}, err
		}
		if in.Draft == nil {
			return FacilityEditSession{}, err
		}
		draft = cloneFacilityRouteDomain(*in.Draft)
	} else {
		if row.AppID != proxyApplicationID && row.AppID != "facility_reverse_proxy" {
			return FacilityEditSession{}, panelerr.Validation("facility_record_not_facility_domain", "application route records must be edited from the application editor")
		}
		baseVersion = row.Version
		if in.Draft != nil {
			draft = cloneFacilityRouteDomain(*in.Draft)
		} else {
			loaded, loadErr := s.routeRowToDomain(ctx, row)
			if loadErr != nil {
				return FacilityEditSession{}, loadErr
			}
			draft = loaded
		}
	}
	draft.Domain = recordID
	draft.Version = baseVersion
	raw, err := json.Marshal(draft)
	if err != nil {
		return FacilityEditSession{}, err
	}
	now := time.Now().UTC()
	sessionID := id.New("fedit")
	dir := s.facilityEditPath(sessionID)
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		return FacilityEditSession{}, err
	}
	created := false
	defer func() {
		if !created {
			_ = os.RemoveAll(dir)
		}
	}()
	assets, err := s.listStaticAssetsForRecord(ctx, recordID)
	if err != nil {
		return FacilityEditSession{}, err
	}
	contentHashes := make(map[string]string, len(assets))
	for _, asset := range assets {
		contentSHA, hashErr := filestorage.HashDirectory(s.staticAssetContentDir(asset.ID))
		if hashErr != nil {
			return FacilityEditSession{}, panelerr.WithDetails(panelerr.Conflict("facility_source_asset_missing", "facility source asset content is missing or unreadable"), map[string]any{"assetId": asset.ID})
		}
		contentHashes[asset.ID] = contentSHA
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FacilityEditSession{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO facility_edit_sessions(id,owner_id,record_id,client_draft_key,state,base_resource_version,draft_json,revision,idle_expires_at,absolute_expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		sessionID, facilityEditOwner, recordID, strings.TrimSpace(in.ClientDraftKey), FacilityEditSessionActive, baseVersion, string(raw), 1, formatTime(now.Add(facilityEditIdleTTL)), formatTime(now.Add(facilityEditAbsoluteTTL)), formatTime(now), formatTime(now))
	if err != nil {
		return FacilityEditSession{}, err
	}
	for _, asset := range assets {
		_, err = tx.ExecContext(ctx, `INSERT INTO facility_edit_session_assets(session_id,asset_key,source_asset_id,name,kind,content_mode,filename,size,sha256,content_sha256,blob_dir,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'ready',?,?)`,
			sessionID, asset.ID, asset.ID, asset.Name, asset.Kind, asset.ContentMode, asset.Filename, asset.Size, asset.SHA256, contentHashes[asset.ID], "", formatTime(asset.CreatedAt), formatTime(asset.UpdatedAt))
		if err != nil {
			return FacilityEditSession{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FacilityEditSession{}, err
	}
	created = true
	return s.GetFacilityEditSession(ctx, sessionID)
}

func (s *Service) GetFacilityEditSession(ctx context.Context, sessionID string) (FacilityEditSession, error) {
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditSession{}, err
	}
	if record.State == FacilityEditSessionCommitting && !record.CommitLeaseExpires.IsZero() && time.Now().UTC().After(record.CommitLeaseExpires) {
		s.editCommitMu.Lock()
		record, err = s.loadFacilityEditSession(ctx, sessionID)
		if err == nil && record.State == FacilityEditSessionCommitting && time.Now().UTC().After(record.CommitLeaseExpires) {
			s.recoverFacilityEditRecord(ctx, record)
		}
		s.editCommitMu.Unlock()
		record, err = s.loadFacilityEditSession(ctx, sessionID)
	}
	return record.FacilityEditSession, err
}

func (s *Service) PatchFacilityEditSession(ctx context.Context, sessionID string, in PatchFacilityEditSessionInput) (FacilityEditSession, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditSession{}, err
	}
	if record.State != FacilityEditSessionActive && record.State != FacilityEditSessionConflict {
		return FacilityEditSession{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	baseVersion, _ := strconv.Atoi(record.BaseResourceVersion.Value)
	if strings.TrimSpace(in.BaseResourceVersion) != "" {
		requestedBase, parseErr := strconv.Atoi(strings.TrimSpace(in.BaseResourceVersion))
		if parseErr != nil {
			return FacilityEditSession{}, panelerr.Validation("resource_version_invalid", "baseResourceVersion is invalid")
		}
		currentVersion, loadErr := s.facilityRecordVersion(ctx, record.RecordID)
		if loadErr != nil {
			return FacilityEditSession{}, loadErr
		}
		if currentVersion != requestedBase {
			return FacilityEditSession{}, panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "facility configuration changed while rebasing"), map[string]any{"expectedVersion": requestedBase, "currentVersion": currentVersion})
		}
		baseVersion = requestedBase
	}
	draft := cloneFacilityRouteDomain(in.Draft)
	draft.Domain = record.RecordID
	draft.Version = baseVersion
	raw, err := json.Marshal(draft)
	if err != nil {
		return FacilityEditSession{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET draft_json=?,base_resource_version=?,revision=revision+1,state=?,conflict_json='',preview_token='',preview_revision=0,preview_expires_at=NULL,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND absolute_expires_at>? AND idle_expires_at>?`,
		string(raw), baseVersion, FacilityEditSessionActive, formatTime(now), formatTime(now.Add(facilityEditIdleTTL)), strings.TrimSpace(sessionID), facilityEditOwner, in.Revision, FacilityEditSessionActive, FacilityEditSessionConflict, formatTime(now), formatTime(now))
	if err != nil {
		return FacilityEditSession{}, err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return FacilityEditSession{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	return s.GetFacilityEditSession(ctx, sessionID)
}

func (s *Service) PutFacilityEditAsset(ctx context.Context, sessionID, assetRef, idempotencyKey string, in FacilityEditAssetInput) (FacilityEditSession, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditSession{}, err
	}
	if record.State != FacilityEditSessionActive {
		return FacilityEditSession{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	assetRef = strings.TrimSpace(assetRef)
	if assetRef == "" || strings.TrimSpace(in.ClientOperationID) == "" {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_operation_invalid", "assetName and clientOperationId are required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSpace(in.FileName)
	}
	kind := strings.TrimSpace(in.Kind)
	contentMode := strings.TrimSpace(in.ContentMode)
	if contentMode == "" {
		contentMode = "binary"
	}
	if name == "" || (kind != StaticSourceUploadedFile && kind != StaticSourceUploadedBundle) || (contentMode != "text" && contentMode != "binary") {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_invalid", "asset name, kind, and content are required")
	}
	var assetKey string
	lookupErr := s.db.QueryRowContext(ctx, `SELECT asset_key FROM facility_edit_session_assets WHERE session_id=? AND name=? LIMIT 1`, sessionID, assetRef).Scan(&assetKey)
	if lookupErr == sql.ErrNoRows {
		lookupErr = s.db.QueryRowContext(ctx, `SELECT asset_key FROM facility_edit_session_assets WHERE session_id=? AND asset_key=? LIMIT 1`, sessionID, assetRef).Scan(&assetKey)
	}
	if lookupErr == sql.ErrNoRows {
		assetKey = assetRef
	} else if lookupErr != nil {
		return FacilityEditSession{}, lookupErr
	}
	var duplicateKey string
	duplicateErr := s.db.QueryRowContext(ctx, `SELECT asset_key FROM facility_edit_session_assets WHERE session_id=? AND name=? AND asset_key<>? LIMIT 1`, sessionID, name, assetKey).Scan(&duplicateKey)
	if duplicateErr != nil && duplicateErr != sql.ErrNoRows {
		return FacilityEditSession{}, duplicateErr
	}
	if duplicateErr == nil {
		return FacilityEditSession{}, panelerr.Validation("facility_static_asset_name_duplicate", "Static asset name must be unique within the facility")
	}
	if kind == StaticSourceUploadedBundle && contentMode != "binary" {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_mode_invalid", "bundle assets must be binary")
	}
	if contentMode == "text" && (len(in.Content) > 1<<20 || !utf8.Valid(in.Content)) {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_text_invalid", "text assets must be valid UTF-8 and no larger than 1 MiB")
	}
	if contentMode == "binary" && len(in.Content) == 0 {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_invalid", "binary asset content is required")
	}
	filename := safeAssetFilename(in.FileName)
	if filename == "" {
		filename = "asset"
	}
	var existingKind, existingMode string
	existingErr := s.db.QueryRowContext(ctx, `SELECT kind,content_mode FROM facility_edit_session_assets WHERE session_id=? AND asset_key=?`, sessionID, assetKey).Scan(&existingKind, &existingMode)
	if existingErr != nil && existingErr != sql.ErrNoRows {
		return FacilityEditSession{}, existingErr
	}
	if existingErr == nil && existingMode != contentMode {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_mode_immutable", "asset content mode cannot be changed; delete and recreate the asset")
	}
	if existingErr == nil && existingKind != kind {
		return FacilityEditSession{}, panelerr.Validation("facility_edit_asset_kind_immutable", "asset kind cannot be changed; delete and recreate the asset")
	}
	requestHash := facilityEditHash(in.Revision, assetKey, name, kind, contentMode, filename, in.Content)
	if session, ok, err := s.facilityEditOperationResult(ctx, sessionID, in.ClientOperationID, idempotencyKey, requestHash); ok || err != nil {
		return session, err
	}
	dir := filepath.Join(s.facilityEditPath(sessionID), "assets", id.New("blob"))
	contentDir := filepath.Join(dir, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return FacilityEditSession{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	if kind == StaticSourceUploadedBundle {
		if err := extractStaticBundle(strings.NewReader(string(in.Content)), int64(len(in.Content)), filename, contentDir); err != nil {
			return FacilityEditSession{}, err
		}
	} else if err := os.WriteFile(filepath.Join(contentDir, filename), in.Content, 0o644); err != nil {
		return FacilityEditSession{}, err
	}
	contentSHA, err := filestorage.HashDirectory(contentDir)
	if err != nil {
		return FacilityEditSession{}, err
	}
	sum := sha256.Sum256(in.Content)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FacilityEditSession{}, err
	}
	defer tx.Rollback()
	var sourceID, previousBlob, created string
	_ = tx.QueryRowContext(ctx, `SELECT source_asset_id,blob_dir,created_at FROM facility_edit_session_assets WHERE session_id=? AND asset_key=?`, sessionID, assetKey).Scan(&sourceID, &previousBlob, &created)
	if created == "" {
		created = formatTime(now)
	}
	if s.beforeFacilityEditRevisionBump != nil {
		s.beforeFacilityEditRevisionBump()
	}
	if err := bumpFacilityEditRevision(ctx, tx, sessionID, in.Revision); err != nil {
		return FacilityEditSession{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO facility_edit_session_assets(session_id,asset_key,source_asset_id,name,kind,content_mode,filename,size,sha256,content_sha256,blob_dir,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'ready',?,?) ON CONFLICT(session_id,asset_key) DO UPDATE SET name=excluded.name,kind=excluded.kind,content_mode=excluded.content_mode,filename=excluded.filename,size=excluded.size,sha256=excluded.sha256,content_sha256=excluded.content_sha256,blob_dir=excluded.blob_dir,state='ready',updated_at=excluded.updated_at`,
		sessionID, assetKey, sourceID, name, kind, contentMode, filename, len(in.Content), hex.EncodeToString(sum[:]), contentSHA, dir, created, formatTime(now))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return FacilityEditSession{}, panelerr.Validation("facility_static_asset_name_duplicate", "Static asset name must be unique within the facility")
		}
		return FacilityEditSession{}, err
	}
	if err := insertFacilityEditOperation(ctx, tx, sessionID, in.ClientOperationID, facilityIdempotencyKey(idempotencyKey), requestHash); err != nil {
		return FacilityEditSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return FacilityEditSession{}, err
	}
	cleanup = false
	if previousBlob != "" && previousBlob != dir {
		_ = os.RemoveAll(previousBlob)
	}
	result, err := s.GetFacilityEditSession(ctx, sessionID)
	if err == nil {
		_ = s.storeFacilityEditOperationResult(ctx, sessionID, in.ClientOperationID, result)
	}
	return result, err
}

func (s *Service) DeleteFacilityEditAsset(ctx context.Context, sessionID, assetRef, idempotencyKey string, in FacilityEditMutationInput) (FacilityEditSession, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	if strings.TrimSpace(in.ClientOperationID) == "" {
		return FacilityEditSession{}, panelerr.Validation("client_operation_id_required", "clientOperationId is required")
	}
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditSession{}, err
	}
	if record.State != FacilityEditSessionActive {
		return FacilityEditSession{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	assetRef = strings.TrimSpace(assetRef)
	var assetKey string
	lookupErr := s.db.QueryRowContext(ctx, `SELECT asset_key FROM facility_edit_session_assets WHERE session_id=? AND name=? LIMIT 1`, sessionID, assetRef).Scan(&assetKey)
	if lookupErr == sql.ErrNoRows {
		assetKey = assetRef
	} else if lookupErr != nil {
		return FacilityEditSession{}, lookupErr
	}
	for _, route := range record.Draft.Paths {
		assetName := strings.TrimSpace(route.AssetName)
		if assetName == "" {
			assetName = strings.TrimSpace(route.AssetID)
		}
		if assetName == "" {
			continue
		}
		for _, asset := range record.Assets {
			if asset.AssetKey == strings.TrimSpace(assetKey) && (asset.Name == assetName || asset.AssetKey == assetName || asset.SourceAssetID == assetName) {
				return FacilityEditSession{}, panelerr.WithDetails(panelerr.Conflict("facility_static_asset_in_use", "Static asset is still referenced by a draft route"), map[string]any{"assetKey": assetKey, "assetName": assetName, "domain": record.Draft.Domain, "path": route.Path})
			}
		}
	}
	requestHash := facilityEditHash(in.Revision, assetKey, "delete")
	if session, ok, err := s.facilityEditOperationResult(ctx, sessionID, in.ClientOperationID, idempotencyKey, requestHash); ok || err != nil {
		return session, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FacilityEditSession{}, err
	}
	defer tx.Rollback()
	var blob string
	if err := tx.QueryRowContext(ctx, `SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key=?`, sessionID, assetKey).Scan(&blob); err != nil {
		if err == sql.ErrNoRows {
			var currentRevision int
			var currentState string
			loadErr := tx.QueryRowContext(ctx, `SELECT revision,state FROM facility_edit_sessions WHERE id=? AND owner_id=?`, sessionID, facilityEditOwner).Scan(&currentRevision, &currentState)
			if loadErr != nil || currentRevision != in.Revision || currentState != FacilityEditSessionActive {
				return FacilityEditSession{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
			}
			if err := insertFacilityEditOperation(ctx, tx, sessionID, in.ClientOperationID, facilityIdempotencyKey(idempotencyKey), requestHash); err != nil {
				return FacilityEditSession{}, err
			}
			if err := tx.Commit(); err != nil {
				return FacilityEditSession{}, err
			}
			result, resultErr := s.GetFacilityEditSession(ctx, sessionID)
			if resultErr == nil {
				_ = s.storeFacilityEditOperationResult(ctx, sessionID, in.ClientOperationID, result)
			}
			return result, resultErr
		}
		return FacilityEditSession{}, err
	}
	if s.beforeFacilityEditRevisionBump != nil {
		s.beforeFacilityEditRevisionBump()
	}
	if err := bumpFacilityEditRevision(ctx, tx, sessionID, in.Revision); err != nil {
		return FacilityEditSession{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM facility_edit_session_assets WHERE session_id=? AND asset_key=?`, sessionID, assetKey); err != nil {
		return FacilityEditSession{}, err
	}
	if err := insertFacilityEditOperation(ctx, tx, sessionID, in.ClientOperationID, facilityIdempotencyKey(idempotencyKey), requestHash); err != nil {
		return FacilityEditSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return FacilityEditSession{}, err
	}
	if blob != "" {
		_ = os.RemoveAll(blob)
	}
	result, err := s.GetFacilityEditSession(ctx, sessionID)
	if err == nil {
		_ = s.storeFacilityEditOperationResult(ctx, sessionID, in.ClientOperationID, result)
	}
	return result, err
}

func (s *Service) ValidateFacilityEditSession(ctx context.Context, sessionID string, revision int) (FacilityEditValidationResult, error) {
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditValidationResult{}, err
	}
	if record.Revision != revision || record.State != FacilityEditSessionActive {
		return FacilityEditValidationResult{}, s.facilityEditConflict(ctx, sessionID, revision)
	}
	diagnostics := s.validateFacilityEditDraft(ctx, record)
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state=? AND idle_expires_at>? AND absolute_expires_at>?`,
		formatTime(now), formatTime(now.Add(facilityEditIdleTTL)), sessionID, facilityEditOwner, revision, FacilityEditSessionActive, formatTime(now), formatTime(now))
	if err != nil {
		return FacilityEditValidationResult{}, err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return FacilityEditValidationResult{}, s.facilityEditConflict(ctx, sessionID, revision)
	}
	return FacilityEditValidationResult{Valid: !facilityDiagnosticsBlock(diagnostics), Revision: revision, Diagnostics: diagnostics}, nil
}

func (s *Service) PreviewFacilityEditSession(ctx context.Context, sessionID string, revision int) (FacilityEditPreviewResult, error) {
	validation, err := s.ValidateFacilityEditSession(ctx, sessionID, revision)
	if err != nil {
		return FacilityEditPreviewResult{}, err
	}
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditPreviewResult{}, err
	}
	diagnostics := append([]applications.Diagnostic(nil), validation.Diagnostics...)
	diagnostics = append(diagnostics, applications.Diagnostic{Code: "facility_cross_module_insights_stale", Severity: "warning", Message: i18n.Translate("facility_cross_module_insights_stale", "DNS, certificate, firewall, and gateway port insights are temporarily unavailable")})
	var appRoutes []applications.ApplicationReverseProxyConfig
	if s.apps != nil {
		if routes, routesErr := s.apps.ApplicationReverseProxyConfigs(ctx); routesErr == nil {
			appRoutes = routes
		}
	}
	if normalized, normalizeErr := s.normalizeEditDomain(ctx, record); normalizeErr == nil {
		plane, _ := s.loadConfig(ctx)
		snapshot := ReverseProxyConfig{ID: ReverseProxyID, DeploymentServers: append([]string(nil), plane.DeploymentServers...), Domains: []FacilityRouteDomain{normalized}}
		if _, summaryErr := s.routeSummaries(ctx, snapshot, appRoutes); summaryErr != nil {
			diagnostics = append(diagnostics, applications.Diagnostic{Code: "facility_route_summary_stale", Severity: "warning", Message: i18n.Translate("facility_route_summary_stale", "Route summary could not be refreshed"), Details: map[string]any{"error": summaryErr.Error()}})
		}
	}
	now := time.Now().UTC()
	token := id.New("fpreview")
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET preview_token=?,preview_revision=?,preview_expires_at=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state=? AND idle_expires_at>? AND absolute_expires_at>?`,
		token, revision, formatTime(now.Add(facilityEditPreviewTTL)), formatTime(now), formatTime(now.Add(facilityEditIdleTTL)), sessionID, facilityEditOwner, revision, FacilityEditSessionActive, formatTime(now), formatTime(now))
	if err != nil {
		return FacilityEditPreviewResult{}, err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return FacilityEditPreviewResult{}, s.facilityEditConflict(ctx, sessionID, revision)
	}
	return FacilityEditPreviewResult{Revision: revision, Diagnostics: diagnostics, Token: applications.PreviewToken{Value: token, Action: "facility.reverse_proxy.commit", SubjectVersion: record.BaseResourceVersion.Value}, ExpiresAt: now.Add(facilityEditPreviewTTL)}, nil
}

func (s *Service) CommitFacilityEditSession(ctx context.Context, sessionID, idempotencyKey string, in CommitFacilityEditSessionInput) (FacilityEditCommitResult, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return FacilityEditCommitResult{}, err
	}
	now := time.Now().UTC()
	if record.State == FacilityEditSessionCommitting && (record.CommitLeaseExpires.IsZero() || !now.Before(record.CommitLeaseExpires)) {
		s.recoverFacilityEditRecord(ctx, record)
		record, err = s.loadFacilityEditSession(ctx, sessionID)
		if err != nil {
			return FacilityEditCommitResult{}, err
		}
	}
	if record.State == FacilityEditSessionCommitted && record.CommitResult != nil {
		if record.CommitKey != facilityIdempotencyKey(idempotencyKey) {
			return FacilityEditCommitResult{}, panelerr.Conflict("idempotency_key_reused", "commit idempotency key does not match")
		}
		return *record.CommitResult, nil
	}
	if record.Revision != in.Revision || record.State != FacilityEditSessionActive {
		return FacilityEditCommitResult{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	if record.PreviewToken == nil || record.PreviewToken.Value != strings.TrimSpace(in.PreviewToken) || record.PreviewRevision != in.Revision || time.Now().UTC().After(record.PreviewExpiresAt) {
		return FacilityEditCommitResult{}, panelerr.Conflict("preview_stale", "facility edit preview is missing or stale")
	}
	if strings.TrimSpace(in.BaseResourceVersion) != record.BaseResourceVersion.Value {
		return FacilityEditCommitResult{}, panelerr.Conflict("resource_version_conflict", "facility base version changed")
	}
	diagnostics := s.validateFacilityEditDraft(ctx, record)
	if facilityDiagnosticsBlock(diagnostics) {
		return FacilityEditCommitResult{}, panelerr.WithDetails(panelerr.Validation("facility_reverse_proxy_invalid", "facility edit session is invalid"), map[string]any{"diagnostics": diagnostics})
	}
	manifest, err := s.prepareFacilityCommitManifest(record)
	if err != nil {
		return FacilityEditCommitResult{}, err
	}
	if err := s.verifyFacilityManifestAssets(manifest); err != nil {
		return FacilityEditCommitResult{}, err
	}
	manifestPath := filepath.Join(s.facilityEditPath(sessionID), "commit-manifest.json")
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		return FacilityEditCommitResult{}, err
	}
	now = time.Now().UTC()
	leaseOwner := id.New("fcommit")
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET state=?,commit_lease_owner=?,commit_lease_expires_at=?,commit_idempotency_key=?,manifest_path=?,updated_at=? WHERE id=? AND revision=? AND state=?`,
		FacilityEditSessionCommitting, leaseOwner, formatTime(now.Add(facilityEditCommitLease)), facilityIdempotencyKey(idempotencyKey), manifestPath, formatTime(now), sessionID, in.Revision, FacilityEditSessionActive)
	if err != nil {
		return FacilityEditCommitResult{}, err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return FacilityEditCommitResult{}, s.facilityEditConflict(ctx, sessionID, in.Revision)
	}
	if err := s.renewFacilityCommitLease(ctx, sessionID, leaseOwner); err != nil {
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, FacilityEditSessionActive, "")
		return FacilityEditCommitResult{}, err
	}
	if err := s.moveFacilityManifestFiles(&manifest); err != nil {
		s.rollbackFacilityManifestFiles(manifest)
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, FacilityEditSessionActive, "")
		return FacilityEditCommitResult{}, err
	}
	manifest.FilesMoved = true
	if err := s.renewFacilityCommitLease(ctx, sessionID, leaseOwner); err != nil {
		s.rollbackFacilityManifestFiles(manifest)
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, FacilityEditSessionActive, "")
		return FacilityEditCommitResult{}, err
	}
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		s.rollbackFacilityManifestFiles(manifest)
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, FacilityEditSessionActive, "")
		return FacilityEditCommitResult{}, err
	}
	previous, _ := s.facilityRecordDomain(ctx, record.RecordID)
	if err := s.renewFacilityCommitLease(ctx, sessionID, leaseOwner); err != nil {
		s.rollbackFacilityManifestFiles(manifest)
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, FacilityEditSessionActive, "")
		return FacilityEditCommitResult{}, err
	}
	if err := s.commitFacilityManifestDB(ctx, manifest); err != nil {
		s.rollbackFacilityManifestFiles(manifest)
		state := FacilityEditSessionActive
		conflict := ""
		if facilityPanelErrorCode(err) == "resource_version_conflict" {
			state, conflict = FacilityEditSessionConflict, `{"code":"resource_version_conflict"}`
		}
		s.resetFacilityCommit(ctx, sessionID, leaseOwner, state, conflict)
		return FacilityEditCommitResult{}, err
	}
	manifest.DBCommitted = true
	_ = writeFacilityManifest(manifestPath, manifest)
	committed, err := s.GetConfigRecord(ctx, record.RecordID)
	if err != nil {
		return FacilityEditCommitResult{}, err
	}
	plane, _ := s.loadConfig(ctx)
	applyIDs := facilityRecordApplyServers(previous, committed.Domain, plane.DeploymentServers)
	result := FacilityEditCommitResult{
		Record:          committed,
		ResourceVersion: recordResourceVersion(committed),
		ApplyRequested:  false,
		ApplyNeeded:     true,
		ApplyServerIDs:  applyIDs,
	}
	if config, cfgErr := s.GetReverseProxy(ctx); cfgErr == nil {
		result.Config = config
	}
	if facilityDomainDNSChanged(previous, committed.Domain) {
		if _, dnsErr := s.triggerDNSSync(ctx, []string{committed.RecordID}, "facility_app", false); dnsErr != nil {
			result.Diagnostics = append(result.Diagnostics, applications.Diagnostic{Code: "facility_dns_sync_failed", Severity: "warning", Message: i18n.Translate("facility_dns_sync_failed", "Configuration was committed, but requesting the DNS record sync failed"), Details: map[string]any{"error": dnsErr.Error()}})
		}
	}
	if err := s.finishFacilityCommit(ctx, record, leaseOwner, result); err != nil {
		return FacilityEditCommitResult{}, err
	}
	_ = os.RemoveAll(manifest.BackupDir)
	return result, nil
}

func (s *Service) DiscardFacilityEditSession(ctx context.Context, sessionID string) error {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	record, err := s.loadFacilityEditSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if record.State != FacilityEditSessionActive && record.State != FacilityEditSessionConflict {
		return panelerr.Conflict("facility_edit_session_not_discardable", "facility edit session cannot be discarded")
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND state IN (?,?)`, FacilityEditSessionDiscarded, formatTime(now), formatTime(now), sessionID, facilityEditOwner, FacilityEditSessionActive, FacilityEditSessionConflict)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return panelerr.Conflict("facility_edit_session_not_discardable", "facility edit session cannot be discarded")
	}
	_ = os.RemoveAll(s.facilityEditPath(sessionID))
	return nil
}

func (s *Service) validateFacilityEditDraft(ctx context.Context, record facilityEditRecord) []applications.Diagnostic {
	draft := record.Draft
	draft.Domain = record.RecordID
	plane, err := s.loadConfig(ctx)
	if err != nil {
		return facilityDiagnosticForError(err)
	}
	if diagnostics := facilityDomainTopologyDiagnostics(draft, plane.DeploymentServers); len(diagnostics) > 0 {
		return diagnostics
	}
	normalized, err := normalizeFacilityDomain(draft, plane.DeploymentServers)
	if err != nil {
		return facilityDiagnosticForError(err)
	}
	assets := map[string]FacilityEditAsset{}
	for _, asset := range record.Assets {
		assets[asset.AssetKey] = asset
		assets[asset.Name] = asset
	}
	assetDiagnostics := []applications.Diagnostic{}
	for _, route := range normalized.Paths {
		// 只有静态规则才引用静态资产；redirect/proxy_pass 路由（即使
		// sourceType 被前端默认值带成 uploaded_file）不得参与资产校验。
		if route.RuleType != StaticRuleStatic || (route.SourceType != StaticSourceUploadedFile && route.SourceType != StaticSourceUploadedBundle) {
			continue
		}
		asset, ok := assets[route.AssetName]
		if !ok && route.AssetID != "" {
			asset, ok = assets[route.AssetID]
		}
		if !ok {
			assetDiagnostics = append(assetDiagnostics, applications.Diagnostic{Code: "facility_static_asset_referenced_after_delete", Severity: "error", Field: "domains", Message: i18n.Translate("facility_static_asset_referenced_after_delete", "A route still references a deleted asset"), Details: map[string]any{"domain": normalized.Domain, "path": route.Path, "assetName": route.AssetName}})
			continue
		}
		if asset.Kind != route.SourceType {
			assetDiagnostics = append(assetDiagnostics, applications.Diagnostic{Code: "facility_static_site_asset_kind_invalid", Severity: "error", Field: "domains", Message: i18n.Translate("facility_static_site_asset_kind_invalid", "Static asset kind does not match route source"), Details: map[string]any{"domain": normalized.Domain, "path": route.Path, "assetName": route.AssetName}})
		}
	}
	if len(assetDiagnostics) > 0 {
		return assetDiagnostics
	}
	snapshot := ReverseProxyConfig{ID: ReverseProxyID, DeploymentServers: append([]string(nil), plane.DeploymentServers...), Domains: []FacilityRouteDomain{normalized}}
	if err := s.validateRouteConflicts(ctx, snapshot); err != nil {
		return facilityDiagnosticForError(err)
	}
	return []applications.Diagnostic{}
}

func facilityDomainTopologyDiagnostics(domain FacilityRouteDomain, gateways []string) []applications.Diagnostic {
	gatewaySet := map[string]struct{}{}
	for _, serverID := range gateways {
		serverID = strings.TrimSpace(serverID)
		if serverID != "" {
			gatewaySet[serverID] = struct{}{}
		}
	}
	diagnostics := []applications.Diagnostic{}
	for _, serverID := range domain.OriginServerIDs {
		if _, ok := gatewaySet[strings.TrimSpace(serverID)]; !ok {
			diagnostics = append(diagnostics, applications.Diagnostic{Code: "facility_gateway_removal_invalidates_origin", Severity: "error", Field: "domains", Message: i18n.Translate("facility_gateway_removal_invalidates_origin", "Removing a gateway would remove a configured domain origin"), Details: map[string]any{"domain": domain.Domain, "serverId": serverID}})
		}
	}
	primary := strings.TrimSpace(domain.AnyAccess.PrimaryOriginServerID)
	if domain.AnyAccess.Enabled && primary != "" {
		if _, ok := gatewaySet[primary]; !ok {
			diagnostics = append(diagnostics, applications.Diagnostic{Code: "facility_gateway_removal_invalidates_anyaccess_primary", Severity: "error", Field: "domains", Message: i18n.Translate("facility_gateway_removal_invalidates_anyaccess_primary", "Removing a gateway would remove the AnyAccess primary origin"), Details: map[string]any{"domain": domain.Domain, "serverId": primary}})
		}
	}
	return diagnostics
}

func facilityDiagnosticForError(err error) []applications.Diagnostic {
	var target *panelerr.Error
	if errors.As(err, &target) {
		return []applications.Diagnostic{{Code: target.Code, Severity: "error", Message: i18n.Translate(target.Code, target.Message), Details: target.Details}}
	}
	return []applications.Diagnostic{{Code: "facility_validation_failed", Severity: "error", Message: err.Error()}}
}

func facilityDiagnosticsBlock(items []applications.Diagnostic) bool {
	for _, item := range items {
		if item.Severity == "error" {
			return true
		}
	}
	return false
}

func (s *Service) loadFacilityEditSession(ctx context.Context, sessionID string) (facilityEditRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,owner_id,record_id,client_draft_key,state,base_resource_version,draft_json,revision,preview_token,preview_revision,COALESCE(preview_expires_at,''),commit_lease_owner,COALESCE(commit_lease_expires_at,''),commit_idempotency_key,commit_result_json,manifest_path,idle_expires_at,absolute_expires_at,created_at,updated_at,COALESCE(committed_at,'') FROM facility_edit_sessions WHERE id=? AND owner_id=?`, strings.TrimSpace(sessionID), facilityEditOwner)
	var record facilityEditRecord
	var draftRaw, preview, previewExpires, leaseExpires, resultRaw, idle, absolute, created, updated, committed string
	var baseVersion int
	if err := row.Scan(&record.ID, &record.OwnerID, &record.RecordID, &record.ClientDraftKey, &record.State, &baseVersion, &draftRaw, &record.Revision, &preview, &record.PreviewRevision, &previewExpires, &record.CommitLeaseOwner, &leaseExpires, &record.CommitKey, &resultRaw, &record.ManifestPath, &idle, &absolute, &created, &updated, &committed); err != nil {
		if err == sql.ErrNoRows {
			return facilityEditRecord{}, panelerr.NotFound("facility_edit_session")
		}
		return facilityEditRecord{}, err
	}
	_ = json.Unmarshal([]byte(draftRaw), &record.Draft)
	record.BaseResourceVersion = applications.ResourceVersion{Value: strconv.Itoa(baseVersion)}
	record.IdleExpiresAt, record.AbsoluteExpiresAt = parseTime(idle), parseTime(absolute)
	record.CreatedAt, record.UpdatedAt = parseTime(created), parseTime(updated)
	record.PreviewExpiresAt, record.CommitLeaseExpires = parseTime(previewExpires), parseTime(leaseExpires)
	if preview != "" {
		record.PreviewToken = &applications.PreviewToken{Value: preview, Action: "facility.reverse_proxy.commit", SubjectVersion: strconv.Itoa(baseVersion)}
	}
	if committed != "" {
		value := parseTime(committed)
		record.CommittedAt = &value
	}
	if resultRaw != "" {
		var result FacilityEditCommitResult
		if json.Unmarshal([]byte(resultRaw), &result) == nil {
			record.CommitResult = &result
		}
	}
	now := time.Now().UTC()
	terminal := record.State == FacilityEditSessionCommitted || record.State == FacilityEditSessionDiscarded || record.State == FacilityEditSessionExpired
	activeCommitLease := record.State == FacilityEditSessionCommitting && !record.CommitLeaseExpires.IsZero() && now.Before(record.CommitLeaseExpires)
	if !terminal && !activeCommitLease && record.State != FacilityEditSessionCommitting && (now.After(record.IdleExpiresAt) || now.After(record.AbsoluteExpiresAt)) {
		_, _ = s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET state=?,updated_at=? WHERE id=? AND owner_id=? AND state IN (?,?)`, FacilityEditSessionExpired, formatTime(now), record.ID, facilityEditOwner, FacilityEditSessionActive, FacilityEditSessionConflict)
		record.State = FacilityEditSessionExpired
		_ = os.RemoveAll(s.facilityEditPath(record.ID))
	}
	assets, err := s.loadFacilityEditAssets(ctx, record.ID)
	if err != nil {
		return facilityEditRecord{}, err
	}
	record.Assets = assets
	if record.Draft.Domain == "" {
		record.Draft.Domain = record.RecordID
	}
	for pathIndex := range record.Draft.Paths {
		path := &record.Draft.Paths[pathIndex]
		if strings.TrimSpace(path.AssetName) == "" && strings.TrimSpace(path.AssetID) != "" {
			for _, asset := range assets {
				if asset.AssetKey == path.AssetID || asset.SourceAssetID == path.AssetID {
					path.AssetName = asset.Name
					break
				}
			}
		}
		path.AssetID = ""
	}
	return record, nil
}

func (s *Service) loadFacilityEditAssets(ctx context.Context, sessionID string) ([]FacilityEditAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT asset_key,source_asset_id,name,kind,content_mode,filename,size,sha256,created_at,updated_at FROM facility_edit_session_assets WHERE session_id=? AND state='ready' ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []FacilityEditAsset{}
	for rows.Next() {
		var asset FacilityEditAsset
		var created, updated string
		if err := rows.Scan(&asset.AssetKey, &asset.SourceAssetID, &asset.Name, &asset.Kind, &asset.ContentMode, &asset.Filename, &asset.Size, &asset.SHA256, &created, &updated); err != nil {
			return nil, err
		}
		asset.CreatedAt, asset.UpdatedAt = parseTime(created), parseTime(updated)
		result = append(result, asset)
	}
	return result, rows.Err()
}

func bumpFacilityEditRevision(ctx context.Context, tx *sql.Tx, sessionID string, revision int) error {
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx, `UPDATE facility_edit_sessions SET revision=revision+1,state=?,preview_token='',preview_revision=0,preview_expires_at=NULL,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND idle_expires_at>? AND absolute_expires_at>?`,
		FacilityEditSessionActive, formatTime(now), formatTime(now.Add(facilityEditIdleTTL)), sessionID, facilityEditOwner, revision, FacilityEditSessionActive, FacilityEditSessionConflict, formatTime(now), formatTime(now))
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return panelerr.Conflict("edit_session_revision_conflict", "facility edit session changed in another client")
	}
	return nil
}

func (s *Service) facilityEditConflict(ctx context.Context, sessionID string, expected int) error {
	var current int
	var state string
	if err := s.db.QueryRowContext(ctx, `SELECT revision,state FROM facility_edit_sessions WHERE id=? AND owner_id=?`, sessionID, facilityEditOwner).Scan(&current, &state); err != nil {
		if err == sql.ErrNoRows {
			return panelerr.NotFound("facility_edit_session")
		}
		return err
	}
	return panelerr.WithDetails(panelerr.Conflict("edit_session_revision_conflict", "facility edit session changed in another client"), map[string]any{"expectedRevision": expected, "currentRevision": current, "state": state})
}

func (s *Service) prepareFacilityCommitManifest(record facilityEditRecord) (facilityCommitManifest, error) {
	assets := make([]facilityManifestAsset, 0, len(record.Assets))
	rows, err := s.db.Query(`SELECT asset_key,source_asset_id,name,kind,content_mode,filename,size,sha256,content_sha256,blob_dir,created_at,updated_at FROM facility_edit_session_assets WHERE session_id=? AND state='ready'`, record.ID)
	if err != nil {
		return facilityCommitManifest{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item facilityManifestAsset
		if err := rows.Scan(&item.AssetKey, &item.SourceID, &item.Name, &item.Kind, &item.ContentMode, &item.Filename, &item.Size, &item.SHA256, &item.ContentSHA256, &item.BlobDir, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return facilityCommitManifest{}, err
		}
		item.FinalID = item.SourceID
		if item.FinalID == "" {
			item.FinalID = id.New("facility_static")
		}
		assets = append(assets, item)
	}
	draft := cloneFacilityRouteDomain(record.Draft)
	draft.Domain = record.RecordID
	baseVersion, _ := strconv.Atoi(record.BaseResourceVersion.Value)
	var previous *FacilityRouteDomain
	if loaded, err := s.facilityRecordDomain(context.Background(), record.RecordID); err == nil {
		previous = loaded
	}
	return facilityCommitManifest{SessionID: record.ID, RecordID: record.RecordID, BaseVersion: baseVersion, Domain: draft, PreviousDomain: previous, Assets: assets, BackupDir: filepath.Join(s.facilityEditPath(record.ID), "backup")}, nil
}

func (s *Service) verifyFacilityManifestAssets(manifest facilityCommitManifest) error {
	for _, asset := range manifest.Assets {
		contentDir := ""
		if asset.BlobDir != "" {
			contentDir = filepath.Join(asset.BlobDir, "content")
		} else if asset.SourceID != "" {
			contentDir = s.staticAssetContentDir(asset.SourceID)
		}
		if contentDir == "" {
			return panelerr.WithDetails(panelerr.Conflict("facility_asset_content_missing", "facility asset content is missing"), map[string]any{"assetKey": asset.AssetKey})
		}
		actual, err := filestorage.HashDirectory(contentDir)
		if err != nil {
			return panelerr.WithDetails(panelerr.Conflict("facility_asset_content_missing", "facility asset content is missing or unreadable"), map[string]any{"assetKey": asset.AssetKey})
		}
		if actual != asset.ContentSHA256 {
			return panelerr.WithDetails(panelerr.Conflict("facility_asset_hash_mismatch", "facility asset content changed while editing"), map[string]any{"assetKey": asset.AssetKey, "expectedSha256": asset.ContentSHA256, "actualSha256": actual})
		}
	}
	return nil
}

func (s *Service) moveFacilityManifestFiles(manifest *facilityCommitManifest) error {
	if err := os.MkdirAll(manifest.BackupDir, 0o700); err != nil {
		return err
	}
	finalIDs := map[string]facilityManifestAsset{}
	for _, asset := range manifest.Assets {
		finalIDs[asset.FinalID] = asset
	}
	existing, err := s.listStaticAssetsForRecord(context.Background(), manifest.RecordID)
	if err != nil {
		return err
	}
	for _, asset := range existing {
		desired, retained := finalIDs[asset.ID]
		if retained && desired.BlobDir == "" {
			continue
		}
		from, to := s.staticAssetDir(asset.ID), filepath.Join(manifest.BackupDir, asset.ID)
		if _, statErr := os.Stat(from); statErr == nil {
			if err := os.Rename(from, to); err != nil {
				return err
			}
		}
	}
	for _, asset := range manifest.Assets {
		if asset.BlobDir == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.staticAssetDir(asset.FinalID)), 0o700); err != nil {
			return err
		}
		if err := os.Rename(asset.BlobDir, s.staticAssetDir(asset.FinalID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) rollbackFacilityManifestFiles(manifest facilityCommitManifest) {
	for _, asset := range manifest.Assets {
		if asset.BlobDir == "" {
			continue
		}
		final := s.staticAssetDir(asset.FinalID)
		if _, err := os.Stat(final); err == nil {
			_ = os.MkdirAll(filepath.Dir(asset.BlobDir), 0o700)
			_ = os.Rename(final, asset.BlobDir)
		}
	}
	entries, _ := os.ReadDir(manifest.BackupDir)
	for _, entry := range entries {
		backup := filepath.Join(manifest.BackupDir, entry.Name())
		final := s.staticAssetDir(entry.Name())
		if _, err := os.Stat(final); errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(backup, final)
		}
	}
}

func (s *Service) commitFacilityManifestDB(ctx context.Context, manifest facilityCommitManifest) error {
	plane, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	domain := manifest.Domain
	domain.Domain = manifest.RecordID
	normalized, err := normalizeFacilityDomain(domain, plane.DeploymentServers)
	if err != nil {
		return err
	}
	originsRaw, err := json.Marshal(normalized.OriginServerIDs)
	if err != nil {
		return err
	}
	anyRaw, err := json.Marshal(normalized.AnyAccess)
	if err != nil {
		return err
	}
	pathsRaw, err := json.Marshal(normalized.Paths)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM facility_static_assets WHERE record_id=?`, manifest.RecordID); err != nil {
		return err
	}
	for _, asset := range manifest.Assets {
		if _, err := tx.ExecContext(ctx, `INSERT INTO facility_static_assets(id,record_id,name,kind,content_mode,filename,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, asset.FinalID, manifest.RecordID, asset.Name, asset.Kind, asset.ContentMode, asset.Filename, asset.Size, asset.SHA256, asset.CreatedAt, asset.UpdatedAt); err != nil {
			return err
		}
	}
	now := formatTime(time.Now().UTC())
	var currentVersion int
	rowErr := tx.QueryRowContext(ctx, `SELECT version FROM reverse_proxy_routes WHERE domain=?`, manifest.RecordID).Scan(&currentVersion)
	if rowErr != nil && rowErr != sql.ErrNoRows {
		return rowErr
	}
	var result sql.Result
	if rowErr == sql.ErrNoRows {
		if manifest.BaseVersion != 0 {
			return panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "facility configuration changed while editing"), map[string]any{"expectedVersion": manifest.BaseVersion, "currentVersion": 0})
		}
		result, err = tx.ExecContext(ctx, `INSERT INTO reverse_proxy_routes(domain,app_id,version,origin_server_ids,any_access_json,target_port,paths_json,created_at,updated_at) VALUES(?,?,1,?,?,0,?,?,?)`, manifest.RecordID, proxyApplicationID, string(originsRaw), string(anyRaw), string(pathsRaw), now, now)
	} else if currentVersion != manifest.BaseVersion {
		return panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "facility configuration changed while editing"), map[string]any{"expectedVersion": manifest.BaseVersion, "currentVersion": currentVersion})
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE reverse_proxy_routes SET version=version+1,origin_server_ids=?,any_access_json=?,paths_json=?,updated_at=? WHERE domain=? AND version=? AND app_id IN (?,?)`, string(originsRaw), string(anyRaw), string(pathsRaw), now, manifest.RecordID, manifest.BaseVersion, proxyApplicationID, "facility_reverse_proxy")
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "facility configuration changed while editing"), map[string]any{"expectedVersion": manifest.BaseVersion, "recordId": manifest.RecordID})
		}
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		var latest int
		_ = tx.QueryRowContext(ctx, `SELECT version FROM reverse_proxy_routes WHERE domain=?`, manifest.RecordID).Scan(&latest)
		return panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "facility configuration changed while editing"), map[string]any{"expectedVersion": manifest.BaseVersion, "currentVersion": latest})
	}
	return tx.Commit()
}

func (s *Service) finishFacilityCommit(ctx context.Context, record facilityEditRecord, leaseOwner string, result FacilityEditCommitResult) error {
	raw, _ := json.Marshal(result)
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET state=?,commit_result_json=?,commit_lease_owner='',commit_lease_expires_at=NULL,committed_at=?,updated_at=? WHERE id=? AND owner_id=? AND commit_lease_owner=?`, FacilityEditSessionCommitted, string(raw), formatTime(now), formatTime(now), record.ID, facilityEditOwner, leaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return panelerr.Conflict("facility_commit_lease_lost", "facility edit commit lease was lost")
	}
	return nil
}

func (s *Service) resetFacilityCommit(ctx context.Context, sessionID, leaseOwner, state, conflict string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET state=?,conflict_json=?,commit_lease_owner='',commit_lease_expires_at=NULL,updated_at=? WHERE id=? AND commit_lease_owner=?`, state, conflict, formatTime(time.Now().UTC()), sessionID, leaseOwner)
}

func (s *Service) renewFacilityCommitLease(ctx context.Context, sessionID, leaseOwner string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE facility_edit_sessions SET commit_lease_expires_at=?,updated_at=? WHERE id=? AND state=? AND commit_lease_owner=?`, formatTime(time.Now().UTC().Add(facilityEditCommitLease)), formatTime(time.Now().UTC()), sessionID, FacilityEditSessionCommitting, leaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return panelerr.Conflict("facility_commit_lease_lost", "facility edit commit lease was lost")
	}
	return nil
}

func writeFacilityManifest(path string, manifest facilityCommitManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return filestorage.WriteAtomic(path, raw, 0o600)
}

func (s *Service) recoverFacilityEditSessions(ctx context.Context) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM facility_edit_sessions WHERE state=?`, FacilityEditSessionCommitting)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var sessionID string
		if rows.Scan(&sessionID) == nil {
			ids = append(ids, sessionID)
		}
	}
	rows.Close()
	for _, sessionID := range ids {
		if record, loadErr := s.loadFacilityEditSession(ctx, sessionID); loadErr == nil {
			s.recoverFacilityEditRecord(ctx, record)
		}
	}
}

func (s *Service) recoverFacilityEditRecord(ctx context.Context, record facilityEditRecord) {
	if record.ManifestPath == "" {
		s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionActive, "")
		return
	}
	raw, err := os.ReadFile(record.ManifestPath)
	if err != nil {
		s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionConflict, `{"code":"commit_manifest_missing"}`)
		return
	}
	var manifest facilityCommitManifest
	if json.Unmarshal(raw, &manifest) != nil {
		s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionConflict, `{"code":"commit_manifest_invalid"}`)
		return
	}
	if !manifest.DBCommitted {
		manifest.DBCommitted = s.facilityManifestDBCommitted(ctx, manifest)
	}
	if !manifest.DBCommitted {
		currentVersion, versionErr := s.facilityRecordVersion(ctx, manifest.RecordID)
		if versionErr != nil || currentVersion != manifest.BaseVersion {
			s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionConflict, `{"code":"commit_outcome_ambiguous"}`)
			return
		}
		s.rollbackFacilityManifestFiles(manifest)
		s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionActive, "")
		return
	}
	committed, err := s.GetConfigRecord(ctx, manifest.RecordID)
	if err != nil {
		s.resetFacilityCommit(ctx, record.ID, record.CommitLeaseOwner, FacilityEditSessionConflict, `{"code":"commit_outcome_ambiguous"}`)
		return
	}
	plane, _ := s.loadConfig(ctx)
	applyIDs := facilityRecordApplyServers(manifest.PreviousDomain, committed.Domain, plane.DeploymentServers)
	result := FacilityEditCommitResult{
		Record:          committed,
		ResourceVersion: recordResourceVersion(committed),
		ApplyRequested:  true,
		ApplyNeeded:     false,
		ApplyServerIDs:  applyIDs,
		Diagnostics:     []applications.Diagnostic{{Code: "facility_commit_recovered", Severity: "info", Message: i18n.Translate("facility_commit_recovered", "Committed facility configuration was recovered after restart")}},
	}
	if config, cfgErr := s.GetReverseProxy(ctx); cfgErr == nil {
		result.Config = config
	}
	if err := s.triggerConfigReload(ctx, manifest.RecordID, applyIDs); err != nil {
		result.ApplyRequested = false
		result.Diagnostics = append(result.Diagnostics, applications.Diagnostic{Code: "facility_apply_request_failed", Severity: "warning", Message: i18n.Translate("facility_apply_request_failed", "Configuration was recovered, but applying it could not be requested"), Details: map[string]any{"error": err.Error()}})
	}
	if pending := s.pendingDNSDomains(ctx); len(pending) > 0 {
		if _, err := s.triggerDNSSync(ctx, pending, "facility_recovery", false); err != nil {
			result.Diagnostics = append(result.Diagnostics, applications.Diagnostic{Code: "facility_dns_sync_failed", Severity: "warning", Message: i18n.Translate("facility_dns_sync_failed", "Configuration was recovered, but requesting the DNS record sync failed"), Details: map[string]any{"error": err.Error()}})
		}
	}
	_ = s.finishFacilityCommit(ctx, record, record.CommitLeaseOwner, result)
	_ = os.RemoveAll(manifest.BackupDir)
}

func (s *Service) facilityManifestDBCommitted(ctx context.Context, manifest facilityCommitManifest) bool {
	row, err := s.loadRouteRow(ctx, manifest.RecordID)
	if err != nil || row.Version != manifest.BaseVersion+1 {
		return false
	}
	plane, err := s.loadConfig(ctx)
	if err != nil {
		return false
	}
	loaded, err := s.routeRowToDomain(ctx, row)
	if err != nil {
		return false
	}
	got, err := normalizeFacilityDomain(loaded, plane.DeploymentServers)
	if err != nil {
		return false
	}
	want, err := normalizeFacilityDomain(manifest.Domain, plane.DeploymentServers)
	if err != nil || !facilityDomainsEqual(want, got) {
		return false
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,sha256 FROM facility_static_assets WHERE record_id=?`, manifest.RecordID)
	if err != nil {
		return false
	}
	defer rows.Close()
	persisted := map[string]string{}
	for rows.Next() {
		var assetID, sha string
		if rows.Scan(&assetID, &sha) != nil {
			return false
		}
		persisted[assetID] = sha
	}
	if len(persisted) != len(manifest.Assets) {
		return false
	}
	for _, asset := range manifest.Assets {
		if persisted[asset.FinalID] != asset.SHA256 {
			return false
		}
		contentSHA, err := filestorage.HashDirectory(s.staticAssetContentDir(asset.FinalID))
		if err != nil || contentSHA != asset.ContentSHA256 {
			return false
		}
	}
	return true
}

func (s *Service) startFacilityEditSessionCleanup() {
	s.editCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(facilityEditCleanupPeriod)
			defer ticker.Stop()
			for now := range ticker.C {
				s.cleanupFacilityEditSessions(now.UTC())
			}
		}()
	})
}

func (s *Service) cleanupFacilityEditSessions(now time.Time) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	rows, err := s.db.Query(`SELECT id FROM facility_edit_sessions WHERE state=? AND (commit_lease_expires_at IS NULL OR commit_lease_expires_at='' OR commit_lease_expires_at<=?)`, FacilityEditSessionCommitting, formatTime(now))
	if err == nil {
		ids := []string{}
		for rows.Next() {
			var sessionID string
			if rows.Scan(&sessionID) == nil {
				ids = append(ids, sessionID)
			}
		}
		rows.Close()
		for _, sessionID := range ids {
			if record, loadErr := s.loadFacilityEditSession(context.Background(), sessionID); loadErr == nil {
				s.recoverFacilityEditRecord(context.Background(), record)
			}
		}
	}
	rows, err = s.db.Query(`SELECT id FROM facility_edit_sessions WHERE state NOT IN (?,?,?) AND (idle_expires_at<=? OR absolute_expires_at<=?)`, FacilityEditSessionCommitting, FacilityEditSessionCommitted, FacilityEditSessionDiscarded, formatTime(now), formatTime(now))
	if err == nil {
		ids := []string{}
		for rows.Next() {
			var sessionID string
			if rows.Scan(&sessionID) == nil {
				ids = append(ids, sessionID)
			}
		}
		rows.Close()
		for _, sessionID := range ids {
			res, _ := s.db.Exec(`UPDATE facility_edit_sessions SET state=?,updated_at=? WHERE id=? AND state<>?`, FacilityEditSessionExpired, formatTime(now), sessionID, FacilityEditSessionCommitting)
			if affected, _ := res.RowsAffected(); affected == 1 {
				_ = os.RemoveAll(s.facilityEditPath(sessionID))
			}
		}
	}
	cutoff := formatTime(now.Add(-24 * time.Hour))
	rows, err = s.db.Query(`SELECT id FROM facility_edit_sessions WHERE state IN (?,?,?) AND updated_at<=?`, FacilityEditSessionCommitted, FacilityEditSessionDiscarded, FacilityEditSessionExpired, cutoff)
	if err == nil {
		ids := []string{}
		for rows.Next() {
			var sessionID string
			if rows.Scan(&sessionID) == nil {
				ids = append(ids, sessionID)
			}
		}
		rows.Close()
		for _, sessionID := range ids {
			_, _ = s.db.Exec(`DELETE FROM facility_edit_sessions WHERE id=?`, sessionID)
			_ = os.RemoveAll(s.facilityEditPath(sessionID))
		}
	}
	s.cleanupFacilityEditOrphans(now)
}

func (s *Service) cleanupFacilityEditOrphans(now time.Time) {
	entries, err := os.ReadDir(s.editSessionDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.editSessionDir, entry.Name())
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM facility_edit_sessions WHERE id=?`, entry.Name()).Scan(&exists); err == sql.ErrNoRows && filestorage.WorkspaceStale(dir, now, facilityEditOrphanStaleAge) {
			_ = os.RemoveAll(dir)
		}
	}
}

func (s *Service) facilityEditPath(sessionID string) string {
	return filepath.Join(s.editSessionDir, filepath.Base(strings.TrimSpace(sessionID)))
}

func cloneFacilityRouteDomain(in FacilityRouteDomain) FacilityRouteDomain {
	raw, _ := json.Marshal(in)
	var out FacilityRouteDomain
	_ = json.Unmarshal(raw, &out)
	return out
}

func (s *Service) facilityRecordVersion(ctx context.Context, recordID string) (int, error) {
	row, err := s.loadRouteRow(ctx, recordID)
	if err != nil {
		if isPanelNotFound(err) {
			return 0, nil
		}
		return 0, err
	}
	return row.Version, nil
}

func (s *Service) facilityRecordDomain(ctx context.Context, recordID string) (*FacilityRouteDomain, error) {
	row, err := s.loadRouteRow(ctx, recordID)
	if err != nil {
		if isPanelNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	domain, err := s.routeRowToDomain(ctx, row)
	if err != nil {
		return nil, err
	}
	return &domain, nil
}

func (s *Service) normalizeEditDomain(ctx context.Context, record facilityEditRecord) (FacilityRouteDomain, error) {
	draft := record.Draft
	draft.Domain = record.RecordID
	plane, err := s.loadConfig(ctx)
	if err != nil {
		return FacilityRouteDomain{}, err
	}
	return normalizeFacilityDomain(draft, plane.DeploymentServers)
}

func facilityRecordApplyServers(previous *FacilityRouteDomain, next FacilityRouteDomain, gateways []string) []string {
	ids := anyAccessRouteServerIDs(next.AnyAccess, next.OriginServerIDs, gateways)
	if previous != nil {
		ids = append(ids, anyAccessRouteServerIDs(previous.AnyAccess, previous.OriginServerIDs, gateways)...)
	}
	return uniqueSorted(ids)
}

func facilityDomainDNSChanged(previous *FacilityRouteDomain, next FacilityRouteDomain) bool {
	if previous == nil {
		return strings.TrimSpace(next.Domain) != ""
	}
	return domainDNSSignature(*previous) != domainDNSSignature(next)
}

func facilityDomainsEqual(left, right FacilityRouteDomain) bool {
	left.Version = 0
	right.Version = 0
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func cloneFacilityDomains(in []FacilityRouteDomain) []FacilityRouteDomain {
	raw, _ := json.Marshal(in)
	var out []FacilityRouteDomain
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []FacilityRouteDomain{}
	}
	return out
}

func insertFacilityEditOperation(ctx context.Context, tx *sql.Tx, sessionID, operationID, idempotencyKey, requestHash string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO facility_edit_session_operations(session_id,client_operation_id,idempotency_key,request_hash,result_json,created_at) VALUES(?,?,?,?,?,?)`, sessionID, operationID, idempotencyKey, requestHash, "", formatTime(time.Now().UTC()))
	if err != nil {
		// 仅唯一约束碰撞才是带应原等序的冗余冲突；其他 DB 错误原样传递，不能装成冗余冲突。
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return panelerr.Conflict("facility_edit_operation_conflict", "facility edit operation already exists")
		}
		return err
	}
	return nil
}

func (s *Service) facilityEditOperationResult(ctx context.Context, sessionID, operationID, idempotencyKey, requestHash string) (FacilityEditSession, bool, error) {
	var storedHash, raw string
	err := s.db.QueryRowContext(ctx, `SELECT request_hash,result_json FROM facility_edit_session_operations WHERE session_id=? AND (client_operation_id=? OR idempotency_key=?)`, sessionID, operationID, facilityIdempotencyKey(idempotencyKey)).Scan(&storedHash, &raw)
	if err == sql.ErrNoRows {
		return FacilityEditSession{}, false, nil
	}
	if err != nil {
		return FacilityEditSession{}, false, err
	}
	if storedHash != requestHash {
		return FacilityEditSession{}, true, panelerr.Conflict("idempotency_key_reused", "idempotency key was used for a different request")
	}
	if raw != "" {
		var result FacilityEditSession
		if json.Unmarshal([]byte(raw), &result) == nil {
			return result, true, nil
		}
	}
	result, err := s.GetFacilityEditSession(ctx, sessionID)
	return result, true, err
}

func (s *Service) storeFacilityEditOperationResult(ctx context.Context, sessionID, operationID string, result FacilityEditSession) error {
	raw, _ := json.Marshal(result)
	_, err := s.db.ExecContext(ctx, `UPDATE facility_edit_session_operations SET result_json=? WHERE session_id=? AND client_operation_id=?`, string(raw), sessionID, operationID)
	return err
}

func facilityIdempotencyKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "missing"
	}
	return value
}

func facilityEditHash(values ...any) string {
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func facilityPanelErrorCode(err error) string {
	var target *panelerr.Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
