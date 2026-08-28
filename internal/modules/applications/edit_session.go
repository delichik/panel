package applications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	id "panel/internal/platform/identity"
)

const (
	editSessionIdleTTL          = 24 * time.Hour
	editSessionAbsoluteTTL      = 7 * 24 * time.Hour
	editSessionPreviewTTL       = 5 * time.Minute
	editSessionCommitLease      = 2 * time.Minute
	editSessionCleanupPeriod    = 10 * time.Minute
	editSessionOrphanStaleAfter = time.Hour
	applicationEditSessionOwner = "panel-single-administrator"
)

// editOperationRow 承载 editOperationResult 的幂等查询结果列。
type editOperationRow struct {
	RequestHash string `orm:"column:request_hash"`
	ResultJSON  string `orm:"column:result_json"`
}

// editSessionConflictRow 承载 editMutationConflict 读取的版本/状态列。
type editSessionConflictRow struct {
	Revision int    `orm:"column:revision"`
	State    string `orm:"column:state"`
}

// editSessionCleanupRow 承载 cleanupEditSessions 恢复提交扫描的会话列。
type editSessionCleanupRow struct {
	ID      string `orm:"column:id"`
	OwnerID string `orm:"column:owner_id"`
}

type editSessionRecord struct {
	ApplicationEditSession
	OwnerID            string
	PreviewRevision    int
	PreviewExpiresAt   time.Time
	CommitLeaseOwner   string
	CommitLeaseExpires time.Time
	CommitKey          string
	CommitApplication  string
}

