package facilityapps

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	panelerr "panel/internal/platform/errors"
)

type FacilityAssetDownload struct {
	Name        string
	Kind        string
	ContentMode string
	Filename    string
	Root        string
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
	var sourceID, blobDir string
	ref := strings.TrimSpace(assetRef)
	err = s.db.QueryRowContext(ctx, `SELECT name,kind,content_mode,filename,source_asset_id,blob_dir FROM facility_edit_session_assets WHERE session_id=? AND name=? AND state='ready'`, record.ID, ref).Scan(&result.Name, &result.Kind, &result.ContentMode, &result.Filename, &sourceID, &blobDir)
	if err == sql.ErrNoRows {
		// Legacy edit sessions exposed the opaque asset key. Keep reads working
		// while new clients address assets by name.
		err = s.db.QueryRowContext(ctx, `SELECT name,kind,content_mode,filename,source_asset_id,blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key=? AND state='ready'`, record.ID, ref).Scan(&result.Name, &result.Kind, &result.ContentMode, &result.Filename, &sourceID, &blobDir)
	}
	if err == sql.ErrNoRows {
		return FacilityAssetDownload{}, panelerr.NotFound("facility_edit_session_asset")
	}
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	if strings.TrimSpace(blobDir) != "" {
		result.Root = filepath.Join(blobDir, "content")
	} else if strings.TrimSpace(sourceID) != "" {
		result.Root = s.staticAssetContentDir(sourceID)
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
	err = s.db.QueryRowContext(ctx, `SELECT name,kind,content_mode,filename FROM facility_static_assets WHERE id=?`, asset.ID).Scan(&result.Name, &result.Kind, &result.ContentMode, &result.Filename)
	if err != nil {
		return FacilityAssetDownload{}, err
	}
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
