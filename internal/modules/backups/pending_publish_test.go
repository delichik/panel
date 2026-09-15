package backups

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPublishPendingRestoreReplacesWidePermissionPairAsStrictAtomicGroup(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	oldDir := pendingDir(cfg.DataRoot)
	if err := os.MkdirAll(oldDir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(oldDir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "old.panel-backup"), []byte("old"), 0666); err != nil {
		t.Fatal(err)
	}
	oldMarker, _ := json.Marshal(pendingRestore{ArchiveFilename: "old.panel-backup", CreatedAt: time.Now().UTC()})
	if err := os.WriteFile(filepath.Join(oldDir, "pending.json"), oldMarker, 0666); err != nil {
		t.Fatal(err)
	}

	archive := []byte("new archive")
	marker := pendingRestore{
		ArchiveFilename: "backup.panel-backup", CreatedAt: time.Now().UTC(),
		MaintenanceAuth: &maintenanceCredential{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash},
	}
	marker.ArchiveSHA256 = digestBytes(archive)
	marker.ArchiveSize = int64(len(archive))
	markerRaw, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishPendingRestore(cfg.DataRoot, marker.ArchiveFilename, archive, markerRaw, nil); err != nil {
		t.Fatal(err)
	}
	assertPrivateMode(t, pendingDir(cfg.DataRoot), 0700)
	assertPrivateMode(t, filepath.Join(pendingDir(cfg.DataRoot), "pending.json"), 0600)
	assertPrivateMode(t, filepath.Join(pendingDir(cfg.DataRoot), marker.ArchiveFilename), 0600)
	gotMarker, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if gotMarker.ArchiveFilename != marker.ArchiveFilename {
		t.Fatalf("published marker points to %q", gotMarker.ArchiveFilename)
	}
	raw, err := os.ReadFile(filepath.Join(pendingDir(cfg.DataRoot), gotMarker.ArchiveFilename))
	if err != nil || string(raw) != string(archive) {
		t.Fatalf("published archive mismatch: %q err=%v", raw, err)
	}
}

func TestInterruptedPendingPublicationRestoresPreviousConsistentPair(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	oldDir := pendingDir(cfg.DataRoot)
	if err := os.MkdirAll(oldDir, 0700); err != nil {
		t.Fatal(err)
	}
	oldArchive := []byte("old archive")
	if err := os.WriteFile(filepath.Join(oldDir, "old.panel-backup"), oldArchive, 0600); err != nil {
		t.Fatal(err)
	}
	oldMarker := pendingRestore{ArchiveFilename: "old.panel-backup", CreatedAt: time.Now().UTC()}
	oldMarkerRaw, _ := json.Marshal(oldMarker)
	if err := os.WriteFile(filepath.Join(oldDir, "pending.json"), oldMarkerRaw, 0600); err != nil {
		t.Fatal(err)
	}

	newArchive := []byte("new archive")
	newMarker := pendingRestore{
		ArchiveFilename: "new.panel-backup", CreatedAt: time.Now().UTC(),
		MaintenanceAuth: &maintenanceCredential{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash},
	}
	newMarker.ArchiveSHA256 = digestBytes(newArchive)
	newMarker.ArchiveSize = int64(len(newArchive))
	newMarkerRaw, _ := json.Marshal(newMarker)
	err := publishPendingRestore(cfg.DataRoot, newMarker.ArchiveFilename, newArchive, newMarkerRaw, func(step string) error {
		if step == "after_backup_previous" {
			return errSimulatedPublishCrash
		}
		return nil
	})
	if !errors.Is(err, errSimulatedPublishCrash) {
		t.Fatalf("publish interruption error = %v", err)
	}
	resolved, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ArchiveFilename != oldMarker.ArchiveFilename {
		t.Fatalf("resolved marker points to %q, want previous pair", resolved.ArchiveFilename)
	}
	raw, err := os.ReadFile(filepath.Join(pendingDir(cfg.DataRoot), resolved.ArchiveFilename))
	if err != nil || string(raw) != string(oldArchive) {
		t.Fatalf("resolved previous archive mismatch: %q err=%v", raw, err)
	}
}

func digestBytes(raw []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func TestResolvePendingPublicationPrefersValidPreviousOverTornTarget(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	target := pendingDir(cfg.DataRoot)
	previous := pendingPreviousDir(cfg.DataRoot)
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	tornMarker, _ := json.Marshal(pendingRestore{ArchiveFilename: "missing.panel-backup"})
	if err := os.WriteFile(filepath.Join(target, "pending.json"), tornMarker, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(previous, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previous, "previous.panel-backup"), []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	previousMarker, _ := json.Marshal(pendingRestore{ArchiveFilename: "previous.panel-backup"})
	if err := os.WriteFile(filepath.Join(previous, "pending.json"), previousMarker, 0600); err != nil {
		t.Fatal(err)
	}
	marker, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if marker.ArchiveFilename != "previous.panel-backup" {
		t.Fatalf("resolved marker = %q, want valid previous pair", marker.ArchiveFilename)
	}
	if pathExists(previous) {
		t.Fatal("valid previous directory should be promoted atomically")
	}
}

func TestResolvePendingPublicationRejectsDigestMismatchedTargetBeforeDroppingPrevious(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	target := pendingDir(cfg.DataRoot)
	previous := pendingPreviousDir(cfg.DataRoot)
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	wrongArchive := []byte("old archive under new marker")
	if err := os.WriteFile(filepath.Join(target, "target.panel-backup"), wrongArchive, 0600); err != nil {
		t.Fatal(err)
	}
	targetMarker := pendingRestore{ArchiveFilename: "target.panel-backup", ArchiveSHA256: digestBytes([]byte("expected new archive")), ArchiveSize: int64(len(wrongArchive))}
	targetRaw, _ := json.Marshal(targetMarker)
	if err := os.WriteFile(filepath.Join(target, "pending.json"), targetRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(previous, 0700); err != nil {
		t.Fatal(err)
	}
	previousArchive := []byte("last consistent archive")
	if err := os.WriteFile(filepath.Join(previous, "previous.panel-backup"), previousArchive, 0600); err != nil {
		t.Fatal(err)
	}
	previousMarker := pendingRestore{ArchiveFilename: "previous.panel-backup"}
	previousRaw, _ := json.Marshal(previousMarker)
	if err := os.WriteFile(filepath.Join(previous, "pending.json"), previousRaw, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ArchiveFilename != previousMarker.ArchiveFilename || resolved.ArchiveSHA256 != digestBytes(previousArchive) {
		t.Fatalf("digest-mismatched target replaced valid previous: %+v", resolved)
	}
}

func TestPendingValidationRejectsArchiveTraversalAndNonRegularFiles(t *testing.T) {
	if safePendingArchiveFilename("../outside.panel-backup") || safePendingArchiveFilename(`..\\outside.panel-backup`) {
		t.Fatal("archive filename traversal was accepted")
	}
	dir := t.TempDir()
	marker, _ := json.Marshal(pendingRestore{ArchiveFilename: "archive.panel-backup"})
	if err := os.WriteFile(filepath.Join(dir, "pending.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "archive.panel-backup"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := validatePendingDirectory(dir); err == nil {
		t.Fatal("directory masquerading as restore archive was accepted")
	}
}

func assertPrivateMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