func (s *Service) BeginEditSession(ctx context.Context, owner string, in BeginEditSessionInput) (ApplicationEditSession, error) {
	owner = normalizeEditOwner(owner)
	now := time.Now().UTC()
	draft := SaveInput{DeploymentMode: DeploymentModeAll, DeploymentServers: []string{}, ReverseProxy: []ReverseProxyRule{}}
	baseVersion := 0
	var baseUpdatedAt time.Time
	applicationID := strings.TrimSpace(in.ApplicationID)
	if applicationID != "" {
		app, err := s.Get(ctx, applicationID)
		if err != nil {
			return ApplicationEditSession{}, err
		}
		if app.Kind == ApplicationKindFacility || app.DeletionRequested {
			return ApplicationEditSession{}, panelerr.NotFound("application")
		}
		draft = saveInputFromApplication(app)
		baseVersion = app.Version
		baseUpdatedAt = app.UpdatedAt
	}
	if in.Draft != nil {
		draft = normalizeEditDraft(*in.Draft)
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	sessionID := id.New("aedit")
	dir := s.editSessionPath(sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ApplicationEditSession{}, err
	}
	created := false
	defer func() {
		if !created {
			_ = os.RemoveAll(dir)
		}
	}()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = orm.RawExec(ctx, tx, `INSERT INTO application_edit_sessions(id,application_id,owner_id,client_draft_key,state,base_resource_version,base_resource_updated_at,draft_json,revision,idle_expires_at,absolute_expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sessionID, applicationID, owner, strings.TrimSpace(in.ClientDraftKey), EditSessionStateActive, baseVersion, formatOptionalEditTime(baseUpdatedAt), string(raw), 1, formatTime(now.Add(editSessionIdleTTL)), formatTime(now.Add(editSessionAbsoluteTTL)), formatTime(now), formatTime(now))
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if applicationID != "" {
		files, err := s.listFiles(ctx, applicationID, true)
		if err != nil {
			return ApplicationEditSession{}, err
		}
		for _, file := range files {
			fileKey := file.ID
			blobPath := filepath.Join(dir, fileKey)
			if err := os.WriteFile(blobPath, file.Content, 0o600); err != nil {
				return ApplicationEditSession{}, err
			}
			if _, err := orm.RawExec(ctx, tx, `INSERT INTO application_edit_session_files(session_id,file_key,name,kind,content_type,size,sha256,blob_path,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				sessionID, fileKey, file.Name, file.Kind, file.ContentType, file.Size, file.SHA256, blobPath, "ready", formatTime(file.CreatedAt), formatTime(file.UpdatedAt)); err != nil {
				return ApplicationEditSession{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return ApplicationEditSession{}, err
	}
	created = true
	return s.GetEditSession(ctx, owner, sessionID)
}

func (s *Service) GetEditSession(ctx context.Context, owner, sessionID string) (ApplicationEditSession, error) {
	record, err := s.loadEditSession(ctx, normalizeEditOwner(owner), sessionID)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if record.State == EditSessionStateCommitting && !record.CommitLeaseExpires.IsZero() && time.Now().UTC().After(record.CommitLeaseExpires) {
		if recovered, ok := s.recoverEditCommit(ctx, record); ok {
			return recovered, nil
		}
		refreshed, reloadErr := s.loadEditSession(ctx, record.OwnerID, record.ID)
		if reloadErr == nil {
			return refreshed.ApplicationEditSession, nil
		}
	}
	return record.ApplicationEditSession, nil
}

func (s *Service) PatchEditSession(ctx context.Context, owner, sessionID string, in PatchEditSessionInput) (ApplicationEditSession, error) {
	draft := normalizeEditDraft(in.Draft)
	raw, err := json.Marshal(draft)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, s.db, `UPDATE application_edit_sessions SET draft_json=?,revision=revision+1,preview_token='',preview_revision=0,preview_expires_at=NULL,state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND absolute_expires_at>?`,
		string(raw), EditSessionStateActive, formatTime(now), formatTime(now.Add(editSessionIdleTTL)), strings.TrimSpace(sessionID), normalizeEditOwner(owner), in.Revision, EditSessionStateActive, EditSessionStateConflict, formatTime(now))
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if err := expectEditMutation(res); err != nil {
		return ApplicationEditSession{}, s.editMutationConflict(ctx, owner, sessionID, in.Revision)
	}
	return s.GetEditSession(ctx, owner, sessionID)
}

func (s *Service) PutEditSessionFile(ctx context.Context, owner, sessionID, fileRef, idempotencyKey string, in EditSessionFileInput) (ApplicationEditSession, error) {
	kind := strings.TrimSpace(in.Kind)
	if kind != "" && kind != ApplicationFileKindTemplate {
		return ApplicationEditSession{}, panelerr.Validation("application_file_kind_invalid", "JSON file writes only support template files")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
	if err != nil {
		return ApplicationEditSession{}, panelerr.Validation("application_file_content_invalid", "file content must be base64 encoded")
	}
	if strings.TrimSpace(in.ClientOperationID) == "" {
		return ApplicationEditSession{}, panelerr.Validation("client_operation_id_required", "clientOperationId is required")
	}
	name, err := normalizeApplicationFileName(firstNonEmpty(in.Name, in.Path))
	if err != nil {
		return ApplicationEditSession{}, err
	}
	kind = ApplicationFileKindTemplate
	fileRef = strings.TrimSpace(fileRef)
	if fileRef == "" {
		return ApplicationEditSession{}, panelerr.Validation("application_file_name_invalid", "file name is required")
	}
	fileKey, err := s.resolveEditSessionFileKey(ctx, sessionID, fileRef)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if fileKey == "" {
		fileKey = fileRef
	}
	contentType := inferApplicationFileContentType(name, content, true)
	requestHash := editRequestHash(in.Revision, name, kind, contentType, content)
	if result, ok, err := s.editOperationResult(ctx, owner, sessionID, in.ClientOperationID, idempotencyKey, requestHash); err != nil || ok {
		return result, err
	}
	return s.writeEditSessionFile(ctx, owner, sessionID, fileKey, idempotencyKey, in.ClientOperationID, requestHash, in.Revision, name, kind, contentType, content)
}

func (s *Service) UploadEditSessionBinary(ctx context.Context, owner, sessionID, fileRef, idempotencyKey string, in EditSessionBinaryInput) (ApplicationEditSession, error) {
	if strings.TrimSpace(in.ClientOperationID) == "" {
		return ApplicationEditSession{}, panelerr.Validation("client_operation_id_required", "clientOperationId is required")
	}
	if len(in.Content) == 0 {
		return ApplicationEditSession{}, panelerr.Validation("application_file_content_invalid", "file content is required")
	}
	name, err := normalizeApplicationFileName(firstNonEmpty(in.Name, in.Path))
	if err != nil {
		return ApplicationEditSession{}, err
	}
	fileRef = strings.TrimSpace(fileRef)
	if fileRef == "" {
		return ApplicationEditSession{}, panelerr.Validation("application_file_name_invalid", "file name is required")
	}
	fileKey, err := s.resolveEditSessionFileKey(ctx, sessionID, fileRef)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if fileKey == "" {
		fileKey = fileRef
	}
	contentType := inferApplicationFileContentType(firstNonEmpty(strings.TrimSpace(in.FileName), name), in.Content, false)
	requestHash := editRequestHash(in.Revision, name, ApplicationFileKindBinary, contentType, in.Content)
	if result, ok, err := s.editOperationResult(ctx, owner, sessionID, in.ClientOperationID, idempotencyKey, requestHash); err != nil || ok {
		return result, err
	}
	return s.writeEditSessionFile(ctx, owner, sessionID, fileKey, idempotencyKey, in.ClientOperationID, requestHash, in.Revision, name, ApplicationFileKindBinary, contentType, in.Content)
}

func (s *Service) GetEditSessionFile(ctx context.Context, owner, sessionID, fileRef string) (EditSessionFileContent, error) {
	record, err := s.loadEditSession(ctx, normalizeEditOwner(owner), sessionID)
	if err != nil {
		return EditSessionFileContent{}, err
	}
	if record.State != EditSessionStateActive && record.State != EditSessionStateConflict {
		return EditSessionFileContent{}, panelerr.Conflict("application_edit_session_state_invalid", "application edit session is not editable")
	}

	fileKey, resolveErr := s.resolveEditSessionFileKey(ctx, record.ID, fileRef)
	if resolveErr != nil {
		return EditSessionFileContent{}, resolveErr
	}
	var m models.ApplicationEditSessionFile
	err = orm.New(s.db).From("application_edit_session_files").Select("file_key", "name", "kind", "content_type", "size", "sha256", "blob_path").Where("session_id=?", record.ID).And("file_key=?", strings.TrimSpace(fileKey)).And("state=?", "ready").First(ctx, &m)
	if err == sql.ErrNoRows {
		return EditSessionFileContent{}, panelerr.NotFound("application_edit_session_file")
	}
	if err != nil {
		return EditSessionFileContent{}, err
	}
	result := EditSessionFileContent{
		FileKey:     m.FileKey,
		Name:        m.Name,
		Kind:        m.Kind,
		ContentType: m.ContentType,
		Size:        int64(m.Size),
		SHA256:      m.SHA256,
	}
	blobPath := m.BlobPath
	content, err := readEditSessionBlob(result.FileKey, blobPath, result.Size, result.SHA256)
	if err != nil {
		return EditSessionFileContent{}, err
	}
	result.ContentBase64 = base64.StdEncoding.EncodeToString(content)
	result.Content = content
	result.Path = result.Name
	return result, nil
}

// resolveEditSessionFileKey translates the public name reference to the
// storage key used by older edit sessions. New requests never need to know
// that key, but the fallback keeps pre-name sessions addressable by name.
func (s *Service) resolveEditSessionFileKey(ctx context.Context, sessionID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	var fileKey string
	err := orm.New(s.db).From("application_edit_session_files").Select("file_key").Where("session_id=?", strings.TrimSpace(sessionID)).And("name=?", ref).And("state=?", "ready").ScanValue(ctx, &fileKey)
	if err == sql.ErrNoRows {
		err = orm.New(s.db).From("application_edit_session_files").Select("file_key").Where("session_id=?", strings.TrimSpace(sessionID)).And("file_key=?", ref).And("state=?", "ready").ScanValue(ctx, &fileKey)
	}
	if err == sql.ErrNoRows {
		return "", nil
	}
	return fileKey, err
}

func (s *Service) UploadEditSessionArchive(ctx context.Context, owner, sessionID, idempotencyKey string, in EditSessionArchiveInput) (ApplicationEditSession, error) {
	if strings.TrimSpace(in.ClientOperationID) == "" {
		return ApplicationEditSession{}, panelerr.Validation("application_edit_operation_invalid", "name and clientOperationId are required")
	}
	if len(in.Content) == 0 {
		return ApplicationEditSession{}, panelerr.Validation("application_file_content_invalid", "archive file is required")
	}
	name, err := normalizeApplicationFileName(firstNonEmpty(in.Name, in.BasePath))
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if _, err := extractApplicationFileArchive(strings.NewReader(string(in.Content)), int64(len(in.Content)), in.FileName); err != nil {
		// strings.Reader implements ReaderAt; using it keeps validation identical to the legacy path.
		return ApplicationEditSession{}, err
	}
	fileKey, err := s.resolveEditSessionFileKey(ctx, sessionID, name)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	if fileKey == "" && strings.TrimSpace(in.FileKey) != "" {
		fileKey, err = s.resolveEditSessionFileKey(ctx, sessionID, in.FileKey)
		if err != nil {
			return ApplicationEditSession{}, err
		}
	}
	if fileKey == "" {
		fileKey = name
	}
	requestHash := editRequestHash(in.Revision, name, ApplicationFileKindArchive, in.FileName, in.Content)
	if result, ok, err := s.editOperationResult(ctx, owner, sessionID, in.ClientOperationID, idempotencyKey, requestHash); err != nil || ok {
		return result, err
	}
	return s.writeEditSessionFile(ctx, owner, sessionID, fileKey, idempotencyKey, in.ClientOperationID, requestHash, in.Revision, name, ApplicationFileKindArchive, in.FileName, in.Content)
}

func (s *Service) DeleteEditSessionFile(ctx context.Context, owner, sessionID, fileRef, idempotencyKey string, in EditSessionMutationInput) (ApplicationEditSession, error) {
	if strings.TrimSpace(in.ClientOperationID) == "" {
		return ApplicationEditSession{}, panelerr.Validation("client_operation_id_required", "clientOperationId is required")
	}
	fileKey, err := s.resolveEditSessionFileKey(ctx, sessionID, fileRef)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	requestHash := editRequestHash(in.Revision, strings.TrimSpace(fileRef), "delete")
	if result, ok, err := s.editOperationResult(ctx, owner, sessionID, in.ClientOperationID, idempotencyKey, requestHash); err != nil || ok {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var blobPath string
	_ = orm.New(tx).From("application_edit_session_files").Select("blob_path").Where("session_id=?", sessionID).And("file_key=?", fileKey).ScanValue(ctx, &blobPath)
	if err := orm.New(tx).From("application_edit_session_files").Where("session_id=?", sessionID).And("file_key=?", fileKey).Delete(ctx); err != nil {
		return ApplicationEditSession{}, err
	}
	if err := s.bumpEditRevision(ctx, tx, owner, sessionID, in.Revision); err != nil {
		return ApplicationEditSession{}, err
	}
	if err := insertEditOperation(ctx, tx, sessionID, in.ClientOperationID, requireIdempotencyKey(idempotencyKey), requestHash); err != nil {
		return ApplicationEditSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApplicationEditSession{}, err
	}
	if blobPath != "" {
		_ = os.Remove(blobPath)
	}
	result, err := s.GetEditSession(ctx, owner, sessionID)
	if err == nil {
		_ = s.storeEditOperationResult(ctx, sessionID, in.ClientOperationID, result)
	}
	return result, err
}

func (s *Service) ValidateEditSession(ctx context.Context, owner, sessionID string, revision int) (EditSessionValidationResult, error) {
	record, files, err := s.editSessionWithFiles(ctx, owner, sessionID, revision)
	if err != nil {
		return EditSessionValidationResult{}, err
	}
	diagnostics, err := s.validateEditDraft(ctx, record, files)
	if err != nil {
		return EditSessionValidationResult{}, err
	}
	now := time.Now().UTC()
	_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", record.ID).And("owner_id=?", record.OwnerID).And("revision=?", record.Revision).And("state=?", EditSessionStateActive).UpdateColumns(ctx, map[string]any{
		"updated_at":      formatTime(now),
		"idle_expires_at": formatTime(now.Add(editSessionIdleTTL)),
	})
	return EditSessionValidationResult{Valid: !hasBlockingDiagnostic(diagnostics), Revision: record.Revision, Diagnostics: diagnostics}, nil
}

func (s *Service) PreviewEditSession(ctx context.Context, owner, sessionID string, revision int) (EditSessionPreviewResult, error) {
	validation, err := s.ValidateEditSession(ctx, owner, sessionID, revision)
	if err != nil {
		return EditSessionPreviewResult{}, err
	}
	now := time.Now().UTC()
	token := id.New("apreview")
	record, err := s.loadEditSession(ctx, normalizeEditOwner(owner), sessionID)
	if err != nil {
		return EditSessionPreviewResult{}, err
	}
	diagnostics := append([]Diagnostic{}, validation.Diagnostics...)
	diagnostics = append(diagnostics, Diagnostic{Code: "application_cross_module_insights_unavailable", Severity: "info", Message: i18n.Translate("application_cross_module_insights_unavailable", "Cross-module DNS, certificate, and gateway insights are not available in this API revision")})
	res, err := orm.RawExec(ctx, s.db, `UPDATE application_edit_sessions SET preview_token=?,preview_revision=?,preview_expires_at=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state=?`,
		token, revision, formatTime(now.Add(editSessionPreviewTTL)), formatTime(now), formatTime(now.Add(editSessionIdleTTL)), sessionID, normalizeEditOwner(owner), revision, EditSessionStateActive)
	if err != nil {
		return EditSessionPreviewResult{}, err
	}
	if err := expectEditMutation(res); err != nil {
		return EditSessionPreviewResult{}, s.editMutationConflict(ctx, owner, sessionID, revision)
	}
	return EditSessionPreviewResult{Revision: revision, Diagnostics: diagnostics, Token: PreviewToken{Value: token, Action: "application.commit", SubjectVersion: strconv.Itoa(record.BaseResourceVersion.ValueInt())}, ExpiresAt: now.Add(editSessionPreviewTTL)}, nil
}

func (s *Service) CommitEditSession(ctx context.Context, owner, sessionID, idempotencyKey string, in CommitEditSessionInput) (EditCommitResult, error) {
	owner = normalizeEditOwner(owner)
	record, files, err := s.editSessionWithFiles(ctx, owner, sessionID, in.Revision)
	if err != nil {
		return EditCommitResult{}, err
	}
	if record.State == EditSessionStateCommitted && record.CommitResult != nil {
		if record.CommitKey != requireIdempotencyKey(idempotencyKey) {
			return EditCommitResult{}, panelerr.Conflict("idempotency_key_reused", "commit idempotency key does not match the completed request")
		}
		return *record.CommitResult, nil
	}
	if record.PreviewToken == nil || record.PreviewToken.Value != strings.TrimSpace(in.PreviewToken) || record.PreviewRevision != in.Revision || time.Now().UTC().After(record.PreviewExpiresAt) {
		return EditCommitResult{}, panelerr.Conflict("preview_stale", "application edit preview is missing or stale")
	}
	if strings.TrimSpace(in.BaseResourceVersion) != strconv.Itoa(record.BaseResourceVersion.ValueInt()) {
		return EditCommitResult{}, panelerr.Conflict("resource_version_conflict", "application base version changed")
	}
	diagnostics, err := s.validateEditDraft(ctx, record, files)
	if err != nil {
		return EditCommitResult{}, err
	}
	if hasBlockingDiagnostic(diagnostics) {
		return EditCommitResult{}, panelerr.WithDetails(panelerr.Validation("application_invalid", "application edit session is invalid"), map[string]any{"diagnostics": diagnostics})
	}
	commitKey := requireIdempotencyKey(idempotencyKey)
	commitApplicationID := record.ApplicationID
	if commitApplicationID == "" {
		commitApplicationID = id.New("app")
	}
	now := time.Now().UTC()
	leaseOwner := id.New("acommit")
	res, err := orm.RawExec(ctx, s.db, `UPDATE application_edit_sessions SET state=?,commit_lease_owner=?,commit_lease_expires_at=?,commit_idempotency_key=?,commit_application_id=?,updated_at=? WHERE id=? AND owner_id=? AND revision=? AND state=?`,
		EditSessionStateCommitting, leaseOwner, formatTime(now.Add(editSessionCommitLease)), commitKey, commitApplicationID, formatTime(now), sessionID, owner, in.Revision, EditSessionStateActive)
	if err != nil {
		return EditCommitResult{}, err
	}
	if err := expectEditMutation(res); err != nil {
		return EditCommitResult{}, s.editMutationConflict(ctx, owner, sessionID, in.Revision)
	}
	record.CommitApplication = commitApplicationID
	record.CommitKey = commitKey
	var app Application
	if record.ApplicationID == "" {
		app, err = s.createWithFilesID(ctx, commitApplicationID, record.Draft, files)
	} else {
		app, err = s.updateWithFilesIfVersion(ctx, record.ApplicationID, record.BaseResourceVersion.ValueInt(), record.Draft, files)
	}
	if err != nil {
		if panelErrorCode(err) == "resource_version_conflict" {
			_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", sessionID).And("owner_id=?", owner).And("commit_lease_owner=?", leaseOwner).UpdateColumns(ctx, map[string]any{
				"state":                   EditSessionStateConflict,
				"conflict_json":           `{"code":"resource_version_conflict"}`,
				"commit_lease_owner":      "",
				"commit_lease_expires_at": "",
				"updated_at":              formatTime(time.Now().UTC()),
			})
			return EditCommitResult{}, err
		}
		if recovered, ok := s.recoverPersistedEditCommit(ctx, record, files, err); ok {
			return recovered, nil
		}
		nextState := EditSessionStateActive
		conflictJSON := ""
		if s.editCommitPersistenceObserved(ctx, record) {
			nextState = EditSessionStateConflict
			conflictJSON = `{"code":"commit_outcome_ambiguous"}`
		}
		_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", sessionID).And("owner_id=?", owner).And("commit_lease_owner=?", leaseOwner).UpdateColumns(ctx, map[string]any{
			"state":                   nextState,
			"conflict_json":           conflictJSON,
			"commit_lease_owner":      "",
			"commit_lease_expires_at": "",
			"updated_at":              formatTime(time.Now().UTC()),
		})
		return EditCommitResult{}, err
	}
	result := EditCommitResult{Application: app, ResourceVersion: applicationResourceVersion(app), ApplyRequested: record.Draft.Enabled, Diagnostics: []Diagnostic{{Code: "application_apply_operation_reference_unavailable", Severity: "info", Message: "Configuration was committed and reconciliation was requested; an operation reference is not available from the current coordinator"}}}
	resultRaw, _ := json.Marshal(result)
	committedAt := time.Now().UTC()
	err = orm.New(s.db).From("application_edit_sessions").Where("id=?", sessionID).And("owner_id=?", owner).And("commit_lease_owner=?", leaseOwner).UpdateColumns(ctx, map[string]any{
		"state":                   EditSessionStateCommitted,
		"commit_result_json":      string(resultRaw),
		"commit_lease_owner":      "",
		"commit_lease_expires_at": "",
		"committed_at":            formatTime(committedAt),
		"updated_at":              formatTime(committedAt),
	})
	return result, err
}

