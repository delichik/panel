package secretstore

import (
	"os"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/storage"
)

func TestOpenGeneratesMasterKeyAndEncryptsFields(t *testing.T) {
	t.Setenv(MasterKeyEnvVar, "")
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	secrets, err := Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(MasterKeyPath(cfg.DataRoot)); err != nil {
		t.Fatalf("expected master key file: %v", err)
	}
	ciphertext, err := secrets.Encrypt("asset-1", "tls_certificate", "private_key", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := secrets.Decrypt("asset-1", "tls_certificate", "private_key", ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := secrets.Decrypt("asset-2", "tls_certificate", "private_key", ciphertext); err == nil {
		t.Fatal("expected associated data mismatch to fail decryption")
	}
}

func TestOpenRejectsMismatchedEnvKey(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Encrypt("asset-1", "tls_certificate", "private_key", []byte("secret")); err != nil {
		t.Fatal(err)
	}
	t.Setenv(MasterKeyEnvVar, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if _, err := Open(cfg, store.AppDB()); err == nil {
		t.Fatal("expected mismatched env master key to fail")
	}
}
