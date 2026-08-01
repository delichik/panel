package facilityapps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
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

const (
	facilityAssetArchiveMaxFiles     = 10000
	facilityAssetArchiveMaxDepth     = 32
	facilityAssetArchiveMaxExtracted = int64(256 << 20)
	facilityAssetArchiveMaxRatio     = int64(100)
)

func (s *Service) listStaticAssets(ctx context.Context) ([]StaticAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,content_mode,filename,size,sha256,created_at,updated_at FROM facility_static_assets ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaticAsset{}
	for rows.Next() {
		asset, err := scanStaticAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, rows.Err()
}

func (s *Service) UploadStaticAsset(ctx context.Context, in StaticAssetUploadInput) (StaticAsset, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSpace(in.FileName)
	}
	if name == "" {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_name_required", "Static asset name is required")
	}
	var duplicate int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facility_static_assets WHERE name=?`, name).Scan(&duplicate); err != nil {
		return StaticAsset{}, err
	}
	if duplicate > 0 {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_name_duplicate", "Static asset name must be unique within the facility")
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != StaticSourceUploadedFile && kind != StaticSourceUploadedBundle {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_kind_invalid", "Static asset kind is invalid")
	}
	if len(in.Content) == 0 {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_file_required", "Static asset file is required")
	}
	if strings.TrimSpace(s.dataRoot) == "" {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_storage_unavailable", "Static asset storage is unavailable")
	}
	assetID := id.New("facility_static")
	filename := safeAssetFilename(in.FileName)
	if filename == "" {
		filename = "asset"
	}
	dir := s.staticAssetContentDir(assetID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return StaticAsset{}, err
	}
	if kind == StaticSourceUploadedBundle {
		if err := extractStaticBundle(bytes.NewReader(in.Content), int64(len(in.Content)), filename, dir); err != nil {
			_ = os.RemoveAll(s.staticAssetDir(assetID))
			return StaticAsset{}, err
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, filename), in.Content, 0o644); err != nil {
			_ = os.RemoveAll(s.staticAssetDir(assetID))
			return StaticAsset{}, err
		}
	}
	sum := sha256.Sum256(in.Content)
	now := time.Now().UTC()
	asset := StaticAsset{
		ID:          assetID,
		Name:        name,
		Kind:        kind,
		ContentMode: "binary",
		Filename:    filename,
		Size:        int64(len(in.Content)),
		SHA256:      hex.EncodeToString(sum[:]),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.insertStaticAsset(ctx, asset); err != nil {
		_ = os.RemoveAll(s.staticAssetDir(assetID))
		return StaticAsset{}, err
	}
	return asset, nil
}

func (s *Service) DeleteStaticAsset(ctx context.Context, assetName string) error {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	assetName = strings.TrimSpace(assetName)
	if assetName == "" {
		return panelerr.NotFound("static asset")
	}
	asset, err := s.getStaticAssetByRef(ctx, assetName)
	if err != nil {
		return err
	}
	assetID := asset.ID
	used, err := s.staticAssetInUse(ctx, assetID)
	if err != nil {
		return err
	}
	if used {
		return panelerr.Conflict("facility_static_asset_in_use", "Static asset is still used by reverse proxy")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM facility_static_assets WHERE id=?`, assetID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return panelerr.NotFound("static asset")
	}
	if err := bumpFacilityConfigVersionTx(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = os.RemoveAll(s.staticAssetDir(assetID))
	return nil
}

