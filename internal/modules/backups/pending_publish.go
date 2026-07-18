package backups

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var errSimulatedPublishCrash = errors.New("simulated restore pending publish crash")

func pendingPreviousDir(dataRoot string) string {
	return filepath.Join(filepath.Dir(pendingDir(dataRoot)), "restore-pending.previous")
}

func resolvePendingPublication(dataRoot string) error {
	target := pendingDir(dataRoot)
	previous := pendingPreviousDir(dataRoot)
	if pathExists(target) {
		targetErr := validatePendingDirectory(target)
		if targetErr == nil {
			if pathExists(previous) && !pendingDirectoryHasDigest(target) && validatePendingDirectory(previous) == nil {
				if err := securePendingPermissions(previous); err != nil {
					return err
				}
				if err := os.RemoveAll(target); err != nil {
					return err
				}
				if err := os.Rename(previous, target); err != nil {
					return err
				}
				return syncDirectory(filepath.Dir(target))
			}
			if err := securePendingPermissions(target); err != nil {
				return err
			}
			if pathExists(previous) {
				if err := os.RemoveAll(previous); err != nil {
					return err
				}
			}
			return nil
		}
		if pathExists(previous) && validatePendingDirectory(previous) == nil {
			if err := securePendingPermissions(previous); err != nil {
				return err
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			if err := os.Rename(previous, target); err != nil {
				return err
			}
			return syncDirectory(filepath.Dir(target))
		}
		return targetErr
	}
	if pathExists(previous) {
		if err := validatePendingDirectory(previous); err != nil {
			return err
		}
		if err := securePendingPermissions(previous); err != nil {
			return err
		}
		if err := os.Rename(previous, target); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	}
	return nil
}

func validatePendingDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("restore pending path is not a directory")
	}
	markerPath := filepath.Join(dir, "pending.json")
	markerInfo, err := os.Lstat(markerPath)
	if err != nil {
		return err
	}
	if !markerInfo.Mode().IsRegular() {
		return errors.New("restore pending marker is not a regular file")
	}
	markerRaw, err := os.ReadFile(markerPath)
	if err != nil {
		return err
	}
	var marker pendingRestore
	if err := json.Unmarshal(markerRaw, &marker); err != nil {
		return err
	}
	if !safePendingArchiveFilename(marker.ArchiveFilename) {
		return errors.New("restore pending archive filename is invalid")
	}
	archiveInfo, err := os.Lstat(filepath.Join(dir, marker.ArchiveFilename))
	if err != nil {
		return err
	}
	if !archiveInfo.Mode().IsRegular() {
		return errors.New("restore pending archive is not a regular file")
	}
	if marker.ArchiveSHA256 != "" || marker.ArchiveSize != 0 {
		if marker.ArchiveSHA256 == "" || marker.ArchiveSize != archiveInfo.Size() {
			return errors.New("restore pending archive size does not match marker")
		}
		digest, err := fileSHA256(filepath.Join(dir, marker.ArchiveFilename))
		if err != nil {
			return err
		}
		if digest != marker.ArchiveSHA256 {
			return errors.New("restore pending archive digest does not match marker")
		}
	}
	return nil
}

func pendingDirectoryHasDigest(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "pending.json"))
	if err != nil {
		return false
	}
	var marker pendingRestore
	return json.Unmarshal(raw, &marker) == nil && marker.ArchiveSHA256 != "" && marker.ArchiveSize > 0
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func upgradeLegacyPendingIntegrity(dir string, marker *pendingRestore) error {
	if marker.ArchiveSHA256 != "" && marker.ArchiveSize > 0 {
		return nil
	}
	archivePath := filepath.Join(dir, marker.ArchiveFilename)
	info, err := os.Lstat(archivePath)
	if err != nil {
		return err
	}
	digest, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	marker.ArchiveSHA256 = digest
	marker.ArchiveSize = info.Size()
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(dir, ".pending.json.upgrade")
	_ = os.Remove(tmpPath)
	if err := writeExclusiveSyncedFile(tmpPath, raw); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, "pending.json")); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return syncDirectory(dir)
}