func (s *Service) DiscardEditSession(ctx context.Context, owner, sessionID string) error {
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, s.db, `UPDATE application_edit_sessions SET state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND state IN (?,?)`,
		EditSessionStateDiscarded, formatTime(now), formatTime(now), strings.TrimSpace(sessionID), normalizeEditOwner(owner), EditSessionStateActive, EditSessionStateConflict)
	if err != nil {
		return err
	}
	if err := expectEditMutation(res); err != nil {
		return panelerr.Conflict("application_edit_session_not_discardable", "application edit session cannot be discarded in its current state")
	}
	_ = os.RemoveAll(s.editSessionPath(sessionID))
	return nil
}

func (s *Service) writeEditSessionFile(ctx context.Context, owner, sessionID, fileKey, idempotencyKey, operationID, requestHash string, revision int, name, kind, contentType string, content []byte) (ApplicationEditSession, error) {
	owner = normalizeEditOwner(owner)
	dir := s.editSessionPath(sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ApplicationEditSession{}, err
	}
	tempPath := filepath.Join(dir, id.New("partial")+".partial")
	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return ApplicationEditSession{}, err
	}
	defer os.Remove(tempPath)
	// Every attempted revision gets a new immutable blob. A stale revision or a
	// database constraint failure must never overwrite the blob referenced by
	// the current row.
	blobPath := filepath.Join(dir, id.New("blob")+".blob")
	if err := os.Rename(tempPath, blobPath); err != nil {
		return ApplicationEditSession{}, err
	}
	blobCommitted := false
	defer func() {
		if !blobCommitted {
			_ = os.Remove(blobPath)
		}
	}()
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplicationEditSession{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var previousBlobPath string
	_ = orm.New(tx).From("application_edit_session_files").Select("blob_path").Where("session_id=?", sessionID).And("file_key=?", fileKey).ScanValue(ctx, &previousBlobPath)
	if err := s.bumpEditRevision(ctx, tx, owner, sessionID, revision); err != nil {
		return ApplicationEditSession{}, err
	}
	_, err = orm.RawExec(ctx, tx, `INSERT INTO application_edit_session_files(session_id,file_key,name,kind,content_type,size,sha256,blob_path,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(session_id,file_key) DO UPDATE SET name=excluded.name,kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,blob_path=excluded.blob_path,state='ready',updated_at=excluded.updated_at`,
		sessionID, fileKey, name, kind, strings.TrimSpace(contentType), len(content), hex.EncodeToString(sum[:]), blobPath, "ready", formatTime(now), formatTime(now))
	if err != nil {
		return ApplicationEditSession{}, applicationSaveError(err)
	}
	if err := insertEditOperation(ctx, tx, sessionID, operationID, requireIdempotencyKey(idempotencyKey), requestHash); err != nil {
		return ApplicationEditSession{}, err
	}
	if err := tx.Commit(); err != nil {
		return ApplicationEditSession{}, err
	}
	blobCommitted = true
	if previousBlobPath != "" && previousBlobPath != blobPath {
		_ = os.Remove(previousBlobPath)
	}
	result, err := s.GetEditSession(ctx, owner, sessionID)
	if err == nil {
		_ = s.storeEditOperationResult(ctx, sessionID, operationID, result)
	}
	return result, err
}

