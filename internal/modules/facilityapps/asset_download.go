package facilityapps

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

type FacilityAssetDownload struct {
	Name        string
	Kind        string
	ContentMode string
	Filename    string
	Root        string
}

type facilityEditSessionAssetRow struct {
	Name          string
	Kind          string
	ContentMode   string
	Filename      string
	SourceAssetID string
	BlobDir       string
}

type facilityStaticAssetRow struct {
	Name        string
	Kind        string
	ContentMode string
	Filename    string
}

func (s *Service) GetFacilityEditAssetDownload(ctx context.Context, sessionID, assetRef string) (FacilityAssetDownload, error) {
	record, err := s.loadFacilityEditSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	if record.State != FacilityEditSessionActive && record.State != FacilityEditSessionConflict {
		return FacilityAssetDownload{}, panelerr.Conflict("facility_edit_session_state_invalid", "facility edit session is not readable")
	}
	var result FacilityAssetDownload
	var row facilityEditSessionAssetRow
	ref := strings.TrimSpace(assetRef)
	err = orm.New(s.db).From("facility_edit_session_assets").Select("name", "kind", "content_mode", "filename", "source_asset_id", "blob_dir").Where("session_id=?", record.ID).And("name=?", ref).And("state=?", "ready").First(ctx, &row)
	if err == sql.ErrNoRows {
		// Legacy edit sessions exposed the opaque asset key. Keep reads working
		// while new clients address assets by name.
		err = orm.New(s.db).From("facility_edit_session_assets").Select("name", "kind", "content_mode", "filename", "source_asset_id", "blob_dir").Where("session_id=?", record.ID).And("asset_key=?", ref).And("state=?", "ready").First(ctx, &row)
	}
	if err == sql.ErrNoRows {
		return FacilityAssetDownload{}, panelerr.NotFound("facility_edit_session_asset")
	}
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	result.Name = row.Name
	result.Kind = row.Kind
	result.ContentMode = row.ContentMode
	result.Filename = row.Filename
	if strings.TrimSpace(row.BlobDir) != "" {
		result.Root = filepath.Join(row.BlobDir, "content")
	} else if strings.TrimSpace(row.SourceAssetID) != "" {
		result.Root = s.staticAssetContentDir(row.SourceAssetID)
	} else {
		return FacilityAssetDownload{}, panelerr.NotFound("facility_edit_session_asset_content")
	}
	return validateFacilityAssetDownload(result)
}

func (s *Service) GetStaticAssetDownload(ctx context.Context, assetRef string) (FacilityAssetDownload, error) {
	asset, err := s.getStaticAssetByRef(ctx, assetRef)
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	var result FacilityAssetDownload
	var row facilityStaticAssetRow
	err = orm.New(s.db).From("facility_static_assets").Select("name", "kind", "content_mode", "filename").Where("id=?", asset.ID).First(ctx, &row)
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	result.Name = row.Name
	result.Kind = row.Kind
	result.ContentMode = row.ContentMode
	result.Filename = row.Filename
	result.Root = s.staticAssetContentDir(asset.ID)
	return validateFacilityAssetDownload(result)
}

func validateFacilityAssetDownload(result FacilityAssetDownload) (FacilityAssetDownload, error) {
	info, err := os.Stat(result.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return FacilityAssetDownload{}, panelerr.NotFound("facility_static_asset_content")
		}
		return FacilityAssetDownload{}, err
	}
	if !info.IsDir() {
		return FacilityAssetDownload{}, panelerr.Validation("facility_static_asset_content_invalid", "static asset content is invalid")
	}
	return result, nil
}
