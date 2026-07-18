package facilityapps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type facilitySaveSession struct {
	ID            string
	Dir           string
	BaseUpdatedAt time.Time
	BaseVersion   int
	Assets        map[string]*stagedFacilityAsset
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
}

type stagedFacilityAsset struct {
	Asset StaticAsset
	Dir   string
}

func (s *Service) BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	current, err := s.loadConfig(ctx)
	if err != nil {
		return SaveSessionResult{}, err
	}
	baseUpdatedAt := in.BaseUpdatedAt
	if baseUpdatedAt.IsZero() {
		baseUpdatedAt = current.UpdatedAt
	}
	assets, err := s.ListStaticAssets(ctx)
	if err != nil {
		return SaveSessionResult{}, err
	}
	now := time.Now().UTC()
	session := &facilitySaveSession{
		ID:            id.New("frpsave"),
		Dir:           filepath.Join(s.sessionDir, id.New("session")),
		BaseUpdatedAt: baseUpdatedAt,
		BaseVersion:   current.Version,
		Assets:        map[string]*stagedFacilityAsset{},
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     now.Add(30 * time.Minute),
	}
	for _, asset := range assets {
		assetCopy := asset
		session.Assets[asset.ID] = &stagedFacilityAsset{Asset: assetCopy}
	}
	if err := os.MkdirAll(filepath.Join(session.Dir, "assets"), 0o700); err != nil {
		return SaveSessionResult{}, err
	}
	s.sessionMu.Lock()
	s.saveSessions[session.ID] = session
	s.sessionMu.Unlock()
	return session.result(), nil
}

func (s *Service) UploadSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetUploadInput) (StaticAsset, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return StaticAsset{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSpace(in.FileName)
	}
	if name == "" {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_name_required", "Static asset name is required")
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != StaticSourceUploadedFile && kind != StaticSourceUploadedBundle {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_kind_invalid", "Static asset kind is invalid")
	}
	if len(in.Content) == 0 {
		return StaticAsset{}, panelerr.Validation("facility_static_asset_file_required", "Static asset file is required")
	}
	assetID := strings.TrimSpace(in.AssetID)
	if assetID == "" {
		assetID = id.New("facility_static")
	} else if _, ok := session.Assets[assetID]; !ok {
		return StaticAsset{}, panelerr.NotFound("static asset")
	}
	filename := safeAssetFilename(in.FileName)
	if filename == "" {
		filename = "asset"
	}
	assetDir := filepath.Join(session.Dir, "assets", assetID)
	if err := os.RemoveAll(assetDir); err != nil {
		return StaticAsset{}, err
	}
	contentDir := filepath.Join(assetDir, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return StaticAsset{}, err
	}
	if kind == StaticSourceUploadedBundle {
		if err := extractStaticBundle(bytes.NewReader(in.Content), int64(len(in.Content)), filename, contentDir); err != nil {
			_ = os.RemoveAll(assetDir)
			return StaticAsset{}, err
		}
	} else if err := os.WriteFile(filepath.Join(contentDir, filename), in.Content, 0o644); err != nil {
		_ = os.RemoveAll(assetDir)
		return StaticAsset{}, err
	}
	sum := sha256.Sum256(in.Content)
	now := time.Now().UTC()
	createdAt := now
	if previous := session.Assets[assetID]; previous != nil && !previous.Asset.CreatedAt.IsZero() {
		createdAt = previous.Asset.CreatedAt
	}
	asset := StaticAsset{ID: assetID, Name: name, Kind: kind, Filename: filename, Size: int64(len(in.Content)), SHA256: hex.EncodeToString(sum[:]), CreatedAt: createdAt, UpdatedAt: now}
	s.sessionMu.Lock()
	session.Assets[assetID] = &stagedFacilityAsset{Asset: asset, Dir: assetDir}
	session.UpdatedAt = now
	session.ExpiresAt = now.Add(30 * time.Minute)
	s.sessionMu.Unlock()
	_ = ctx
	return asset, nil
}

func (s *Service) DeleteSaveSessionAsset(ctx context.Context, sessionID string, in StaticAssetDeleteInput) error {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return err
	}
	assetID := strings.TrimSpace(in.AssetID)
	if assetID == "" {
		return panelerr.NotFound("static asset")
	}
	s.sessionMu.Lock()
	staged := session.Assets[assetID]
	if staged != nil {
		delete(session.Assets, assetID)
		session.UpdatedAt = time.Now().UTC()
		session.ExpiresAt = session.UpdatedAt.Add(30 * time.Minute)
	}
	s.sessionMu.Unlock()
	if staged == nil {
		return panelerr.NotFound("static asset")
	}
	if staged.Dir != "" {
		_ = os.RemoveAll(staged.Dir)
	}
	_ = ctx
	return nil
}