func (s *Service) bumpEditRevision(ctx context.Context, tx *sql.Tx, owner, sessionID string, revision int) error {
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, tx, `UPDATE application_edit_sessions SET revision=revision+1,preview_token='',preview_revision=0,preview_expires_at=NULL,state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND absolute_expires_at>?`,
		EditSessionStateActive, formatTime(now), formatTime(now.Add(editSessionIdleTTL)), sessionID, owner, revision, EditSessionStateActive, EditSessionStateConflict, formatTime(now))
	if err != nil {
		return err
	}
	if err := expectEditMutation(res); err != nil {
		return s.editMutationConflict(ctx, owner, sessionID, revision)
	}
	return nil
}

func (s *Service) editSessionWithFiles(ctx context.Context, owner, sessionID string, revision int) (editSessionRecord, []ApplicationFile, error) {
	record, err := s.loadEditSession(ctx, normalizeEditOwner(owner), sessionID)
	if err != nil {
		return editSessionRecord{}, nil, err
	}
	if record.Revision != revision {
		return editSessionRecord{}, nil, s.editMutationConflict(ctx, owner, sessionID, revision)
	}
	if record.State != EditSessionStateActive && record.State != EditSessionStateCommitted {
		return editSessionRecord{}, nil, panelerr.Conflict("application_edit_session_state_invalid", "application edit session is not active")
	}
	files, err := s.editSessionApplicationFiles(ctx, sessionID, record.ApplicationID)
	return record, files, err
}

