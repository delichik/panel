package applications

import (
	"context"
	"database/sql"
	"io"
	"strings"

	appruntime "panel/internal/modules/applications/runtime"
	appspec "panel/internal/modules/applications/spec"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
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

func fileQueryColumns(includeContent bool) []string {
	if includeContent {
		return []string{"id", "application_id", "name", "kind", "content_type", "size", "sha256", "content", "created_at", "updated_at"}
	}
	return []string{"id", "application_id", "name", "kind", "content_type", "size", "sha256", "created_at", "updated_at"}
}

func (s *Service) listFiles(ctx context.Context, appID string, includeContent bool) ([]ApplicationFile, error) {
	var files []models.ApplicationFile
	if err := orm.New(s.db).From("application_files").Select(fileQueryColumns(includeContent)...).Where("application_id=?", appID).OrderBy("name ASC").All(ctx, &files); err != nil {
		return nil, err
	}
	out := make([]ApplicationFile, 0, len(files))
	for _, file := range files {
		out = append(out, toDomainApplicationFile(file, includeContent))
	}
	return out, nil
}

func (s *Service) getFileByName(ctx context.Context, appID, name string, includeContent bool) (ApplicationFile, error) {
	var file models.ApplicationFile
	err := orm.New(s.db).From("application_files").Select(fileQueryColumns(includeContent)...).Where("application_id=?", appID).And("name=?", name).First(ctx, &file)
	if err == sql.ErrNoRows {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	if err != nil {
		return ApplicationFile{}, err
	}
	return toDomainApplicationFile(file, includeContent), nil
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
	var byID models.ApplicationFile
	if err := orm.New(s.db).From("application_files").Select(fileQueryColumns(includeContent)...).Where("application_id=?", appID).And("id=?", ref).First(ctx, &byID); err != nil {
		return ApplicationFile{}, err
	}
	return toDomainApplicationFile(byID, includeContent), nil
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
