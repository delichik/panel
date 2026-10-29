package applications

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	"panel/internal/platform/filestorage"
	id "panel/internal/platform/identity"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

func (s *Service) dataRoot() string {
	if s == nil {
		return ""
	}
	cfg := s.currentConfig()
	if root := strings.TrimSpace(cfg.DataRoot); root != "" {
		return root
	}
	// SaveSessionDir defaults to {dataRoot}/tmp/application-save-sessions.
	sessionDir := strings.TrimSpace(cfg.SaveSessionDir)
	if sessionDir == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(sessionDir))
}

func (s *Service) applicationFilesRoot() string {
	return filepath.Join(s.dataRoot(), "applications", "files")
}

func (s *Service) applicationFilesDir(appID string) string {
	return filepath.Join(s.applicationFilesRoot(), filepath.Base(strings.TrimSpace(appID)))
}

func (s *Service) applicationFileDir(appID, fileID string) string {
	return filepath.Join(s.applicationFilesDir(appID), filepath.Base(strings.TrimSpace(fileID)))
}

func (s *Service) applicationFileContentDir(appID, fileID string) string {
	return filepath.Join(s.applicationFileDir(appID, fileID), "content")
}

func (s *Service) applicationFileContentPath(appID, fileID, filename string) string {
	name := applicationSafeFilename(filename)
	if name == "" {
		name = "content.bin"
	}
	return filepath.Join(s.applicationFileContentDir(appID, fileID), name)
}

func applicationSafeFilename(value string) string {
	if name := filestorage.SafeFilename(value); name != "" {
		return name
	}
	return ""
}

func applicationFileFilename(file ApplicationFile) string {
	if name := applicationSafeFilename(file.Filename); name != "" {
		return name
	}
	if name := applicationSafeFilename(file.Name); name != "" {
		return name
	}
	return "content.bin"
}

func sha256Hex(content []byte) string {
	return filestorage.SHA256Hex(content)
}

// hydrateApplicationFileContent loads body bytes from the committed content
// layout, legacy BlobPath, or the legacy Content BLOB.
func hydrateApplicationFileContent(s *Service, file *ApplicationFile) error {
	if file == nil {
		return nil
	}
	if raw, err := readApplicationFileBytes(s, *file); err == nil {
		if expected := strings.TrimSpace(file.SHA256); expected != "" && sha256Hex(raw) != expected {
			if len(file.Content) > 0 {
				return nil
			}
			return os.ErrInvalid
		}
		file.Content = raw
		return nil
	} else if len(file.Content) > 0 {
		return nil
	} else {
		return err
	}
}

func readApplicationFileBytes(s *Service, file ApplicationFile) ([]byte, error) {
	if path := strings.TrimSpace(file.BlobPath); path != "" {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return os.ReadFile(path)
		}
		// Legacy absolute path may have pointed at a directory after layout move.
		if raw, err := readSingleContentFile(path); err == nil {
			return raw, nil
		}
		if raw, err := readSingleContentFile(filepath.Join(path, "content")); err == nil {
			return raw, nil
		}
	}
	if s != nil && s.dataRoot() != "" && strings.TrimSpace(file.ApplicationID) != "" && strings.TrimSpace(file.ID) != "" {
		contentDir := s.applicationFileContentDir(file.ApplicationID, file.ID)
		if name := applicationFileFilename(file); name != "" {
			if raw, err := os.ReadFile(filepath.Join(contentDir, name)); err == nil {
				return raw, nil
			}
		}
		if raw, err := readSingleContentFile(contentDir); err == nil {
			return raw, nil
		}
		// Pre-layout flat .blob next to the file id.
		legacyBlob := filepath.Join(s.applicationFilesDir(file.ApplicationID), filepath.Base(file.ID)+".blob")
		if raw, err := os.ReadFile(legacyBlob); err == nil {
			return raw, nil
		}
	}
	if len(file.Content) > 0 {
		return file.Content, nil
	}
	return nil, os.ErrNotExist
}

func readSingleContentFile(dirOrFile string) ([]byte, error) {
	info, err := os.Stat(dirOrFile)
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		return os.ReadFile(dirOrFile)
	}
	entries, err := os.ReadDir(dirOrFile)
	if err != nil {
		return nil, err
	}
	var fileName string
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			continue
		}
		if fileName != "" {
			return nil, os.ErrInvalid
		}
		fileName = entry.Name()
	}
	if fileName == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(dirOrFile, fileName))
}

func fileQueryColumns(includeContent bool) []string {
	if includeContent {
		// content remains for alpha dual-read of rows not yet backfilled.
		return []string{"id", "application_id", "name", "kind", "content_type", "filename", "size", "sha256", "blob_path", "content", "created_at", "updated_at"}
	}
	return []string{"id", "application_id", "name", "kind", "content_type", "filename", "size", "sha256", "blob_path", "created_at", "updated_at"}
}