func (s *Service) loadEditSession(ctx context.Context, owner, sessionID string) (editSessionRecord, error) {
	var row editSessionRow
	if err := orm.New(s.db).From("application_edit_sessions").Where("id=?", strings.TrimSpace(sessionID)).And("owner_id=?", owner).First(ctx, &row); err != nil {
		if err == sql.ErrNoRows {
			return editSessionRecord{}, panelerr.NotFound("application_edit_session")
		}
		return editSessionRecord{}, err
	}
	record := toEditSessionRecord(row)
	now := time.Now().UTC()
	if record.State != EditSessionStateCommitting && record.State != EditSessionStateCommitted && record.State != EditSessionStateDiscarded && record.State != EditSessionStateExpired && (now.After(record.IdleExpiresAt) || now.After(record.AbsoluteExpiresAt)) {
		_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", record.ID).And("owner_id=?", owner).UpdateColumns(ctx, map[string]any{
			"state":      EditSessionStateExpired,
			"updated_at": formatTime(now),
		})
		record.State = EditSessionStateExpired
		_ = os.RemoveAll(s.editSessionPath(record.ID))
	}
	files, err := s.editSessionFiles(ctx, record.ID)
	if err != nil {
		return editSessionRecord{}, err
	}
	record.Files = files
	return record, nil
}

func (s *Service) editSessionFiles(ctx context.Context, sessionID string) ([]EditSessionFile, error) {
	var rows []models.ApplicationEditSessionFile
	if err := orm.New(s.db).From("application_edit_session_files").Select("file_key", "name", "kind", "content_type", "size", "sha256", "created_at", "updated_at").Where("session_id=?", sessionID).And("state=?", "ready").OrderBy("name").All(ctx, &rows); err != nil {
		return nil, err
	}
	items := make([]EditSessionFile, 0, len(rows))
	for _, m := range rows {
		items = append(items, EditSessionFile{
			FileKey:     m.FileKey,
			Name:        m.Name,
			Path:        m.Name,
			Kind:        m.Kind,
			ContentType: m.ContentType,
			Size:        int64(m.Size),
			SHA256:      m.SHA256,
			CreatedAt:   m.CreatedAt,
			UpdatedAt:   m.UpdatedAt,
		})
	}
	return items, nil
}

