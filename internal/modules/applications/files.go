package applications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
	appspec "panel/internal/modules/applications/spec"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

func (s *Service) ListFiles(ctx context.Context, appID string) ([]ApplicationFile, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return nil, err
	}
	return s.listFiles(ctx, appID, false)
}

func (s *Service) GetFile(ctx context.Context, appID, fileRef string) (ApplicationFile, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return ApplicationFile{}, err
	}
	file, err := s.getFileByRef(ctx, appID, fileRef, true)
	if err == sql.ErrNoRows {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	return file, err
}

func (s *Service) SaveFile(ctx context.Context, appID string, in FileSaveInput) (ApplicationFile, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return ApplicationFile{}, err
	}
	name, err := normalizeApplicationFileName(firstNonEmpty(in.Name, in.Path))
	if err != nil {
		return ApplicationFile{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != ApplicationFileKindBinary && kind != ApplicationFileKindTemplate {
		return ApplicationFile{}, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
	if err != nil {
		return ApplicationFile{}, panelerr.Validation("application_file_content_invalid", "file content must be base64 encoded")
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	file := ApplicationFile{
		ID:            id.New("afile"),
		ApplicationID: appID,
		Name:          name,
		Kind:          kind,
		ContentType:   strings.TrimSpace(in.ContentType),
		Size:          int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		Content:       content,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if existing, existingErr := s.getFileByName(ctx, appID, name, false); existingErr == nil && existing.Kind == file.Kind && existing.ContentType == file.ContentType && existing.Size == file.Size && existing.SHA256 == file.SHA256 {
		return existing, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplicationFile{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO application_files(id,application_id,name,kind,content_type,size,sha256,content,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(application_id,name) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,content=excluded.content,updated_at=excluded.updated_at`,
		file.ID, file.ApplicationID, file.Name, file.Kind, file.ContentType, file.Size, file.SHA256, file.Content, formatTime(file.CreatedAt), formatTime(file.UpdatedAt))
	if err != nil {
		return ApplicationFile{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE applications SET version=version+1,updated_at=? WHERE id=? AND version=?`, formatTime(now), appID, app.Version)
	if err != nil {
		return ApplicationFile{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ApplicationFile{}, resourceVersionConflict(app.Version, app.Version+1)
	}
	if err := tx.Commit(); err != nil {
		return ApplicationFile{}, err
	}
	if err := s.redeployIfEnabled(ctx, app); err != nil {
		return ApplicationFile{}, err
	}
	return s.getFileByName(ctx, appID, name, false)
}

func (s *Service) DeleteFile(ctx context.Context, appID, fileRef string) error {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	file, err := s.getFileByRef(ctx, appID, fileRef, false)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM application_files WHERE application_id=? AND id=?`, appID, file.ID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return panelerr.NotFound("application_file")
	}
	now := time.Now().UTC()
	res, err = tx.ExecContext(ctx, `UPDATE applications SET version=version+1,updated_at=? WHERE id=? AND version=?`, formatTime(now), appID, app.Version)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return resourceVersionConflict(app.Version, app.Version+1)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.redeployIfEnabled(ctx, app)
}

func (s *Service) listFiles(ctx context.Context, appID string, includeContent bool) ([]ApplicationFile, error) {
	columns := `id,application_id,name,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,name,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? ORDER BY name ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicationFile{}
	for rows.Next() {
		file, err := scanApplicationFile(rows, includeContent)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Service) getFileByName(ctx context.Context, appID, name string, includeContent bool) (ApplicationFile, error) {
	columns := `id,application_id,name,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,name,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? AND name=?`, appID, name)
	file, err := scanApplicationFile(row, includeContent)
	if err == sql.ErrNoRows {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	return file, err
}

func (s *Service) getFileByRef(ctx context.Context, appID, ref string, includeContent bool) (ApplicationFile, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	file, err := s.getFileByName(ctx, appID, ref, includeContent)
	if err == nil {
		return file, nil
	}
	columns := `id,application_id,name,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,name,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? AND id=?`, appID, ref)
	return scanApplicationFile(row, includeContent)
}

func scanApplicationFile(row appScanner, includeContent bool) (ApplicationFile, error) {
	var file ApplicationFile
	var createdAt, updatedAt string
	if includeContent {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Name, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &file.Content, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	} else {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Name, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	}
	file.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	file.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	file.Path = file.Name
	if includeContent {
		file.ContentBase64 = base64.StdEncoding.EncodeToString(file.Content)
	}
	return file, nil
}

func (s *Service) attachFiles(ctx context.Context, job appruntime.Spec, spec appspec.Spec, files []ApplicationFile, data map[string]any) (appruntime.Spec, error) {
	fileMounts := applicationFileMounts(spec.Mounts)
	if len(fileMounts) == 0 {
		return job, nil
	}
	filesByName := map[string]ApplicationFile{}
	for _, file := range files {
		filesByName[file.Name] = file
	}
	mounts := make([]appruntime.Mount, 0, len(job.Mounts)+len(fileMounts))
	for _, mount := range job.Mounts {
		if mount.Type != "managed_file" {
			mounts = append(mounts, mount)
		}
	}
	managed := append([]appruntime.ManagedFile(nil), job.Files...)
	for _, mount := range fileMounts {
		if strings.TrimSpace(mount.Type) == "panel_file" {
			if s.internalFiles == nil {
				return appruntime.Spec{}, panelerr.Validation("panel_file_provider_unavailable", "Internal file provider is unavailable")
			}
			reader, info, err := s.internalFiles.OpenInternalFile(ctx, mount.Source)
			if err != nil {
				return appruntime.Spec{}, err
			}
			content, err := io.ReadAll(reader)
			closeErr := reader.Close()
			if err != nil {
				return appruntime.Spec{}, err
			}
			if closeErr != nil {
				return appruntime.Spec{}, closeErr
			}
			rel := panelFileAllocationName(mount.Source)
			mode := strings.TrimSpace(info.Mode)
			if mode == "" {
				mode = panelFilePerms(mount.Source)
			}
			managed = append(managed, appruntime.ManagedFile{Path: rel, Content: content, Mode: mode, UID: cloneInt(mount.UID), GID: cloneInt(mount.GID)})
			mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: rel, Target: mount.Target, ReadOnly: mount.ReadOnly})
			continue
		}
		name, err := normalizeApplicationFileName(mount.Source)
		if err != nil {
			return appruntime.Spec{}, err
		}
		file, ok := filesByName[name]
		if !ok {
			return appruntime.Spec{}, panelerr.Validation("application_file_mount_missing", "mounted application file "+name+" does not exist")
		}
		rel := applicationFileAllocationName(file.ID)
		if file.Kind == ApplicationFileKindArchive {
			managed = append(managed, appruntime.ManagedFile{Kind: appruntime.ManagedFileKindArchive, Path: rel, Content: file.Content, UID: cloneInt(mount.UID), GID: cloneInt(mount.GID)})
			mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: rel, Target: mount.Target, ReadOnly: mount.ReadOnly})
			continue
		}
		rendered := file.Content
		if file.Kind == ApplicationFileKindTemplate {
			text := string(file.Content)
			if s.renderer != nil {
				text, err = s.renderer.Render(ctx, text, data)
				if err != nil {
					return appruntime.Spec{}, err
				}
			}
			rendered = []byte(text)
		}
		mode := strings.TrimSpace(mount.Mode)
		if mode == "" {
			mode = "0644"
		}
		managed = append(managed, appruntime.ManagedFile{Path: rel, Content: rendered, Mode: mode, UID: cloneInt(mount.UID), GID: cloneInt(mount.GID)})
		mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: rel, Target: mount.Target, ReadOnly: mount.ReadOnly})
	}
	job.Mounts = mounts
	job.Files = managed
	return job, nil
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func applicationFileAllocationName(fileID string) string {
	name := sanitizeRuntimeName(strings.TrimSpace(fileID))
	if name == "" {
		name = "file"
	}
	return "application-files/" + name
}