func (s *Service) staticAssetInUse(ctx context.Context, assetID string) (bool, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return false, err
	}
	for _, domain := range cfg.Domains {
		for _, routePath := range domain.Paths {
			asset, err := s.getStaticAsset(ctx, assetID)
			if err != nil {
				return false, err
			}
			if routePath.AssetName == asset.Name {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Service) getStaticAsset(ctx context.Context, assetID string) (StaticAsset, error) {
	asset, err := scanStaticAsset(s.db.QueryRowContext(ctx, `SELECT id,name,kind,content_mode,filename,size,sha256,created_at,updated_at FROM facility_static_assets WHERE id=?`, assetID))
	if err == sql.ErrNoRows {
		return StaticAsset{}, panelerr.NotFound("static asset")
	}
	return asset, err
}

func (s *Service) getStaticAssetByName(ctx context.Context, name string) (StaticAsset, error) {
	asset, err := scanStaticAsset(s.db.QueryRowContext(ctx, `SELECT id,name,kind,content_mode,filename,size,sha256,created_at,updated_at FROM facility_static_assets WHERE name=?`, strings.TrimSpace(name)))
	if err == sql.ErrNoRows {
		return StaticAsset{}, panelerr.NotFound("static asset")
	}
	return asset, err
}

// getStaticAssetByRef keeps old physical-id callers working while all new
// requests use the facility-local asset name.
func (s *Service) getStaticAssetByRef(ctx context.Context, ref string) (StaticAsset, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return StaticAsset{}, panelerr.NotFound("static asset")
	}
	asset, err := s.getStaticAssetByName(ctx, ref)
	if err == nil {
		return asset, nil
	}
	return s.getStaticAsset(ctx, ref)
}

func (s *Service) staticAssetFiles(assetID string) ([]appruntimeFile, error) {
	root := s.staticAssetContentDir(assetID)
	files := []appruntimeFile{}
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !validStaticAssetRelativePath(rel) {
			return errors.New("static asset file path is invalid")
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		files = append(files, appruntimeFile{Path: rel, Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

type appruntimeFile struct {
	Path    string
	Content []byte
}

func (s *Service) insertStaticAsset(ctx context.Context, asset StaticAsset) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO facility_static_assets(id,name,kind,content_mode,filename,size,sha256,created_at,updated_at) VALUES(?,?,?,'binary',?,?,?,?,?)`,
		asset.ID, asset.Name, asset.Kind, asset.Filename, asset.Size, asset.SHA256, formatTime(asset.CreatedAt), formatTime(asset.UpdatedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return panelerr.Validation("facility_static_asset_name_duplicate", "Static asset name must be unique within the facility")
		}
		return err
	}
	if err := bumpFacilityConfigVersionTx(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func bumpFacilityConfigVersionTx(ctx context.Context, tx *sql.Tx) error {
	now := formatTime(time.Now().UTC())
	_, err := tx.ExecContext(ctx, `INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,panel_entry_json,domains_json,last_error,updated_at) VALUES(?,1,'[]','{}','[]','',?) ON CONFLICT(id) DO UPDATE SET version=facility_app_configs.version+1,updated_at=excluded.updated_at`, ReverseProxyID, now)
	return err
}

func scanStaticAsset(row interface{ Scan(dest ...any) error }) (StaticAsset, error) {
	var asset StaticAsset
	var created, updated string
	if err := row.Scan(&asset.ID, &asset.Name, &asset.Kind, &asset.ContentMode, &asset.Filename, &asset.Size, &asset.SHA256, &created, &updated); err != nil {
		return StaticAsset{}, err
	}
	asset.CreatedAt = parseTime(created)
	asset.UpdatedAt = parseTime(updated)
	return asset, nil
}

func (s *Service) staticAssetDir(assetID string) string {
	return filepath.Join(s.dataRoot, "facility-apps", "static-assets", assetID)
}

func (s *Service) staticAssetContentDir(assetID string) string {
	return filepath.Join(s.staticAssetDir(assetID), "content")
}

func safeAssetFilename(value string) string {
	value = strings.TrimSpace(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	value = strings.Trim(value, ". ")
	if value == "" || strings.ContainsAny(value, `/\`) {
		return ""
	}
	return value
}

func extractStaticBundle(reader io.ReaderAt, size int64, filename, target string) error {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		zr, err := zip.NewReader(reader, size)
		if err != nil {
			return panelerr.Validation("facility_static_asset_archive_invalid", "Static asset archive is invalid")
		}
		return extractZip(zr, target, size)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		section := io.NewSectionReader(reader, 0, size)
		gz, err := gzip.NewReader(section)
		if err != nil {
			return panelerr.Validation("facility_static_asset_archive_invalid", "Static asset archive is invalid")
		}
		defer gz.Close()
		return extractTar(tar.NewReader(gz), target, size)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(tar.NewReader(io.NewSectionReader(reader, 0, size)), target, size)
	default:
		return panelerr.Validation("facility_static_asset_archive_invalid", "Folder uploads must use zip, tar, tar.gz, or tgz")
	}
}

func extractZip(reader *zip.Reader, target string, compressedSize int64) error {
	count := 0
	entries := 0
	var extracted int64
	for _, file := range reader.File {
		name := cleanArchivePath(file.Name)
		if name == "" {
			continue
		}
		entries++
		if entries > facilityAssetArchiveMaxFiles {
			return panelerr.Validation("facility_static_asset_archive_limits_exceeded", "Static asset archive exceeds entry count, depth, extracted size, or compression ratio limits")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > uint64(facilityAssetArchiveMaxExtracted) {
			return panelerr.Validation("facility_static_asset_archive_limits_exceeded", "Static asset archive exceeds file count, depth, extracted size, or compression ratio limits")
		}
		count++
		extracted += int64(file.UncompressedSize64)
		if err := validateFacilityArchiveLimits(name, count, extracted, compressedSize); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		if err := writeArchiveFile(target, name, rc); err != nil {
			_ = rc.Close()
			return err
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}
	if count == 0 {
		return panelerr.Validation("facility_static_asset_archive_empty", "Static asset archive is empty")
	}
	return nil
}

func extractTar(reader *tar.Reader, target string, compressedSize int64) error {
	count := 0
	entries := 0
	var extracted int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return panelerr.Validation("facility_static_asset_archive_invalid", "Static asset archive is invalid")
		}
		name := cleanArchivePath(header.Name)
		if name == "" {
			continue
		}
		entries++
		if entries > facilityAssetArchiveMaxFiles {
			return panelerr.Validation("facility_static_asset_archive_limits_exceeded", "Static asset archive exceeds entry count, depth, extracted size, or compression ratio limits")
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if header.Size < 0 {
			return panelerr.Validation("facility_static_asset_archive_limits_exceeded", "Static asset archive exceeds file count, depth, extracted size, or compression ratio limits")
		}
		count++
		extracted += header.Size
		if err := validateFacilityArchiveLimits(name, count, extracted, compressedSize); err != nil {
			return err
		}
		if err := writeArchiveFile(target, name, reader); err != nil {
			return err
		}
	}
	if count == 0 {
		return panelerr.Validation("facility_static_asset_archive_empty", "Static asset archive is empty")
	}
	return nil
}

func validateFacilityArchiveLimits(name string, count int, extracted, compressed int64) error {
	depth := strings.Count(strings.Trim(name, "/"), "/") + 1
	if count > facilityAssetArchiveMaxFiles || depth > facilityAssetArchiveMaxDepth || extracted > facilityAssetArchiveMaxExtracted || (compressed > 0 && extracted > compressed*facilityAssetArchiveMaxRatio && extracted > 1<<20) {
		return panelerr.Validation("facility_static_asset_archive_limits_exceeded", "Static asset archive exceeds file count, depth, extracted size, or compression ratio limits")
	}
	return nil
}

func writeArchiveFile(root, rel string, reader io.Reader) error {
	if !validStaticAssetRelativePath(rel) {
		return panelerr.Validation("facility_static_asset_archive_invalid", "Static asset archive contains an invalid path")
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if cleanTarget != cleanRoot && !strings.HasPrefix(cleanTarget, cleanRoot+string(os.PathSeparator)) {
		return panelerr.Validation("facility_static_asset_archive_invalid", "Static asset archive contains an invalid path")
	}
	if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, reader)
	return err
}

func cleanArchivePath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "/")
	value = path.Clean(value)
	if value == "." || strings.HasPrefix(value, "../") || value == ".." {
		return ""
	}
	return value
}

func validStaticAssetRelativePath(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") && value != ".."
}
