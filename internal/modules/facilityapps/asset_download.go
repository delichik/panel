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

func (s *Service) GetFacilityEditAssetDownload(ctx context.Context, sessionID, assetKey string) (FacilityAssetDownload, error) {
	record, err := s.loadFacilityEditSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	if record.State != FacilityEditSessionActive && record.State != FacilityEditSessionConflict {
		return FacilityAssetDownload{}, panelerr.Conflict("facility_edit_session_state_invalid", "facility edit session is not readable")
	}
	var result FacilityAssetDownload
	var sourceID, blobDir string
	err = s.db.QueryRowContext(ctx, `SELECT name,kind,content_mode,filename,source_asset_id,blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key=? AND state='ready'`, record.ID, strings.TrimSpace(assetKey)).Scan(&result.Name, &result.Kind, &result.ContentMode, &result.Filename, &sourceID, &blobDir)
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

func (s *Service) GetStaticAssetDownload(ctx context.Context, assetID string) (FacilityAssetDownload, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" || filepath.Base(assetID) != assetID {
		return FacilityAssetDownload{}, panelerr.NotFound("facility_static_asset")
	}
	var result FacilityAssetDownload
	err := s.db.QueryRowContext(ctx, `SELECT name,kind,content_mode,filename FROM facility_static_assets WHERE id=?`, assetID).Scan(&result.Name, &result.Kind, &result.ContentMode, &result.Filename)
	if err == sql.ErrNoRows {
		return FacilityAssetDownload{}, panelerr.NotFound("facility_static_asset")
	}
	if err != nil {
		return FacilityAssetDownload{}, err
	}
	result.Root = s.staticAssetContentDir(assetID)
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