func (s *Service) editSessionApplicationFiles(ctx context.Context, sessionID, applicationID string) ([]ApplicationFile, error) {
	var rows []models.ApplicationEditSessionFile
	if err := orm.New(s.db).From("application_edit_session_files").Select("file_key", "name", "kind", "content_type", "size", "sha256", "blob_path", "created_at", "updated_at").Where("session_id=?", sessionID).And("state=?", "ready").OrderBy("name").All(ctx, &rows); err != nil {
		return nil, err
	}
	files := make([]ApplicationFile, 0, len(rows))
	for _, m := range rows {
		file := ApplicationFile{
			ID:            m.FileKey,
			ApplicationID: applicationID,
			Name:          m.Name,
			Path:          m.Name,
			Kind:          m.Kind,
			ContentType:   m.ContentType,
			Size:          int64(m.Size),
			SHA256:        m.SHA256,
			CreatedAt:     m.CreatedAt,
			UpdatedAt:     m.UpdatedAt,
		}
		content, err := readEditSessionBlob(m.FileKey, m.BlobPath, file.Size, file.SHA256)
		if err != nil {
			return nil, err
		}
		file.Content = content
		files = append(files, file)
	}
	return files, nil
}

func readEditSessionBlob(fileKey, blobPath string, expectedSize int64, expectedHash string) ([]byte, error) {
	content, err := os.ReadFile(blobPath)
	if err != nil {
		return nil, panelerr.WithDetails(panelerr.Conflict("application_edit_file_missing", "application edit session file is missing"), map[string]any{"fileKey": fileKey})
	}
	if int64(len(content)) != expectedSize {
		return nil, panelerr.WithDetails(panelerr.Conflict("application_edit_file_size_mismatch", "application edit session file size does not match"), map[string]any{"fileKey": fileKey})
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != expectedHash {
		return nil, panelerr.WithDetails(panelerr.Conflict("application_edit_file_hash_mismatch", "application edit session file hash does not match"), map[string]any{"fileKey": fileKey})
	}
	return content, nil
}

func (s *Service) validateEditDraft(ctx context.Context, record editSessionRecord, files []ApplicationFile) ([]Diagnostic, error) {
	appID := record.ApplicationID
	generation := 1
	if appID == "" {
		appID = record.CommitApplication
		if appID == "" {
			appID = "application-edit-preview"
		}
	} else {
		app, err := s.Get(ctx, appID)
		if err != nil {
			return nil, err
		}
		generation = app.Generation
	}
	_, err := s.prepareWithFiles(ctx, record.Draft, generation, appID, files)
	if err == nil {
		return []Diagnostic{}, nil
	}
	var pe *panelerr.Error
	if errors.As(err, &pe) {
		diagnostics := []Diagnostic{{Code: pe.Code, Severity: "error", Message: i18n.Translate(pe.Code, pe.Message)}}
		if issues, ok := pe.Details["issues"].([]ValidationIssue); ok {
			diagnostics = diagnostics[:0]
			for _, issue := range issues {
				diagnostics = append(diagnostics, Diagnostic{Code: pe.Code, Severity: "error", Field: issue.Field, Message: i18n.Translate("", issue.Message)})
			}
		}
		return diagnostics, nil
	}
	return nil, err
}

func (s *Service) recoverEditCommit(ctx context.Context, record editSessionRecord) (ApplicationEditSession, bool) {
	if record.CommitApplication == "" {
		return ApplicationEditSession{}, false
	}
	files, err := s.editSessionApplicationFiles(ctx, record.ID, record.ApplicationID)
	if err != nil {
		return ApplicationEditSession{}, false
	}
	result, ok := s.persistedEditCommitResult(ctx, record, files, nil)
	if !ok {
		nextState := EditSessionStateActive
		conflictJSON := ""
		if s.editCommitPersistenceObserved(ctx, record) {
			nextState = EditSessionStateConflict
			conflictJSON = `{"code":"commit_outcome_ambiguous"}`
		}
		_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", record.ID).And("owner_id=?", record.OwnerID).And("state=?", EditSessionStateCommitting).UpdateColumns(ctx, map[string]any{
			"state":                   nextState,
			"conflict_json":           conflictJSON,
			"commit_lease_owner":      "",
			"commit_lease_expires_at": "",
			"updated_at":              formatTime(time.Now().UTC()),
		})
		return ApplicationEditSession{}, false
	}
	result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "application_commit_recovered", Severity: "info", Message: i18n.Translate("application_commit_recovered", "The committed application was recovered after an interrupted response")})
	raw, _ := json.Marshal(result)
	now := time.Now().UTC()
	err = orm.New(s.db).From("application_edit_sessions").Where("id=?", record.ID).And("owner_id=?", record.OwnerID).And("state=?", EditSessionStateCommitting).UpdateColumns(ctx, map[string]any{
		"state":                   EditSessionStateCommitted,
		"commit_result_json":      string(raw),
		"commit_lease_owner":      "",
		"commit_lease_expires_at": "",
		"committed_at":            formatTime(now),
		"updated_at":              formatTime(now),
	})
	if err != nil {
		return ApplicationEditSession{}, false
	}
	resultSession, err := s.loadEditSession(ctx, record.OwnerID, record.ID)
	return resultSession.ApplicationEditSession, err == nil
}

func (s *Service) editCommitPersistenceObserved(ctx context.Context, record editSessionRecord) bool {
	appID := record.CommitApplication
	if appID == "" {
		appID = record.ApplicationID
	}
	if appID == "" {
		return false
	}
	app, err := s.Get(ctx, appID)
	if err != nil {
		return false
	}
	if record.ApplicationID == "" {
		// The create commit reserves a unique application ID before persistence;
		// observing that ID proves the create transaction committed.
		return true
	}
	return app.Version > record.BaseResourceVersion.ValueInt()
}

func (s *Service) recoverPersistedEditCommit(ctx context.Context, record editSessionRecord, files []ApplicationFile, applyErr error) (EditCommitResult, bool) {
	result, ok := s.persistedEditCommitResult(ctx, record, files, applyErr)
	if !ok {
		return EditCommitResult{}, false
	}
	raw, _ := json.Marshal(result)
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, s.db, `UPDATE application_edit_sessions SET state=?,commit_result_json=?,commit_lease_owner='',commit_lease_expires_at=NULL,committed_at=?,updated_at=? WHERE id=? AND owner_id=? AND state=?`, EditSessionStateCommitted, string(raw), formatTime(now), formatTime(now), record.ID, record.OwnerID, EditSessionStateCommitting)
	if err != nil || expectEditMutation(res) != nil {
		return EditCommitResult{}, false
	}
	return result, true
}

