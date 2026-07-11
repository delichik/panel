package backups

import (
	"os"
	"path/filepath"
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
		LogDatabase:    logDB,
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
