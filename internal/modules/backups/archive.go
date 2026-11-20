package backups

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const manifestName = "manifest.json"

func buildArchive(cfg ArchiveConfig, encrypted bool) ([]byte, Manifest, error) {
	manifest := Manifest{
		FormatVersion: 1,
		PanelVersion:  cfg.PanelVersion,
		CreatedAt:     time.Now().UTC(),
		Encrypted:     encrypted,
		Includes:      []string{"dataRoot", "appDatabase", "logDatabase", "coordinationDatabase", "metricsDatabase"},
		Metadata: map[string]string{
			"log":     "included_history",
			"metrics": "included_history",
		},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	seen := map[string]struct{}{}
	addRoot := func(root, prefix string) error {
		return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if shouldSkipDir(root, path) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(filepath.Join(prefix, rel))
			return addFile(zw, path, name, &manifest, seen)
		})
	}
	if err := addRoot(cfg.DataRoot, "dataRoot"); err != nil {
		return nil, manifest, err
	}
	for _, item := range []struct {
		path string
		name string
	}{
		{cfg.AppDatabase, "databases/app.db"},
		{cfg.LogDatabase, "databases/log.db"},
		{cfg.CoordinationDatabase, "databases/coordination.db"},
		{cfg.MetricsDatabase, "databases/metrics.db"},
	} {
		if item.path == "" {
			continue
		}
		if err := addFile(zw, item.path, item.name, &manifest, seen); err != nil {
			return nil, manifest, err
		}
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, manifest, err
	}
	w, err := zw.Create(manifestName)
	if err != nil {
		return nil, manifest, err
	}
	if _, err := w.Write(manifestBytes); err != nil {
		return nil, manifest, err
	}
	if err := zw.Close(); err != nil {
		return nil, manifest, err
	}
	return buf.Bytes(), manifest, nil
}

func addFile(zw *zip.Writer, path, name string, manifest *Manifest, seen map[string]struct{}) error {
	name = filepath.ToSlash(filepath.Clean(name))
	if !safeArchivePath(name) {
		return errors.New("unsafe archive path")
	}
	if _, ok := seen[name]; ok {
		return nil
	}
	seen[name] = struct{}{}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		// Skip symlinks and special files so backups cannot escape DataRoot
		// by following links (BKP-API-005 / archive path safety).
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	hasher := sha256.New()
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name
	header.Method = zip.Deflate
	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.MultiWriter(w, hasher), file); err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, ManifestFile{
		Path:   name,
		Size:   info.Size(),
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
	})
	return nil
}

func readManifest(raw []byte, password string) (Manifest, []byte, error) {
	plain, err := decryptBytes(raw, password)
	if err != nil {
		return Manifest{}, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return Manifest{}, nil, errArchiveInvalid
	}
	for _, file := range zr.File {
		if file.Name != manifestName {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return Manifest{}, nil, err
		}
		defer rc.Close()
		var manifest Manifest
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return Manifest{}, nil, errArchiveInvalid
		}
		if manifest.FormatVersion != 1 {
			return Manifest{}, nil, errArchiveUnsupported
		}
		for _, item := range manifest.Files {
			if !safeArchivePath(item.Path) {
				return Manifest{}, nil, errArchiveInvalid
			}
		}
		return manifest, plain, nil
	}
	return Manifest{}, nil, errArchiveInvalid
}

func extractArchive(plain []byte, target string, manifest Manifest) error {
	expected := make(map[string]ManifestFile, len(manifest.Files))
	for _, item := range manifest.Files {
		path := filepath.ToSlash(filepath.Clean(item.Path))
		if !safeArchivePath(path) {
			return errArchiveInvalid
		}
		if _, dup := expected[path]; dup {
			return errArchiveInvalid
		}
		expected[path] = ManifestFile{Path: path, Size: item.Size, SHA256: strings.ToLower(strings.TrimSpace(item.SHA256))}
	}
	zr, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return errArchiveInvalid
	}
	seen := make(map[string]struct{}, len(expected))
	for _, file := range zr.File {
		if file.Name == manifestName {
			continue
		}
		info := file.FileInfo()
		if info.IsDir() {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(file.Name))
		if !safeArchivePath(name) {
			return errArchiveInvalid
		}
		item, ok := expected[name]
		if !ok {
			return errArchiveInvalid
		}
		if _, dup := seen[name]; dup {
			return errArchiveInvalid
		}
		seen[name] = struct{}{}
		out := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		err = writeExtractedFileVerified(out, rc, file.Mode(), item)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	if len(seen) != len(expected) {
		return errArchiveInvalid
	}
	return nil
}

func writeExtractedFileVerified(path string, r io.Reader, mode os.FileMode, expected ManifestFile) error {
	if mode == 0 {
		mode = 0600
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer f.Close()
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hasher), r)
	if err != nil {
		return err
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if n != expected.Size || !strings.EqualFold(sum, expected.SHA256) {
		return errArchiveInvalid
	}
	return nil
}

func safeArchivePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == path && clean != "." && !strings.HasPrefix(clean, "../") && clean != ".."
}

func shouldSkipDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] != "tmp" {
		return false
	}
	switch parts[1] {
	case "backups", "backup-export-pending", "restore-pending", "restore-pending.previous", "restore-staging", "maintenance", "key-assets", "key-asset-exports":
		// These directories hold temporary working data or encrypted/private
		// key material that must not be carried into an (possibly unencrypted)
		// backup archive, and restore media must not be resurrected.
		return true
	}
	return false
}

type ArchiveConfig struct {
	DataRoot             string
	AppDatabase          string
	LogDatabase          string
	CoordinationDatabase string
	MetricsDatabase      string
	PanelVersion         string
}