func (s *Service) persistedEditCommitResult(ctx context.Context, record editSessionRecord, files []ApplicationFile, applyErr error) (EditCommitResult, bool) {
	appID := record.CommitApplication
	if appID == "" {
		appID = record.ApplicationID
	}
	if appID == "" {
		return EditCommitResult{}, false
	}
	app, err := s.Get(ctx, appID)
	if err != nil || (record.ApplicationID != "" && app.Version <= record.BaseResourceVersion.ValueInt()) {
		return EditCommitResult{}, false
	}
	if !s.applicationMatchesEditDraft(ctx, app, record.Draft, files) {
		return EditCommitResult{}, false
	}
	persistedFiles, err := s.listFiles(ctx, app.ID, false)
	if err != nil || !applicationFileSetsMatch(persistedFiles, files) {
		return EditCommitResult{}, false
	}
	diagnostics := []Diagnostic{}
	if applyErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: "application_apply_request_failed", Severity: "warning", Message: i18n.Translate("application_apply_request_failed", "Configuration was committed, but applying it could not be requested"), Details: map[string]any{"error": applyErr.Error()}})
	}
	return EditCommitResult{Application: app, ResourceVersion: applicationResourceVersion(app), ApplyRequested: record.Draft.Enabled, Diagnostics: diagnostics}, true
}

func (s *Service) applicationMatchesEditDraft(ctx context.Context, app Application, draft SaveInput, files []ApplicationFile) bool {
	draft = normalizeEditDraft(draft)
	prepared, err := s.prepareWithFiles(ctx, draft, app.Generation, app.ID, files)
	if err != nil {
		return false
	}
	wantName := strings.TrimSpace(draft.Name)
	if wantName == "" {
		wantName = prepared.spec.Name
	}
	deploymentServers := prepared.deploymentServers
	if deploymentServers == nil {
		deploymentServers = []string{}
	}
	reverseProxy := prepared.reverseProxy
	if reverseProxy == nil {
		reverseProxy = []ReverseProxyRule{}
	}
	want := struct {
		Name              string
		Enabled           bool
		SpecYAML          string
		DeploymentMode    string
		DeploymentServers []string
		ReverseProxy      []ReverseProxyRule
	}{wantName, draft.Enabled, draft.SpecYAML, prepared.deploymentMode, deploymentServers, reverseProxy}
	got := struct {
		Name              string
		Enabled           bool
		SpecYAML          string
		DeploymentMode    string
		DeploymentServers []string
		ReverseProxy      []ReverseProxyRule
	}{app.Name, app.Enabled, app.SpecYAML, app.DeploymentMode, app.DeploymentServers, app.ReverseProxy}
	wantRaw, err1 := json.Marshal(want)
	gotRaw, err2 := json.Marshal(got)
	return err1 == nil && err2 == nil && string(wantRaw) == string(gotRaw)
}

func applicationFileSetsMatch(persisted, desired []ApplicationFile) bool {
	if len(persisted) != len(desired) {
		return false
	}
	type signature struct {
		Kind, ContentType, SHA256 string
		Size                      int64
	}
	want := make(map[string]signature, len(desired))
	for _, file := range desired {
		want[file.Name] = signature{file.Kind, file.ContentType, file.SHA256, file.Size}
	}
	for _, file := range persisted {
		if want[file.Name] != (signature{file.Kind, file.ContentType, file.SHA256, file.Size}) {
			return false
		}
		delete(want, file.Name)
	}
	return len(want) == 0
}

func (s *Service) editOperationResult(ctx context.Context, owner, sessionID, operationID, idempotencyKey, requestHash string) (ApplicationEditSession, bool, error) {
	key := requireIdempotencyKey(idempotencyKey)
	var operationRow editOperationRow
	err := orm.New(s.db).From("application_edit_session_operations").Select("request_hash", "result_json").Where("session_id=?", sessionID).WhereGroup(func(c *orm.Condition) {
		c.Or("client_operation_id=?", operationID).Or("idempotency_key=?", key)
	}).First(ctx, &operationRow)
	if err == sql.ErrNoRows {
		return ApplicationEditSession{}, false, nil
	}
	if err != nil {
		return ApplicationEditSession{}, false, err
	}
	storedHash, resultRaw := operationRow.RequestHash, operationRow.ResultJSON
	if storedHash != requestHash {
		return ApplicationEditSession{}, true, panelerr.Conflict("idempotency_key_reused", "idempotency key was already used for a different request")
	}
	if resultRaw != "" {
		var result ApplicationEditSession
		if json.Unmarshal([]byte(resultRaw), &result) == nil {
			return result, true, nil
		}
	}
	result, err := s.GetEditSession(ctx, owner, sessionID)
	return result, true, err
}

func insertEditOperation(ctx context.Context, tx *sql.Tx, sessionID, operationID, idempotencyKey, requestHash string) error {
	_, err := orm.RawExec(ctx, tx, `INSERT INTO application_edit_session_operations(session_id,client_operation_id,idempotency_key,request_hash,result_json,created_at) VALUES(?,?,?,?,?,?)`, sessionID, operationID, idempotencyKey, requestHash, "", formatTime(time.Now().UTC()))
	if err != nil {
		return panelerr.Conflict("application_edit_operation_conflict", "edit session operation already exists")
	}
	return nil
}

func (s *Service) storeEditOperationResult(ctx context.Context, sessionID, operationID string, result ApplicationEditSession) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	err = orm.New(s.db).From("application_edit_session_operations").Where("session_id=?", sessionID).And("client_operation_id=?", operationID).UpdateColumns(ctx, map[string]any{
		"result_json": string(raw),
	})
	return err
}

func (s *Service) editMutationConflict(ctx context.Context, owner, sessionID string, expected int) error {
	var conflictRow editSessionConflictRow
	err := orm.New(s.db).From("application_edit_sessions").Select("revision", "state").Where("id=?", sessionID).And("owner_id=?", normalizeEditOwner(owner)).First(ctx, &conflictRow)
	if err == sql.ErrNoRows {
		return panelerr.NotFound("application_edit_session")
	}
	if err != nil {
		return err
	}
	current, state := conflictRow.Revision, conflictRow.State
	return panelerr.WithDetails(panelerr.Conflict("edit_session_revision_conflict", "application edit session changed in another client"), map[string]any{"expectedRevision": expected, "currentRevision": current, "state": state})
}

