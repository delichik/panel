package backups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(dataRoot, "secrets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "secrets", "key-assets-master.key"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	appDB := filepath.Join(dataRoot, "db", "app.db")
	logDB := filepath.Join(dataRoot, "db", "log.db")
	metricsDB := filepath.Join(dataRoot, "db", "metrics.db")
	for _, path := range []string{appDB, logDB, metricsDB} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0600); err != nil {
			t.Fatal(err)
		}
	}

	raw, manifest, err := buildArchive(ArchiveConfig{
		DataRoot:        dataRoot,
		AppDatabase:     appDB,
		LogDatabase:     logDB,
		MetricsDatabase: metricsDB,
		PanelVersion:    "test",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Encrypted {
		t.Fatal("plain archive manifest should not be encrypted")
	}
	got, plain, err := readManifest(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.PanelVersion != "test" || len(plain) == 0 {
		t.Fatalf("unexpected manifest: %+v", got)
	}
	staging := filepath.Join(dir, "staging")
	if err := extractArchive(plain, staging, got); err != nil {
		t.Fatalf("extract with verification: %v", err)
	}
}

func TestExtractArchiveRejectsTamperedMemberHash(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, manifest, err := buildArchive(ArchiveConfig{DataRoot: dataRoot, PanelVersion: "test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("expected archived files")
	}
	manifest.Files[0].SHA256 = strings.Repeat("0", 64)
	if err := extractArchive(raw, filepath.Join(dir, "staging"), manifest); err == nil {
		t.Fatal("expected hash mismatch to fail extract")
	}
}

func TestClearSQLiteSidecarsRemovesWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	if err := os.WriteFile(dbPath, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	wal := dbPath + "-wal"
	if err := os.WriteFile(wal, []byte("wal"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := clearSQLiteSidecars(dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wal); !os.IsNotExist(err) {
		t.Fatalf("wal should be removed, err=%v", err)
	}
}

func TestEncryptedBackupArchiveRequiresPassword(t *testing.T) {
	raw := []byte("plain backup")
	encrypted, err := encryptBytes(raw, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !isEncryptedBackup(encrypted) {
		t.Fatal("archive should be encrypted")
	}
	if _, err := decryptBytes(encrypted, ""); err != errPasswordRequired {
		t.Fatalf("expected password required, got %v", err)
	}
	if _, err := decryptBytes(encrypted, "wrong"); err != errPasswordInvalid {
		t.Fatalf("expected invalid password, got %v", err)
	}
	plain, err := decryptBytes(encrypted, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(raw) {
		t.Fatalf("unexpected plaintext %q", plain)
	}
}

func TestSafeArchivePathRejectsTraversal(t *testing.T) {
	for _, path := range []string{"../x", "/absolute", `dataRoot\secret`, ".", ""} {
		if safeArchivePath(path) {
			t.Fatalf("path %q should be rejected", path)
		}
	}
	if !safeArchivePath("dataRoot/secrets/key") {
		t.Fatal("relative archive path should be allowed")
	}
}

func TestBackupArchiveExcludesPrivateTemporaryDirectories(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "data")
	for _, sub := range []string{"key-assets", "key-asset-exports", "restore-pending", "restore-pending.previous", "backups", "maintenance", "backup-export-pending"} {
		subDir := filepath.Join(dataRoot, "tmp", sub)
		if err := os.MkdirAll(subDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "secret.txt"), []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "normal"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "normal", "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	_, manifest, err := buildArchive(ArchiveConfig{DataRoot: dataRoot, PanelVersion: "test"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range manifest.Files {
		if strings.Contains(file.Path, "tmp/") {
			t.Fatalf("temporary/private directory leaked into archive: %s", file.Path)
		}
	}
	foundKeep := false
	for _, file := range manifest.Files {
		if file.Path == "dataRoot/normal/keep.txt" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatal("normal file missing from archive")
	}
}