func (s *Service) CommitSaveSession(ctx context.Context, sessionID string, in CommitSaveSessionInput) (SaveSessionCommitResult, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return SaveSessionCommitResult{}, err
	}
	current, err := s.loadConfig(ctx)
	if err != nil {
		return SaveSessionCommitResult{}, err
	}
	next, err := normalizeInput(in.Save)
	if err != nil {
		return SaveSessionCommitResult{}, err
	}
	if err := s.validatePanelHost(ctx, next); err != nil {
		return SaveSessionCommitResult{}, err
	}
	if err := validateSessionAssetReferences(next, session.Assets); err != nil {
		return SaveSessionCommitResult{}, err
	}
	if err := s.validateRouteConflicts(ctx, next); err != nil {
		return SaveSessionCommitResult{}, err
	}
	rollbackFiles, err := s.installSessionAssets(session)
	if err != nil {
		return SaveSessionCommitResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		rollbackFiles()
		return SaveSessionCommitResult{}, err
	}
	if err := replaceStaticAssetsTx(ctx, tx, session.Assets); err != nil {
		_ = tx.Rollback()
		rollbackFiles()
		return SaveSessionCommitResult{}, err
	}
	if err := saveConfigTxIfVersion(ctx, tx, next, session.BaseVersion); err != nil {
		_ = tx.Rollback()
		rollbackFiles()
		return SaveSessionCommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		rollbackFiles()
		return SaveSessionCommitResult{}, err
	}
	applyRequested := true
	if err := s.syncReverseProxyTraits(ctx, current.DeploymentServers, next.DeploymentServers); err != nil {
		applyRequested = false
		_ = s.setLastError(ctx, err.Error())
	} else if err := s.triggerReverseProxyReconcile(ctx, "facility_app", removedServers(current.DeploymentServers, next.DeploymentServers)); err != nil {
		applyRequested = false
		_ = s.setLastError(ctx, err.Error())
	}
	s.discardSaveSession(sessionID)
	config, err := s.GetReverseProxy(ctx)
	if err != nil {
		return SaveSessionCommitResult{}, err
	}
	return SaveSessionCommitResult{Config: config, ApplyRequested: applyRequested}, nil
}

func (s *Service) DiscardSaveSession(sessionID string) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	s.discardSaveSession(sessionID)
}

func (s *Service) getSaveSession(sessionID string) (*facilitySaveSession, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session := s.saveSessions[strings.TrimSpace(sessionID)]
	if session == nil {
		return nil, panelerr.NotFound("facility reverse proxy save session")
	}
	now := time.Now().UTC()
	if now.After(session.ExpiresAt) {
		delete(s.saveSessions, session.ID)
		_ = os.RemoveAll(session.Dir)
		return nil, panelerr.NotFound("facility reverse proxy save session")
	}
	session.UpdatedAt = now
	session.ExpiresAt = now.Add(30 * time.Minute)
	return session, nil
}

func (s *Service) discardSaveSession(sessionID string) {
	s.sessionMu.Lock()
	session := s.saveSessions[strings.TrimSpace(sessionID)]
	delete(s.saveSessions, strings.TrimSpace(sessionID))
	s.sessionMu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.Dir)
	}
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
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	expired := []*facilitySaveSession{}
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
	entries, err := os.ReadDir(s.sessionDir)
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
		_ = os.RemoveAll(filepath.Join(s.sessionDir, entry.Name()))
	}
}

func (session *facilitySaveSession) result() SaveSessionResult {
	assets := make([]StaticAsset, 0, len(session.Assets))
	for _, staged := range session.Assets {
		assets = append(assets, staged.Asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].CreatedAt.After(assets[j].CreatedAt) })
	return SaveSessionResult{ID: session.ID, ExpiresAt: session.ExpiresAt, Assets: assets}
}