func safePendingArchiveFilename(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`)
}

func publishPendingRestore(dataRoot, archiveName string, archive, marker []byte, hook func(string) error) error {
	parent := filepath.Dir(pendingDir(dataRoot))
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0700); err != nil {
		return err
	}
	if err := resolvePendingPublication(dataRoot); err != nil {
		return err
	}
	tempDir, err := os.MkdirTemp(parent, ".restore-pending-publish-")
	if err != nil {
		return err
	}
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.RemoveAll(tempDir)
		}
	}()
	if err := os.Chmod(tempDir, 0700); err != nil {
		return err
	}
	if err := writeExclusiveSyncedFile(filepath.Join(tempDir, archiveName), archive); err != nil {
		return err
	}
	if err := writeExclusiveSyncedFile(filepath.Join(tempDir, "pending.json"), marker); err != nil {
		return err
	}
	if err := validatePendingPublication(tempDir, archiveName, archive); err != nil {
		return err
	}
	if err := syncDirectory(tempDir); err != nil {
		return err
	}
	target := pendingDir(dataRoot)
	previous := pendingPreviousDir(dataRoot)
	hadPrevious := pathExists(target)
	if hadPrevious {
		if err := securePendingPermissions(target); err != nil {
			return err
		}
		_ = os.RemoveAll(previous)
		if err := os.Rename(target, previous); err != nil {
			return err
		}
		if err := syncDirectory(parent); err != nil {
			return err
		}
		if hook != nil {
			if err := hook("after_backup_previous"); err != nil {
				if errors.Is(err, errSimulatedPublishCrash) {
					return err
				}
				_ = os.Rename(previous, target)
				return err
			}
		}
	}
	if hook != nil {
		if err := hook("before_publish"); err != nil {
			if hadPrevious && !errors.Is(err, errSimulatedPublishCrash) {
				_ = os.Rename(previous, target)
			}
			return err
		}
	}
	if err := os.Rename(tempDir, target); err != nil {
		if hadPrevious {
			_ = os.Rename(previous, target)
		}
		return err
	}
	cleanupTemp = false
	if err := syncDirectory(parent); err != nil {
		return err
	}
	if err := securePendingPermissions(target); err != nil {
		return err
	}
	if pathExists(previous) {
		if err := os.RemoveAll(previous); err != nil {
			return err
		}
	}
	return syncDirectory(parent)
}

func writeExclusiveSyncedFile(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func validatePendingPublication(dir, archiveName string, expectedArchive []byte) error {
	if err := validatePendingDirectory(dir); err != nil {
		return err
	}
	markerRaw, err := os.ReadFile(filepath.Join(dir, "pending.json"))
	if err != nil {
		return err
	}
	var marker pendingRestore
	if err := json.Unmarshal(markerRaw, &marker); err != nil {
		return err
	}
	if marker.ArchiveFilename != archiveName || marker.MaintenanceAuth == nil || !validMaintenanceCredential(*marker.MaintenanceAuth) {
		return errors.New("restore pending marker is incomplete")
	}
	if marker.ArchiveSHA256 == "" || marker.ArchiveSize != int64(len(expectedArchive)) {
		return errors.New("restore pending marker archive digest is missing")
	}
	archive, err := os.ReadFile(filepath.Join(dir, archiveName))
	if err != nil {
		return err
	}
	if !bytes.Equal(archive, expectedArchive) {
		return fmt.Errorf("restore pending archive verification failed")
	}
	return nil
}

func securePendingPermissions(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("restore pending path is not a directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return os.Chmod(path, 0700)
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("restore pending contains a non-regular file")
		}
		return os.Chmod(path, 0600)
	})
}