func (s *Service) editSessionPath(sessionID string) string {
	base := filepath.Dir(s.currentConfig().SaveSessionDir)
	return filepath.Join(base, "application-edit-sessions", filepath.Base(strings.TrimSpace(sessionID)))
}

func (s *Service) startEditSessionCleanup() {
	s.editCleanupOnce.Do(func() {
		go func() {
			s.cleanupEditSessions(time.Now().UTC())
			ticker := time.NewTicker(editSessionCleanupPeriod)
			defer ticker.Stop()
			for now := range ticker.C {
				s.cleanupEditSessions(now.UTC())
			}
		}()
	})
}

func (s *Service) cleanupEditSessions(now time.Time) {
	// A committing workspace is owned by its live lease. Once the lease expires,
	// cleanup becomes the recovery worker even if no client performs GET.
	var committing []editSessionCleanupRow
	err := orm.New(s.db).From("application_edit_sessions").Select("id", "owner_id").Where("state=?", EditSessionStateCommitting).WhereGroup(func(c *orm.Condition) {
		c.Or("commit_lease_expires_at IS NULL").Or("commit_lease_expires_at=?", "").Or("commit_lease_expires_at<=?", formatTime(now))
	}).All(context.Background(), &committing)
	if err == nil {
		for _, item := range committing {
			record, loadErr := s.loadEditSession(context.Background(), item.OwnerID, item.ID)
			if loadErr == nil && record.State == EditSessionStateCommitting {
				_, _ = s.recoverEditCommit(context.Background(), record)
			}
		}
	}
	var expiredIDs []string
	err = orm.New(s.db).From("application_edit_sessions").AndNotIn("state", []string{EditSessionStateCommitting, EditSessionStateCommitted, EditSessionStateDiscarded}).WhereGroup(func(c *orm.Condition) {
		c.Or("idle_expires_at<=?", formatTime(now)).Or("absolute_expires_at<=?", formatTime(now))
	}).Pluck(context.Background(), "id", &expiredIDs)
	if err == nil {
		for _, sessionID := range expiredIDs {
			res, err := orm.RawExec(context.Background(), s.db, `UPDATE application_edit_sessions SET state=?,updated_at=? WHERE id=? AND state NOT IN (?,?,?)`, EditSessionStateExpired, formatTime(now), sessionID, EditSessionStateCommitting, EditSessionStateCommitted, EditSessionStateDiscarded)
			if err == nil {
				if affected, _ := res.RowsAffected(); affected == 1 {
					_ = os.RemoveAll(s.editSessionPath(sessionID))
				}
			}
		}
	}
	cutoff := formatTime(now.Add(-24 * time.Hour))
	var oldIDs []string
	err = orm.New(s.db).From("application_edit_sessions").AndIn("state", []string{EditSessionStateCommitted, EditSessionStateDiscarded, EditSessionStateExpired}).And("updated_at<=?", cutoff).Pluck(context.Background(), "id", &oldIDs)
	if err != nil {
		return
	}
	for _, sessionID := range oldIDs {
		_ = orm.New(s.db).From("application_edit_sessions").Where("id=?", sessionID).Delete(context.Background())
		_ = os.RemoveAll(s.editSessionPath(sessionID))
	}
	s.cleanupOrphanedEditSessionBlobs(now)
}

func (s *Service) cleanupOrphanedEditSessionBlobs(now time.Time) {
	root := filepath.Dir(s.editSessionPath("placeholder"))
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		exists, _ := orm.New(s.db).From("application_edit_sessions").Where("id=?", sessionID).Exists(context.Background())
		if !exists {
			dir := filepath.Join(root, sessionID)
			if editSessionWorkspaceStale(dir, now) {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		dir := filepath.Join(root, sessionID)
		files, readErr := os.ReadDir(dir)
		if readErr != nil {
			continue
		}
		for _, file := range files {
			path := filepath.Join(dir, file.Name())
			info, infoErr := file.Info()
			if infoErr != nil || now.Sub(info.ModTime()) < editSessionOrphanStaleAfter {
				continue
			}
			if strings.HasSuffix(file.Name(), ".partial") {
				_ = os.Remove(path)
				continue
			}
			referenced, _ := orm.New(s.db).From("application_edit_session_files").Where("session_id=?", sessionID).And("blob_path=?", path).Exists(context.Background())
			if !referenced {
				_ = os.Remove(path)
			}
		}
	}
}

func editSessionWorkspaceStale(dir string, now time.Time) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	newest := info.ModTime()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			return false
		}
		if entryInfo.ModTime().After(newest) {
			newest = entryInfo.ModTime()
		}
	}
	return now.Sub(newest) >= editSessionOrphanStaleAfter
}

func saveInputFromApplication(app Application) SaveInput {
	return normalizeEditDraft(SaveInput{Name: app.Name, Enabled: app.Enabled, SpecYAML: app.SpecYAML, DeploymentMode: app.DeploymentMode, DeploymentServers: app.DeploymentServers, ReverseProxy: app.ReverseProxy})
}

func normalizeEditDraft(in SaveInput) SaveInput {
	if strings.TrimSpace(in.DeploymentMode) == "" {
		in.DeploymentMode = DeploymentModeAll
	}
	if in.DeploymentServers == nil {
		in.DeploymentServers = []string{}
	}
	if in.ReverseProxy == nil {
		in.ReverseProxy = []ReverseProxyRule{}
	}
	return in
}

func applicationResourceVersion(app Application) ResourceVersion {
	return ResourceVersion{Value: strconv.Itoa(app.Version), UpdatedAt: app.UpdatedAt}
}

func (v ResourceVersion) ValueInt() int {
	value, _ := strconv.Atoi(v.Value)
	return value
}

func hasBlockingDiagnostic(items []Diagnostic) bool {
	for _, item := range items {
		if item.Severity == "error" {
			return true
		}
	}
	return false
}

func normalizeEditOwner(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "admin"
	}
	return owner
}

func formatOptionalEditTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func requireIdempotencyKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "missing"
	}
	return value
}

func editRequestHash(values ...any) string {
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func expectEditMutation(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func panelErrorCode(err error) string {
	var target *panelerr.Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func parseEditTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
