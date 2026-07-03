package applications

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type saveSession struct {
	ID            string
	ApplicationID string
	Input         SaveInput
	Dir           string
	Files         map[string]*stagedFile
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
}

type stagedFile struct {
	ID            string
	ApplicationID string
	Path          string
	Kind          string
	ContentType   string
	Size          int64
	SHA256        string
	DiskPath      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Service) BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error) {
	if in.ApplicationID != "" {
		if _, err := s.Get(ctx, in.ApplicationID); err != nil {
			return SaveSessionResult{}, err
		}
	}
	sessionID := id.New("asave")
	now := time.Now().UTC()
	session := &saveSession{
		ID:            sessionID,
		ApplicationID: in.ApplicationID,
		Input:         in.Save,
		Dir:           filepath.Join(s.config.SaveSessionDir, sessionID),
		Files:         map[string]*stagedFile{},
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     now.Add(30 * time.Minute),
	}
	if err := os.MkdirAll(session.Dir, 0o700); err != nil {
		return SaveSessionResult{}, err
	}
	if in.ApplicationID != "" {
		files, err := s.listFiles(ctx, in.ApplicationID, true)
		if err != nil {
			return SaveSessionResult{}, err
		}
		for _, file := range files {
			staged, err := s.stageFileBytes(session, file.Path, file.Kind, file.ContentType, file.Content, file.CreatedAt)
			if err != nil {
				return SaveSessionResult{}, err
			}
			staged.ID = file.ID
			staged.ApplicationID = file.ApplicationID
			staged.CreatedAt = file.CreatedAt
			staged.UpdatedAt = file.UpdatedAt
		}
	}
	s.sessionMu.Lock()
	s.saveSessions[session.ID] = session
	s.sessionMu.Unlock()
	return session.result(), nil
}

func (s *Service) UploadSaveSessionFile(ctx context.Context, sessionID string, in FileSaveInput) (ApplicationFile, error) {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return ApplicationFile{}, err
	}
	content, err := decodeApplicationFileInput(in)
	if err != nil {
		return ApplicationFile{}, err
	}
	staged, err := s.stageFileBytes(session, in.Path, in.Kind, in.ContentType, content, time.Now().UTC())
	if err != nil {
		return ApplicationFile{}, err
	}
	_ = ctx
	return staged.applicationFile(nil), nil
}

func (s *Service) UploadSaveSessionArchive(ctx context.Context, sessionID string, in FileArchiveInput) ([]ApplicationFile, error) {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != "binary" && kind != "template" {
		return nil, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	if len(in.Content) == 0 {
		return nil, panelerr.Validation("application_file_content_invalid", "file content is required")
	}
	basePath, err := normalizeApplicationArchiveBasePath(in.BasePath)
	if err != nil {
		return nil, err
	}
	items, err := extractApplicationFileArchive(bytes.NewReader(in.Content), int64(len(in.Content)), in.FileName)
	if err != nil {
		return nil, err
	}
	files := make([]ApplicationFile, 0, len(items))
	for _, item := range items {
		targetPath := item.Name
		if basePath != "" {
			targetPath = path.Join(basePath, item.Name)
		}
		staged, err := s.stageFileBytes(session, targetPath, kind, "", item.Content, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		files = append(files, staged.applicationFile(nil))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	_ = ctx
	return files, nil
}

func (s *Service) DeleteSaveSessionFile(ctx context.Context, sessionID string, in FileDeleteInput) error {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return err
	}
	targetPath, err := normalizeApplicationFilePath(in.Path)
	if err != nil {
		return err
	}
	s.sessionMu.Lock()
	staged := session.Files[targetPath]
	if staged != nil {
		delete(session.Files, targetPath)
		session.UpdatedAt = time.Now().UTC()
	}
	s.sessionMu.Unlock()
	if staged != nil {
		_ = os.Remove(staged.DiskPath)
	}
	_ = ctx
	return nil
}

func (s *Service) CommitSaveSession(ctx context.Context, sessionID string) (Application, error) {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return Application{}, err
	}
	files, err := session.applicationFiles()
	if err != nil {
		return Application{}, err
	}
	var app Application
	if session.ApplicationID == "" {
		app, err = s.createWithFiles(ctx, session.Input, files)
	} else {
		app, err = s.updateWithFiles(ctx, session.ApplicationID, session.Input, files)
	}
	if err != nil {
		return Application{}, err
	}
	s.discardSaveSession(sessionID)
	return app, nil
}

func decodeApplicationFileInput(in FileSaveInput) ([]byte, error) {
	if _, err := normalizeApplicationFilePath(in.Path); err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != "binary" && kind != "template" {
		return nil, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
	if err != nil {
		return nil, panelerr.Validation("application_file_content_invalid", "file content must be base64 encoded")
	}
	return content, nil
}

func (s *Service) getSaveSession(sessionID string) (*saveSession, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session := s.saveSessions[sessionID]
	if session == nil {
		return nil, panelerr.NotFound("application_save_session")
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		delete(s.saveSessions, sessionID)
		_ = os.RemoveAll(session.Dir)
		return nil, panelerr.NotFound("application_save_session")
	}
	session.UpdatedAt = time.Now().UTC()
	session.ExpiresAt = session.UpdatedAt.Add(30 * time.Minute)
	return session, nil
}

func (s *Service) discardSaveSession(sessionID string) {
	s.sessionMu.Lock()
	session := s.saveSessions[sessionID]
	delete(s.saveSessions, sessionID)
	s.sessionMu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.Dir)
	}
}

