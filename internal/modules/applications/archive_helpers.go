package applications

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

func normalizeApplicationFilesForSave(appID string, files []ApplicationFile, now time.Time) []ApplicationFile {
	if files == nil {
		return nil
	}
	out := make([]ApplicationFile, 0, len(files))
	for _, file := range files {
		if file.Name == "" {
			file.Name = file.Path
		}
		file.Path = file.Name
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

const (
	applicationArchiveMaxFiles     = 10000
	applicationArchiveMaxDepth     = 32
	applicationArchiveMaxExtracted = int64(256 << 20)
	applicationArchiveMaxRatio     = int64(100)
)

func extractApplicationFileArchive(reader io.ReaderAt, size int64, filename string) ([]archivedApplicationFile, error) {
	lower := strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.NewReader(reader, size)
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		return extractApplicationZip(zr, size)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		gz, err := gzip.NewReader(io.NewSectionReader(reader, 0, size))
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		defer gz.Close()
		return extractApplicationTar(tar.NewReader(gz), size)
	case strings.HasSuffix(lower, ".tar"):
		return extractApplicationTar(tar.NewReader(io.NewSectionReader(reader, 0, size)), size)
	default:
		return nil, panelerr.Validation("application_file_archive_invalid", "folder uploads must use zip, tar, tar.gz, or tgz")
	}
}

func extractApplicationZip(reader *zip.Reader, compressedSize int64) ([]archivedApplicationFile, error) {
	out := []archivedApplicationFile{}
	entries := 0
	var extracted int64
	for _, file := range reader.File {
		name, ok := cleanApplicationArchivePath(file.Name)
		if !ok {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		if name == "" || file.FileInfo().IsDir() {
			if name != "" {
				entries++
				if entries > applicationArchiveMaxFiles {
					return nil, applicationArchiveLimitError()
				}
			}
			continue
		}
		entries++
		if entries > applicationArchiveMaxFiles {
			return nil, applicationArchiveLimitError()
		}
		if file.UncompressedSize64 > uint64(applicationArchiveMaxExtracted) {
			return nil, applicationArchiveLimitError()
		}
		extracted += int64(file.UncompressedSize64)
		if err := validateApplicationArchiveLimits(name, entries, extracted, compressedSize); err != nil {
			return nil, err
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

func extractApplicationTar(reader *tar.Reader, compressedSize int64) ([]archivedApplicationFile, error) {
	out := []archivedApplicationFile{}
	entries := 0
	var extracted int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		name, ok := cleanApplicationArchivePath(header.Name)
		if !ok {
			return nil, panelerr.Validation("application_file_archive_invalid", "application file archive is invalid")
		}
		if name == "" {
			continue
		}
		entries++
		if entries > applicationArchiveMaxFiles {
			return nil, applicationArchiveLimitError()
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if header.Size < 0 {
			return nil, applicationArchiveLimitError()
		}
		extracted += header.Size
		if err := validateApplicationArchiveLimits(name, entries, extracted, compressedSize); err != nil {
			return nil, err
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		out = append(out, archivedApplicationFile{Name: name, Content: content})
	}
	return nonEmptyApplicationArchive(out)
}

func validateApplicationArchiveLimits(name string, count int, extracted, compressed int64) error {
	depth := strings.Count(strings.Trim(name, "/"), "/") + 1
	if count > applicationArchiveMaxFiles || depth > applicationArchiveMaxDepth || extracted > applicationArchiveMaxExtracted || (compressed > 0 && extracted > compressed*applicationArchiveMaxRatio && extracted > 1<<20) {
		return applicationArchiveLimitError()
	}
	return nil
}

func applicationArchiveLimitError() error {
	return panelerr.Validation("application_file_archive_limits_exceeded", "application file archive exceeds file count, depth, extracted size, or compression ratio limits")
}

func nonEmptyApplicationArchive(files []archivedApplicationFile) ([]archivedApplicationFile, error) {
	if len(files) == 0 {
		return nil, panelerr.Validation("application_file_archive_empty", "application file archive is empty")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func cleanApplicationArchivePath(value string) (string, bool) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "/")
	value = path.Clean(value)
	if value == "." || strings.HasPrefix(value, "../") || value == ".." {
		return "", false
	}
	return value, true
}