func validateSessionAssetReferences(cfg ReverseProxyConfig, assets map[string]*stagedFacilityAsset) error {
	for _, domain := range cfg.Domains {
		for _, routePath := range domain.Paths {
			if routePath.SourceType != StaticSourceUploadedFile && routePath.SourceType != StaticSourceUploadedBundle {
				continue
			}
			staged := assets[routePath.AssetID]
			if staged == nil {
				return panelerr.Validation("facility_static_site_asset_required", "Static site asset is required")
			}
			if staged.Asset.Kind != routePath.SourceType {
				return panelerr.Validation("facility_static_site_asset_kind_invalid", "Static site asset kind does not match its source")
			}
		}
	}
	return nil
}

func sameConfigVersion(current, base time.Time) bool {
	if current.IsZero() && base.IsZero() {
		return true
	}
	return current.Equal(base)
}

func (s *Service) installSessionAssets(session *facilitySaveSession) (func(), error) {
	backupDir := filepath.Join(session.Dir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return nil, err
	}
	existing, err := s.ListStaticAssets(context.Background())
	if err != nil {
		return nil, err
	}
	moves := [][2]string{}
	rollback := func() {
		for i := len(moves) - 1; i >= 0; i-- {
			_ = os.MkdirAll(filepath.Dir(moves[i][0]), 0o700)
			_ = os.Rename(moves[i][1], moves[i][0])
		}
	}
	move := func(from, to string) error {
		if _, err := os.Stat(from); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
		moves = append(moves, [2]string{from, to})
		return nil
	}
	for _, asset := range existing {
		staged := session.Assets[asset.ID]
		if staged != nil && staged.Dir == "" {
			continue
		}
		if err := move(s.staticAssetDir(asset.ID), filepath.Join(backupDir, asset.ID)); err != nil {
			rollback()
			return nil, err
		}
	}
	for assetID, staged := range session.Assets {
		if staged.Dir == "" {
			continue
		}
		if err := move(staged.Dir, s.staticAssetDir(assetID)); err != nil {
			rollback()
			return nil, err
		}
	}
	return rollback, nil
}

func replaceStaticAssetsTx(ctx context.Context, tx *sql.Tx, assets map[string]*stagedFacilityAsset) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM facility_static_assets`); err != nil {
		return err
	}
	items := make([]StaticAsset, 0, len(assets))
	for _, staged := range assets {
		items = append(items, staged.Asset)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, asset := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO facility_static_assets(id,name,kind,filename,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, asset.ID, asset.Name, asset.Kind, asset.Filename, asset.Size, asset.SHA256, formatTime(asset.CreatedAt), formatTime(asset.UpdatedAt)); err != nil {
			return err
		}
	}
	return nil
}

func saveConfigTx(ctx context.Context, tx *sql.Tx, cfg ReverseProxyConfig) error {
	serversRaw, err := json.Marshal(cfg.DeploymentServers)
	if err != nil {
		return err
	}
	panelRaw, err := json.Marshal(cfg.PanelEntry)
	if err != nil {
		return err
	}
	domainsRaw, err := json.Marshal(cfg.Domains)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,panel_entry_json,domains_json,last_error,updated_at)
		VALUES(?,1,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET version=facility_app_configs.version+1,deployment_server_ids_json=excluded.deployment_server_ids_json,panel_entry_json=excluded.panel_entry_json,domains_json=excluded.domains_json,last_error=excluded.last_error,updated_at=excluded.updated_at`, ReverseProxyID, string(serversRaw), string(panelRaw), string(domainsRaw), cfg.LastError, now)
	return err
}

func saveConfigTxIfVersion(ctx context.Context, tx *sql.Tx, cfg ReverseProxyConfig, expectedVersion int) error {
	serversRaw, err := json.Marshal(cfg.DeploymentServers)
	if err != nil {
		return err
	}
	panelRaw, err := json.Marshal(cfg.PanelEntry)
	if err != nil {
		return err
	}
	domainsRaw, err := json.Marshal(cfg.Domains)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var result sql.Result
	if expectedVersion == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,panel_entry_json,domains_json,last_error,updated_at) VALUES(?,1,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, ReverseProxyID, string(serversRaw), string(panelRaw), string(domainsRaw), cfg.LastError, now)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE facility_app_configs SET version=version+1,deployment_server_ids_json=?,panel_entry_json=?,domains_json=?,last_error=?,updated_at=? WHERE id=? AND version=?`, string(serversRaw), string(panelRaw), string(domainsRaw), cfg.LastError, now, ReverseProxyID, expectedVersion)
	}
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return panelerr.Conflict("resource_version_conflict", "facility configuration changed while editing")
	}
	return nil
}
