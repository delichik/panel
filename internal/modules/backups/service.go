package backups

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	panelerr "panel/internal/platform/errors"
)

var (
	errPasswordRequired   = errors.New("backup password required")
	errPasswordInvalid    = errors.New("backup password invalid")
	errArchiveInvalid     = errors.New("backup archive invalid")
	errArchiveUnsupported = errors.New("backup archive version unsupported")
)

type Service struct {
	cfg                ArchiveConfig
	restarter          Restarter
	restorePublishHook func(string) error
}

func withRestorePublishHook(hook func(string) error) Option {
	return func(s *Service) { s.restorePublishHook = hook }
}

type Option func(*Service)

func WithRestarter(restarter Restarter) Option {
	return func(s *Service) { s.restarter = restarter }
}

func NewService(cfg ArchiveConfig, opts ...Option) *Service {
	s := &Service{cfg: cfg, restarter: NewPanelInitRestarter(cfg.DataRoot)}
	for _, opt := range opts {
		opt(s)
	}
	if s.restarter == nil {
		s.restarter = noopRestarter{}
	}
	return s
}

func (s *Service) StartExport(_ context.Context, req ExportRequest) (ExportResponse, error) {
	exportID := time.Now().UTC().Format("20060102T150405Z")
	if err := writePendingExport(s.cfg.DataRoot, pendingExport{
		ExportID:  exportID,
		CreatedAt: time.Now().UTC(),
		Encrypt:   req.Encrypt,
	}); err != nil {
		return ExportResponse{}, err
	}
	restartSupported := s.restarter.Supported()
	if restartSupported {
		s.restarter.RestartSoon(MaintenanceModeExport)
	}
	return ExportResponse{ExportID: exportID, RestartSupported: restartSupported}, nil
}

func writePendingExport(dataRoot string, marker pendingExport) error {
	dir := exportPendingDir(dataRoot)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "pending.json")
	tmp, err := os.CreateTemp(dir, ".pending.json.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	tmpPath = ""
	return syncDirectory(dir)
}

func readPendingExport(dataRoot string) (pendingExport, error) {
	raw, err := os.ReadFile(filepath.Join(exportPendingDir(dataRoot), "pending.json"))
	if err != nil {
		return pendingExport{}, err
	}
	var marker pendingExport
	if err := json.Unmarshal(raw, &marker); err != nil {
		return pendingExport{}, err
	}
	return marker, nil
}

func (s *Service) PreflightRestore(filePath, password string) (RestorePreflightResponse, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return RestorePreflightResponse{}, err
	}
	encrypted := isEncryptedBackup(raw)
	manifest, _, err := readManifest(raw, password)
	if err != nil {
		return RestorePreflightResponse{}, mapArchiveError(err)
	}
	manifest.Encrypted = encrypted
	return RestorePreflightResponse{
		Manifest:         manifest,
		Encrypted:        encrypted,
		PasswordRequired: encrypted,
	}, nil
}

func (s *Service) SavePendingRestore(uploadedPath, password string) (RestoreConfirmResponse, error) {
	raw, err := os.ReadFile(uploadedPath)
	if err != nil {
		return RestoreConfirmResponse{}, err
	}
	manifest, _, err := readManifest(raw, password)
	if err != nil {
		return RestoreConfirmResponse{}, mapArchiveError(err)
	}
	manifest.Encrypted = isEncryptedBackup(raw)
	credential, err := readMaintenanceCredential(context.Background(), s.cfg.AppDatabase)
	if err != nil {
		return RestoreConfirmResponse{}, err
	}
	archiveName := "backup.panel-backup"
	marker := pendingRestore{
		ArchiveFilename: archiveName,
		ArchiveSHA256:   fmt.Sprintf("%x", sha256.Sum256(raw)),
		ArchiveSize:     int64(len(raw)),
		CreatedAt:       time.Now().UTC(),
		Manifest:        manifest,
		MaintenanceAuth: &credential,
	}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return RestoreConfirmResponse{}, err
	}
	if err := publishPendingRestore(s.cfg.DataRoot, archiveName, raw, markerBytes, s.restorePublishHook); err != nil {
		return RestoreConfirmResponse{}, err
	}
	restartSupported := s.restarter.Supported()
	if restartSupported {
		s.restarter.RestartSoon(MaintenanceModeRestore)
	}
	return RestoreConfirmResponse{Pending: true, RestartSupported: restartSupported}, nil
}

func pendingDir(dataRoot string) string {
	return filepath.Join(dataRoot, "tmp", "restore-pending")
}

func exportPendingDir(dataRoot string) string {
	return filepath.Join(dataRoot, "tmp", "backup-export-pending")
}

func mapArchiveError(err error) error {
	switch {
	case errors.Is(err, errPasswordRequired):
		return panelerr.BadRequest("restore_password_required", "Backup password is required")
	case errors.Is(err, errPasswordInvalid):
		return panelerr.BadRequest("restore_password_invalid", "Backup password is invalid")
	case errors.Is(err, errArchiveUnsupported):
		return panelerr.BadRequest("restore_compatibility_failed", "Backup archive version is unsupported")
	default:
		return panelerr.BadRequest("restore_archive_invalid", "Backup archive is invalid")
	}
}