func (s *Service) stageFileBytes(session *saveSession, targetPath, kind, contentType string, content []byte, createdAt time.Time) (*stagedFile, error) {
	targetPath, err := normalizeApplicationFilePath(targetPath)
	if err != nil {
		return nil, err
	}
	kind = strings.TrimSpace(kind)
	if kind != "binary" && kind != "template" {
		return nil, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	staged := &stagedFile{
		ID:            id.New("afile"),
		ApplicationID: session.ApplicationID,
		Path:          targetPath,
		Kind:          kind,
		ContentType:   strings.TrimSpace(contentType),
		Size:          int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		DiskPath:      filepath.Join(session.Dir, id.New("blob")),
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}
	if err := os.WriteFile(staged.DiskPath, content, 0o600); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	old := session.Files[targetPath]
	session.Files[targetPath] = staged
	session.UpdatedAt = now
	session.ExpiresAt = now.Add(30 * time.Minute)
	s.sessionMu.Unlock()
	if old != nil {
		_ = os.Remove(old.DiskPath)
	}
	return staged, nil
}

func (s *Service) startSaveSessionCleanup() {
	s.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				s.cleanupExpiredSaveSessions(time.Now().UTC())
			}
		}()
	})
}

func (s *Service) cleanupExpiredSaveSessions(now time.Time) {
	expired := []*saveSession{}
	s.sessionMu.Lock()
	for key, session := range s.saveSessions {
		if now.Sub(session.UpdatedAt) <= 30*time.Minute {
			continue
		}
		expired = append(expired, session)
		delete(s.saveSessions, key)
	}
	s.sessionMu.Unlock()
	for _, session := range expired {
		_ = os.RemoveAll(session.Dir)
	}
	entries, err := os.ReadDir(s.config.SaveSessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= 30*time.Minute {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.config.SaveSessionDir, entry.Name()))
	}
}

func (session *saveSession) result() SaveSessionResult {
	files := make([]ApplicationFile, 0, len(session.Files))
	for _, staged := range session.Files {
		files = append(files, staged.applicationFile(nil))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return SaveSessionResult{
		ID:            session.ID,
		ApplicationID: session.ApplicationID,
		ExpiresAt:     session.ExpiresAt,
		Files:         files,
	}
}

func (session *saveSession) applicationFiles() ([]ApplicationFile, error) {
	files := make([]ApplicationFile, 0, len(session.Files))
	for _, staged := range session.Files {
		content, err := os.ReadFile(staged.DiskPath)
		if err != nil {
			return nil, err
		}
		files = append(files, staged.applicationFile(content))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (file *stagedFile) applicationFile(content []byte) ApplicationFile {
	contentBase64 := ""
	if content != nil {
		contentBase64 = base64.StdEncoding.EncodeToString(content)
	}
	return ApplicationFile{
		ID:            file.ID,
		ApplicationID: file.ApplicationID,
		Path:          file.Path,
		Kind:          file.Kind,
		ContentType:   file.ContentType,
		Size:          file.Size,
		SHA256:        file.SHA256,
		Content:       content,
		ContentBase64: contentBase64,
		CreatedAt:     file.CreatedAt,
		UpdatedAt:     file.UpdatedAt,
	}
}

func normalizeApplicationFilesForSave(appID string, files []ApplicationFile, now time.Time) []ApplicationFile {
	if files == nil {
		return nil
	}
	out := make([]ApplicationFile, 0, len(files))
	for _, file := range files {
		if file.ID == "" {
			file.ID = id.New("afile")
		}
		file.ApplicationID = appID
		if file.CreatedAt.IsZero() {
			file.CreatedAt = now
		}
		if file.UpdatedAt.IsZero() {
			file.UpdatedAt = now
		}
		out = append(out, file)
	}
	return out
}

type archivedApplicationFile struct {
	Name    string
	Content []byte
}

func extractApplicationFileArchive(reader io.ReaderAt, size int64, filename string) ([]archivedApplicationFile, error) {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.NewReader(reader, size)
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		return extractApplicationZip(zr)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		gz, err := gzip.NewReader(io.NewSectionReader(reader, 0, size))
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		defer gz.Close()
		return extractApplicationTar(tar.NewReader(gz))
	case strings.HasSuffix(lower, ".tar"):
		return extractApplicationTar(tar.NewReader(io.NewSectionReader(reader, 0, size)))
	default:
		return nil, panelerr.Validation("application_file_archive_invalid", "folder uploads must use zip, tar, tar.gz, or tgz")
	}
}

func extractApplicationZip(reader *zip.Reader) ([]archivedApplicationFile, error) {
	out := []archivedApplicationFile{}
	for _, file := range reader.File {
		name := cleanApplicationArchivePath(file.Name)
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		out = append(out, archivedApplicationFile{Name: name, Content: content})
	}
	return nonEmptyApplicationArchive(out)
}

func extractApplicationTar(reader *tar.Reader) ([]archivedApplicationFile, error) {
	out := []archivedApplicationFile{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		name := cleanApplicationArchivePath(header.Name)
		if name == "" {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		out = append(out, archivedApplicationFile{Name: name, Content: content})
	}
	return nonEmptyApplicationArchive(out)
}

func nonEmptyApplicationArchive(files []archivedApplicationFile) ([]archivedApplicationFile, error) {
	if len(files) == 0 {
		return nil, panelerr.Validation("application_file_archive_empty", "application file archive is empty")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func cleanApplicationArchivePath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "/")
	value = path.Clean(value)
	if value == "." || strings.HasPrefix(value, "../") || value == ".." {
		return ""
	}
	return value
}

func normalizeApplicationArchiveBasePath(value string) (string, error) {
	value = strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/")
	if value == "" {
		return "", nil
	}
	normalized, err := normalizeApplicationFilePath(value + "/.archive-base")
	if err != nil {
		return "", err
	}
	return path.Dir(normalized), nil
}
