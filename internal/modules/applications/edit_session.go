package applications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
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
	draft := SaveInput{Variables: map[string]string{}, DeploymentMode: DeploymentModeAll, DeploymentServers: []string{}, ReverseProxy: []ReverseProxyRule{}}
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
	_, err = tx.ExecContext(ctx, `INSERT INTO application_edit_sessions(id,application_id,owner_id,client_draft_key,state,base_resource_version,base_resource_updated_at,draft_json,revision,idle_expires_at,absolute_expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
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
			if _, err := tx.ExecContext(ctx, `INSERT INTO application_edit_session_files(session_id,file_key,name,kind,content_type,size,sha256,blob_path,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
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

func (s *Service) RecoverableEditSessions(ctx context.Context, owner, applicationID, clientDraftKey string) ([]ApplicationEditSession, error) {
	owner = normalizeEditOwner(owner)
	now := formatTime(time.Now().UTC())
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM application_edit_sessions WHERE owner_id=? AND application_id=? AND client_draft_key=? AND state IN (?,?,?) AND idle_expires_at>? AND absolute_expires_at>? ORDER BY updated_at DESC`,
		owner, strings.TrimSpace(applicationID), strings.TrimSpace(clientDraftKey), EditSessionStateActive, EditSessionStateConflict, EditSessionStateCommitting, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicationEditSession{}
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		session, err := s.GetEditSession(ctx, owner, sessionID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		out = append(out, session)
	}
	return out, rows.Err()
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
	res, err := s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET draft_json=?,revision=revision+1,preview_token='',preview_revision=0,preview_expires_at='',state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND absolute_expires_at>?`,
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
	var result EditSessionFileContent
	var blobPath string
	err = s.db.QueryRowContext(ctx, `SELECT file_key,name,kind,content_type,size,sha256,blob_path FROM application_edit_session_files WHERE session_id=? AND file_key=? AND state='ready'`, record.ID, strings.TrimSpace(fileKey)).Scan(
		&result.FileKey, &result.Name, &result.Kind, &result.ContentType, &result.Size, &result.SHA256, &blobPath,
	)
	if err == sql.ErrNoRows {
		return EditSessionFileContent{}, panelerr.NotFound("application_edit_session_file")
	}
	if err != nil {
		return EditSessionFileContent{}, err
	}
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
// that key, but the fallback keeps pre-name sessions recoverable.
func (s *Service) resolveEditSessionFileKey(ctx context.Context, sessionID, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	var fileKey string
	err := s.db.QueryRowContext(ctx, `SELECT file_key FROM application_edit_session_files WHERE session_id=? AND name=? AND state='ready'`, strings.TrimSpace(sessionID), ref).Scan(&fileKey)
	if err == sql.ErrNoRows {
		err = s.db.QueryRowContext(ctx, `SELECT file_key FROM application_edit_session_files WHERE session_id=? AND file_key=? AND state='ready'`, strings.TrimSpace(sessionID), ref).Scan(&fileKey)
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
	_ = tx.QueryRowContext(ctx, `SELECT blob_path FROM application_edit_session_files WHERE session_id=? AND file_key=?`, sessionID, fileKey).Scan(&blobPath)
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_edit_session_files WHERE session_id=? AND file_key=?`, sessionID, fileKey); err != nil {
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
	_, _ = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state=?`,
		formatTime(now), formatTime(now.Add(editSessionIdleTTL)), record.ID, record.OwnerID, record.Revision, EditSessionStateActive)
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
	diagnostics = append(diagnostics, Diagnostic{Code: "application_cross_module_insights_unavailable", Severity: "info", Message: "Cross-module DNS, certificate, and gateway insights are not available in this API revision"})
	res, err := s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET preview_token=?,preview_revision=?,preview_expires_at=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state=?`,
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
	res, err := s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,commit_lease_owner=?,commit_lease_expires_at=?,commit_idempotency_key=?,commit_application_id=?,updated_at=? WHERE id=? AND owner_id=? AND revision=? AND state=?`,
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
			_, _ = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,conflict_json=?,commit_lease_owner='',commit_lease_expires_at='',updated_at=? WHERE id=? AND owner_id=? AND commit_lease_owner=?`, EditSessionStateConflict, `{"code":"resource_version_conflict"}`, formatTime(time.Now().UTC()), sessionID, owner, leaseOwner)
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
		_, _ = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,conflict_json=?,commit_lease_owner='',commit_lease_expires_at='',updated_at=? WHERE id=? AND owner_id=? AND commit_lease_owner=?`, nextState, conflictJSON, formatTime(time.Now().UTC()), sessionID, owner, leaseOwner)
		return EditCommitResult{}, err
	}
	result := EditCommitResult{Application: app, ResourceVersion: applicationResourceVersion(app), ApplyRequested: record.Draft.Enabled, Diagnostics: []Diagnostic{{Code: "application_apply_operation_reference_unavailable", Severity: "info", Message: "Configuration was committed and reconciliation was requested; an operation reference is not available from the current coordinator"}}}
	resultRaw, _ := json.Marshal(result)
	committedAt := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,commit_result_json=?,commit_lease_owner='',commit_lease_expires_at='',committed_at=?,updated_at=? WHERE id=? AND owner_id=? AND commit_lease_owner=?`,
		EditSessionStateCommitted, string(resultRaw), formatTime(committedAt), formatTime(committedAt), sessionID, owner, leaseOwner)
	return result, err
}

func (s *Service) DiscardEditSession(ctx context.Context, owner, sessionID string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND state IN (?,?)`,
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
	_ = tx.QueryRowContext(ctx, `SELECT blob_path FROM application_edit_session_files WHERE session_id=? AND file_key=?`, sessionID, fileKey).Scan(&previousBlobPath)
	if err := s.bumpEditRevision(ctx, tx, owner, sessionID, revision); err != nil {
		return ApplicationEditSession{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_edit_session_files(session_id,file_key,name,kind,content_type,size,sha256,blob_path,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(session_id,file_key) DO UPDATE SET name=excluded.name,kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,blob_path=excluded.blob_path,state='ready',updated_at=excluded.updated_at`,
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
	res, err := tx.ExecContext(ctx, `UPDATE application_edit_sessions SET revision=revision+1,preview_token='',preview_revision=0,preview_expires_at='',state=?,updated_at=?,idle_expires_at=? WHERE id=? AND owner_id=? AND revision=? AND state IN (?,?) AND absolute_expires_at>?`,
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
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,owner_id,client_draft_key,state,base_resource_version,base_resource_updated_at,draft_json,revision,preview_token,preview_revision,preview_expires_at,commit_lease_owner,commit_lease_expires_at,commit_idempotency_key,commit_application_id,commit_result_json,idle_expires_at,absolute_expires_at,created_at,updated_at,committed_at FROM application_edit_sessions WHERE id=? AND owner_id=?`, strings.TrimSpace(sessionID), owner)
	var record editSessionRecord
	var baseUpdatedAt, draftRaw, previewValue, previewExpires, leaseExpires, resultRaw string
	var idleExpires, absoluteExpires, createdAt, updatedAt, committedAt string
	var baseVersion int
	if err := row.Scan(&record.ID, &record.ApplicationID, &record.OwnerID, &record.ClientDraftKey, &record.State, &baseVersion, &baseUpdatedAt, &draftRaw, &record.Revision, &previewValue, &record.PreviewRevision, &previewExpires, &record.CommitLeaseOwner, &leaseExpires, &record.CommitKey, &record.CommitApplication, &resultRaw, &idleExpires, &absoluteExpires, &createdAt, &updatedAt, &committedAt); err != nil {
		if err == sql.ErrNoRows {
			return editSessionRecord{}, panelerr.NotFound("application_edit_session")
		}
		return editSessionRecord{}, err
	}
	_ = json.Unmarshal([]byte(draftRaw), &record.Draft)
	record.Draft = normalizeEditDraft(record.Draft)
	record.IdleExpiresAt = parseEditTime(idleExpires)
	record.AbsoluteExpiresAt = parseEditTime(absoluteExpires)
	record.CreatedAt = parseEditTime(createdAt)
	record.UpdatedAt = parseEditTime(updatedAt)
	record.BaseResourceVersion = ResourceVersion{Value: strconv.Itoa(baseVersion), UpdatedAt: parseEditTime(baseUpdatedAt)}
	if previewValue != "" {
		record.PreviewToken = &PreviewToken{Value: previewValue, Action: "application.commit", SubjectVersion: strconv.Itoa(baseVersion)}
	}
	record.PreviewExpiresAt = parseEditTime(previewExpires)
	record.CommitLeaseExpires = parseEditTime(leaseExpires)
	if committedAt != "" {
		value := parseEditTime(committedAt)
		record.CommittedAt = &value
	}
	if resultRaw != "" {
		var result EditCommitResult
		if json.Unmarshal([]byte(resultRaw), &result) == nil {
			record.CommitResult = &result
		}
	}
	now := time.Now().UTC()
	if record.State != EditSessionStateCommitting && record.State != EditSessionStateCommitted && record.State != EditSessionStateDiscarded && record.State != EditSessionStateExpired && (now.After(record.IdleExpiresAt) || now.After(record.AbsoluteExpiresAt)) {
		_, _ = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,updated_at=? WHERE id=? AND owner_id=?`, EditSessionStateExpired, formatTime(now), record.ID, owner)
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
	rows, err := s.db.QueryContext(ctx, `SELECT file_key,name,kind,content_type,size,sha256,created_at,updated_at FROM application_edit_session_files WHERE session_id=? AND state='ready' ORDER BY name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EditSessionFile{}
	for rows.Next() {
		var item EditSessionFile
		var createdAt, updatedAt string
		if err := rows.Scan(&item.FileKey, &item.Name, &item.Kind, &item.ContentType, &item.Size, &item.SHA256, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt, item.UpdatedAt = parseEditTime(createdAt), parseEditTime(updatedAt)
		item.Path = item.Name
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) editSessionApplicationFiles(ctx context.Context, sessionID, applicationID string) ([]ApplicationFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_key,name,kind,content_type,size,sha256,blob_path,created_at,updated_at FROM application_edit_session_files WHERE session_id=? AND state='ready' ORDER BY name`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []ApplicationFile{}
	for rows.Next() {
		var file ApplicationFile
		var blobPath, createdAt, updatedAt string
		if err := rows.Scan(&file.ID, &file.Name, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &blobPath, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		file.ApplicationID = applicationID
		file.Path = file.Name
		file.Content, err = readEditSessionBlob(file.ID, blobPath, file.Size, file.SHA256)
		if err != nil {
			return nil, err
		}
		file.CreatedAt, file.UpdatedAt = parseEditTime(createdAt), parseEditTime(updatedAt)
		files = append(files, file)
	}
	return files, rows.Err()
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
		diagnostics := []Diagnostic{{Code: pe.Code, Severity: "error", Message: pe.Message}}
		if issues, ok := pe.Details["issues"].([]ValidationIssue); ok {
			diagnostics = diagnostics[:0]
			for _, issue := range issues {
				diagnostics = append(diagnostics, Diagnostic{Code: pe.Code, Severity: "error", Field: issue.Field, Message: issue.Message})
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
		_, _ = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,conflict_json=?,commit_lease_owner='',commit_lease_expires_at='',updated_at=? WHERE id=? AND owner_id=? AND state=?`, nextState, conflictJSON, formatTime(time.Now().UTC()), record.ID, record.OwnerID, EditSessionStateCommitting)
		return ApplicationEditSession{}, false
	}
	result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "application_commit_recovered", Severity: "info", Message: "The committed application was recovered after an interrupted response"})
	raw, _ := json.Marshal(result)
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,commit_result_json=?,commit_lease_owner='',commit_lease_expires_at='',committed_at=?,updated_at=? WHERE id=? AND owner_id=? AND state=?`, EditSessionStateCommitted, string(raw), formatTime(now), formatTime(now), record.ID, record.OwnerID, EditSessionStateCommitting)
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
	res, err := s.db.ExecContext(ctx, `UPDATE application_edit_sessions SET state=?,commit_result_json=?,commit_lease_owner='',commit_lease_expires_at='',committed_at=?,updated_at=? WHERE id=? AND owner_id=? AND state=?`, EditSessionStateCommitted, string(raw), formatTime(now), formatTime(now), record.ID, record.OwnerID, EditSessionStateCommitting)
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
		diagnostics = append(diagnostics, Diagnostic{Code: "application_apply_request_failed", Severity: "warning", Message: "Configuration was committed, but applying it could not be requested", Details: map[string]any{"error": applyErr.Error()}})
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
		Variables         map[string]string
		DeploymentMode    string
		DeploymentServers []string
		ReverseProxy      []ReverseProxyRule
	}{wantName, draft.Enabled, draft.SpecYAML, prepared.variables, prepared.deploymentMode, deploymentServers, reverseProxy}
	got := struct {
		Name              string
		Enabled           bool
		SpecYAML          string
		Variables         map[string]string
		DeploymentMode    string
		DeploymentServers []string
		ReverseProxy      []ReverseProxyRule
	}{app.Name, app.Enabled, app.SpecYAML, app.Variables, app.DeploymentMode, app.DeploymentServers, app.ReverseProxy}
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
	var storedHash, resultRaw string
	err := s.db.QueryRowContext(ctx, `SELECT request_hash,result_json FROM application_edit_session_operations WHERE session_id=? AND (client_operation_id=? OR idempotency_key=?)`, sessionID, operationID, key).Scan(&storedHash, &resultRaw)
	if err == sql.ErrNoRows {
		return ApplicationEditSession{}, false, nil
	}
	if err != nil {
		return ApplicationEditSession{}, false, err
	}
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
	_, err := tx.ExecContext(ctx, `INSERT INTO application_edit_session_operations(session_id,client_operation_id,idempotency_key,request_hash,result_json,created_at) VALUES(?,?,?,?,?,?)`, sessionID, operationID, idempotencyKey, requestHash, "", formatTime(time.Now().UTC()))
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
	_, err = s.db.ExecContext(ctx, `UPDATE application_edit_session_operations SET result_json=? WHERE session_id=? AND client_operation_id=?`, string(raw), sessionID, operationID)
	return err
}

func (s *Service) editMutationConflict(ctx context.Context, owner, sessionID string, expected int) error {
	var current int
	var state string
	err := s.db.QueryRowContext(ctx, `SELECT revision,state FROM application_edit_sessions WHERE id=? AND owner_id=?`, sessionID, normalizeEditOwner(owner)).Scan(&current, &state)
	if err == sql.ErrNoRows {
		return panelerr.NotFound("application_edit_session")
	}
	if err != nil {
		return err
	}
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
	rows, err := s.db.Query(`SELECT id,owner_id FROM application_edit_sessions WHERE state=? AND (commit_lease_expires_at='' OR commit_lease_expires_at<=?)`, EditSessionStateCommitting, formatTime(now))
	if err == nil {
		items := [][2]string{}
		for rows.Next() {
			var item [2]string
			if rows.Scan(&item[0], &item[1]) == nil {
				items = append(items, item)
			}
		}
		rows.Close()
		for _, item := range items {
			record, loadErr := s.loadEditSession(context.Background(), item[1], item[0])
			if loadErr == nil && record.State == EditSessionStateCommitting {
				_, _ = s.recoverEditCommit(context.Background(), record)
			}
		}
	}
	rows, err = s.db.Query(`SELECT id FROM application_edit_sessions WHERE state NOT IN (?,?,?) AND (idle_expires_at<=? OR absolute_expires_at<=?)`, EditSessionStateCommitting, EditSessionStateCommitted, EditSessionStateDiscarded, formatTime(now), formatTime(now))
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
			res, err := s.db.Exec(`UPDATE application_edit_sessions SET state=?,updated_at=? WHERE id=? AND state NOT IN (?,?,?)`, EditSessionStateExpired, formatTime(now), sessionID, EditSessionStateCommitting, EditSessionStateCommitted, EditSessionStateDiscarded)
			if err == nil {
				if affected, _ := res.RowsAffected(); affected == 1 {
					_ = os.RemoveAll(s.editSessionPath(sessionID))
				}
			}
		}
	}
	cutoff := formatTime(now.Add(-24 * time.Hour))
	rows, err = s.db.Query(`SELECT id FROM application_edit_sessions WHERE state IN (?,?,?) AND updated_at<=?`, EditSessionStateCommitted, EditSessionStateDiscarded, EditSessionStateExpired, cutoff)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var sessionID string
		if rows.Scan(&sessionID) == nil {
			_, _ = s.db.Exec(`DELETE FROM application_edit_sessions WHERE id=?`, sessionID)
			_ = os.RemoveAll(s.editSessionPath(sessionID))
		}
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
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM application_edit_sessions WHERE id=?`, sessionID).Scan(&exists); err == sql.ErrNoRows {
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
			var referenced int
			err := s.db.QueryRow(`SELECT 1 FROM application_edit_session_files WHERE session_id=? AND blob_path=?`, sessionID, path).Scan(&referenced)
			if err == sql.ErrNoRows {
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
	return normalizeEditDraft(SaveInput{Name: app.Name, Enabled: app.Enabled, SpecYAML: app.SpecYAML, Variables: app.Variables, DeploymentMode: app.DeploymentMode, DeploymentServers: app.DeploymentServers, ReverseProxy: app.ReverseProxy})
}

func normalizeEditDraft(in SaveInput) SaveInput {
	if in.Variables == nil {
		in.Variables = map[string]string{}
	}
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

func formatOptionalEditTime(value time.Time) string {
	if value.IsZero() {
		return ""
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

func editSessionError(message string, args ...any) error {
	return panelerr.Conflict("application_edit_session_error", fmt.Sprintf(message, args...))
}
