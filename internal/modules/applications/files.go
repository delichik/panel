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

func (s *Service) GetFile(ctx context.Context, appID, fileID string) (ApplicationFile, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return ApplicationFile{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at FROM application_files WHERE application_id=? AND id=?`, appID, fileID)
	file, err := scanApplicationFile(row, true)
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
	targetPath, err := normalizeApplicationFilePath(in.Path)
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
		Path:          targetPath,
		Kind:          kind,
		ContentType:   strings.TrimSpace(in.ContentType),
		Size:          int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		Content:       content,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO application_files(id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(application_id,path) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,content=excluded.content,updated_at=excluded.updated_at`,
		file.ID, file.ApplicationID, file.Path, file.Kind, file.ContentType, file.Size, file.SHA256, file.Content, formatTime(file.CreatedAt), formatTime(file.UpdatedAt))
	if err != nil {
		return ApplicationFile{}, err
	}
	if err := s.redeployIfEnabled(ctx, app); err != nil {
		return ApplicationFile{}, err
	}
	return s.getFileByPath(ctx, appID, targetPath, false)
}

func (s *Service) DeleteFile(ctx context.Context, appID, fileID string) error {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM application_files WHERE application_id=? AND id=?`, appID, fileID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return panelerr.NotFound("application_file")
	}
	return s.redeployIfEnabled(ctx, app)
}

func (s *Service) listFiles(ctx context.Context, appID string, includeContent bool) ([]ApplicationFile, error) {
	columns := `id,application_id,path,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? ORDER BY path ASC`, appID)
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

func (s *Service) getFileByPath(ctx context.Context, appID, filePath string, includeContent bool) (ApplicationFile, error) {
	columns := `id,application_id,path,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? AND path=?`, appID, filePath)
	file, err := scanApplicationFile(row, includeContent)
	if err == sql.ErrNoRows {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	return file, err
}

func scanApplicationFile(row appScanner, includeContent bool) (ApplicationFile, error) {
	var file ApplicationFile
	var createdAt, updatedAt string
	if includeContent {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Path, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &file.Content, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	} else {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Path, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	}
	file.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	file.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
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
	filesByPath := map[string]ApplicationFile{}
	for _, file := range files {
		filesByPath[file.Path] = file
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
		rel, err := normalizeApplicationWorkspacePath(mount.Source)
		if err != nil {
			return appruntime.Spec{}, err
		}
		file, ok := filesByPath[rel]
		if !ok {
			return appruntime.Spec{}, panelerr.Validation("application_file_mount_missing", "mounted application file "+rel+" does not exist")
		}
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
