package facilityapps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type StaticAssetUploadInput struct {
	AssetID   string
	AssetName string
	Name      string
	Kind      string
	FileName  string
	Size      int64
	Content   []byte
}

type StaticAssetDeleteInput struct {
	AssetName string `json:"assetName,omitempty"`
	// AssetID is accepted only by the legacy in-memory save-session endpoint.
	AssetID string `json:"assetId,omitempty"`
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
	duplicate, err := orm.New(s.db).From("facility_static_assets").Where("name=?", name).Count(ctx)
	if err != nil {
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
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := orm.RawExec(ctx, tx, `DELETE FROM facility_static_assets WHERE id=?`, assetID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return panelerr.NotFound("static asset")
		}
		return bumpFacilityConfigVersionTx(ctx, tx)
	}); err != nil {
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
	asset, err := s.getStaticAsset(ctx, assetID)
	if err != nil {
		return false, err
	}
	for _, domain := range cfg.Domains {
		for _, routePath := range domain.Paths {
			if routePath.AssetName == asset.Name {
				return true, nil
			}
		}
	}
	return false, nil
}