func (s *Service) replaceApplicationFiles(ctx context.Context, exec orm.Executor, appID string, files []ApplicationFile) (staleDirs []string, err error) {
	var previous []models.ApplicationFile
	if err := orm.New(exec).From("application_files").Select("id", "blob_path", "filename", "name").Where("application_id=?", appID).All(ctx, &previous); err != nil {
		return nil, err
	}
	previousDirs := map[string]string{}
	for _, row := range previous {
		previousDirs[row.ID] = s.applicationFileDir(appID, row.ID)
		if path := strings.TrimSpace(row.BlobPath); path != "" {
			// Legacy flat .blob path — treat parent for cleanup when unused.
			if strings.HasSuffix(path, ".blob") {
				previousDirs[row.ID+".blob"] = path
			}
		}
	}
	keptDirs := map[string]struct{}{}
	written := make([]string, 0, len(files))
	defer func() {
		if err == nil {
			return
		}
		for _, path := range written {
			if _, keep := keptDirs[path]; keep {
				continue
			}
			filestorage.RemoveAllQuiet(path)
		}
	}()

	if err := orm.New(exec).From("application_files").Where("application_id=?", appID).Delete(ctx); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		for id, dir := range previousDirs {
			if strings.HasSuffix(id, ".blob") {
				staleDirs = append(staleDirs, dir)
				continue
			}
			staleDirs = append(staleDirs, dir)
		}
		return staleDirs, nil
	}

	items := make([]*models.ApplicationFile, 0, len(files))
	for _, file := range files {
		item := fromDomainApplicationFile(file)
		item.ApplicationID = appID
		// 会话文件表的 file_key 对新建文件只是应用内 name（跨应用不唯一），
		// 落库前必须为这些新文件分配全局唯一主键；已有全局 ID 的文件（含 legacy 暂存文件）保留原 ID。
		if strings.TrimSpace(item.ID) == "" || item.ID == file.Name {
			item.ID = id.New("afile")
		}
		filename := applicationFileFilename(file)
		item.Filename = filename
		destDir := s.applicationFileDir(appID, item.ID)
		contentPath := s.applicationFileContentPath(appID, item.ID, filename)

		_, contentExists := os.Stat(s.applicationFileContentDir(appID, item.ID))
		switch {
		case strings.TrimSpace(file.BlobDir) != "":
			if err := os.MkdirAll(filepath.Dir(destDir), 0o700); err != nil {
				return nil, err
			}
			if _, statErr := os.Stat(destDir); statErr == nil {
				// Caller should have moved the previous tree aside; remove if still present.
				filestorage.RemoveAllQuiet(destDir)
			}
			if err := os.Rename(file.BlobDir, destDir); err != nil {
				return nil, err
			}
			written = append(written, destDir)
		case contentExists == nil:
			// Commit rename already placed the tree (or source was unchanged).
		case len(file.Content) > 0:
			if err := filestorage.WriteAtomic(contentPath, file.Content, 0o600); err != nil {
				return nil, err
			}
			written = append(written, destDir)
		case strings.TrimSpace(file.BlobPath) != "" && fileMatchesBlob(file.BlobPath, file.SHA256, file.Size):
			if err := filestorage.CopyAtomic(file.BlobPath, contentPath); err != nil {
				return nil, err
			}
			written = append(written, destDir)
		default:
			if raw, readErr := readApplicationFileBytes(s, file); readErr == nil {
				if err := filestorage.WriteAtomic(contentPath, raw, 0o600); err != nil {
					return nil, err
				}
				written = append(written, destDir)
			}
		}
		keptDirs[destDir] = struct{}{}
		item.BlobPath = ""
		item.Content = nil
		if item.SHA256 == "" && len(file.Content) > 0 {
			item.SHA256 = sha256Hex(file.Content)
		}
		if item.Size == 0 && len(file.Content) > 0 {
			item.Size = len(file.Content)
		}
		items = append(items, item)
	}
	if err := orm.New(exec).From("application_files").InsertBatch(ctx, items); err != nil {
		return nil, err
	}
	for id, dir := range previousDirs {
		if _, keep := keptDirs[dir]; keep {
			continue
		}
		_ = id
		staleDirs = append(staleDirs, dir)
	}
	return staleDirs, nil
}

func fileMatchesBlob(path, wantSHA string, wantSize int64) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if wantSize > 0 && info.Size() != wantSize {
		return false
	}
	if strings.TrimSpace(wantSHA) == "" {
		return true
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return sha256Hex(raw) == wantSHA
}

func (s *Service) removeApplicationFileBlobs(appID string) {
	appID = strings.TrimSpace(appID)
	if s == nil || appID == "" || s.dataRoot() == "" {
		return
	}
	filestorage.RemoveAllQuiet(s.applicationFilesDir(appID))
}

// BackfillApplicationFileBlobs moves legacy application_files.content BLOBs and
// flat {fileId}.blob files into {fileId}/content/{filename}, clears absolute
// blob_path, and empties the Content column. Safe to call repeatedly.
func (s *Service) BackfillApplicationFileBlobs(ctx context.Context) error {
	if s == nil || s.db == nil || s.dataRoot() == "" {
		return nil
	}
	var rows []models.ApplicationFile
	if err := orm.New(s.db).From("application_files").
		Select("id", "application_id", "name", "filename", "size", "sha256", "blob_path", "content").
		All(ctx, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		filename := strings.TrimSpace(row.Filename)
		if filename == "" {
			filename = applicationSafeFilename(row.Name)
		}
		if filename == "" {
			filename = "content.bin"
		}
		destDir := s.applicationFileDir(row.ApplicationID, row.ID)
		contentPath := s.applicationFileContentPath(row.ApplicationID, row.ID, filename)
		contentDir := filepath.Dir(contentPath)

		needsWrite := false
		if _, err := os.Stat(contentPath); err != nil {
			needsWrite = true
		}
		var body []byte
		if needsWrite {
			switch {
			case len(row.Content) > 0:
				body = row.Content
			case strings.TrimSpace(row.BlobPath) != "":
				path := strings.TrimSpace(row.BlobPath)
				if raw, err := os.ReadFile(path); err == nil {
					body = raw
				} else if raw, err := readSingleContentFile(path); err == nil {
					body = raw
				} else if raw, err := readSingleContentFile(filepath.Join(path, "content")); err == nil {
					body = raw
					needsWrite = false // already in content layout under old absolute path
				}
			default:
				legacyBlob := filepath.Join(s.applicationFilesDir(row.ApplicationID), row.ID+".blob")
				if raw, err := os.ReadFile(legacyBlob); err == nil {
					body = raw
				}
			}
			if needsWrite && len(body) == 0 && row.Size == 0 && strings.TrimSpace(row.BlobPath) == "" {
				// Empty placeholder — still ensure filename metadata.
				if _, err := orm.RawExec(ctx, s.db, `UPDATE application_files SET filename=CASE WHEN TRIM(COALESCE(filename,''))='' THEN ? ELSE filename END WHERE id=? AND application_id=?`,
					filename, row.ID, row.ApplicationID); err != nil {
					logging.From(logging.With(ctx, logging.Context{Module: "applications"})).Warn("application file filename backfill failed",
						zap.String("application_id", row.ApplicationID), zap.String("file_id", row.ID), zap.Error(err))
				}
				continue
			}
			if needsWrite {
				if err := os.MkdirAll(contentDir, 0o700); err != nil {
					logging.From(logging.With(ctx, logging.Context{Module: "applications"})).Warn("application file content dir backfill failed",
						zap.String("application_id", row.ApplicationID), zap.String("file_id", row.ID), zap.Error(err))
					continue
				}
				if err := filestorage.WriteAtomic(contentPath, body, 0o600); err != nil {
					logging.From(logging.With(ctx, logging.Context{Module: "applications"})).Warn("application file content backfill write failed",
						zap.String("application_id", row.ApplicationID), zap.String("file_id", row.ID), zap.Error(err))
					continue
				}
			}
		}

		sum := row.SHA256
		if sum == "" && len(body) > 0 {
			sum = sha256Hex(body)
		}
		if _, err := orm.RawExec(ctx, s.db, `UPDATE application_files SET filename=?, blob_path='', sha256=CASE WHEN TRIM(COALESCE(sha256,''))='' THEN ? ELSE sha256 END, content=NULL WHERE id=? AND application_id=?`,
			filename, sum, row.ID, row.ApplicationID); err != nil {
			logging.From(logging.With(ctx, logging.Context{Module: "applications"})).Warn("application file content backfill update failed",
				zap.String("application_id", row.ApplicationID), zap.String("file_id", row.ID), zap.Error(err))
			continue
		}

		// Remove legacy flat .blob once content layout exists.
		legacyBlob := filepath.Join(s.applicationFilesDir(row.ApplicationID), row.ID+".blob")
		if legacyBlob != contentPath {
			filestorage.RemoveQuiet(legacyBlob)
		}
		if old := strings.TrimSpace(row.BlobPath); old != "" && old != contentPath && !strings.HasPrefix(old, destDir+string(filepath.Separator)) && old != destDir {
			if strings.HasSuffix(old, ".blob") {
				filestorage.RemoveQuiet(old)
			}
		}
		_ = destDir
	}
	return nil
}
