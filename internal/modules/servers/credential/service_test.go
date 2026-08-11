package credential

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/secrets"
)

func newCredentialService(t *testing.T) (*Service, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store.AppDB(), secrets), store
}

func TestCredentialResponsesRedactSecrets(t *testing.T) {
	svc, _ := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{Name: "lab", Type: TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cred)
	if strings.Contains(string(b), "secret") {
		t.Fatalf("credential JSON leaked secret: %s", b)
	}
	resolved, err := svc.Resolve(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Password != "secret" {
		t.Fatal("resolved credential should include secret for SSH module")
	}
}

func TestDeleteCredentialInUseRejected(t *testing.T) {
	svc, store := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{Name: "lab", Type: TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(context.Background(), cred.ID); err == nil {
		t.Fatal("expected credential in use conflict")
	}
}

func TestUpdateCredentialKeepsSecretWhenBlank(t *testing.T) {
	svc, _ := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{Name: "lab", Type: TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(context.Background(), cred.ID, UpdateRequest{Name: "lab2", Type: TypePassword, Username: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "lab2" || updated.Username != "root" {
		t.Fatalf("unexpected updated credential: %#v", updated)
	}
	resolved, err := svc.Resolve(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Password != "secret" {
		t.Fatal("blank password update should keep existing secret")
	}
}

func TestCredentialSecretsAreEncryptedInDatabase(t *testing.T) {
	svc, store := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{
		Name:       "lab",
		Type:       TypePrivateKey,
		Username:   "root",
		PrivateKey: "private-key-secret",
		Passphrase: "passphrase-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext, password, keyPath, passphrase string
	if err := store.AppDB().QueryRow(`SELECT secret_ciphertext,password_secret,private_key_path,passphrase_secret FROM credentials WHERE id=?`, cred.ID).
		Scan(&ciphertext, &password, &keyPath, &passphrase); err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" {
		t.Fatal("credential ciphertext should be stored")
	}
	if strings.Contains(ciphertext, "private-key-secret") || strings.Contains(ciphertext, "passphrase-secret") {
		t.Fatal("credential ciphertext leaked plaintext")
	}
	if password != "" || keyPath != "" || passphrase != "" {
		t.Fatalf("legacy credential fields were populated: password=%q path=%q passphrase=%q", password, keyPath, passphrase)
	}
}

func TestEnsureLegacySecretsMigratedEncryptsAndDeletesPrivateKeyFile(t *testing.T) {
	t.Setenv(secretstore.MasterKeyEnvVar, "")
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	keyPath := filepath.Join(cfg.DataRoot, "keys", "legacy.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("legacy-private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,password_secret,private_key_path,passphrase_secret,created_at,updated_at)
		VALUES('cred_legacy','legacy','private_key','root','',?,'legacy-passphrase','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, keyPath); err != nil {
		t.Fatal(err)
	}
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), secrets)
	if err := svc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy private key file still exists: %v", err)
	}
	resolved, err := svc.Resolve(context.Background(), "cred_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved.PrivateKey) != "legacy-private-key" || resolved.Passphrase != "legacy-passphrase" {
		t.Fatalf("unexpected migrated credential: %#v", resolved)
	}
	var ciphertext, storedPath, storedPassphrase string
	if err := store.AppDB().QueryRow(`SELECT secret_ciphertext,private_key_path,passphrase_secret FROM credentials WHERE id='cred_legacy'`).
		Scan(&ciphertext, &storedPath, &storedPassphrase); err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || storedPath != "" || storedPassphrase != "" {
		t.Fatalf("legacy fields not cleared: ciphertext=%t path=%q passphrase=%q", ciphertext != "", storedPath, storedPassphrase)
	}
	if err := svc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
}

func TestEnsureLegacySecretsMigratedEncryptsPassword(t *testing.T) {
	svc, store := newCredentialService(t)
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,password_secret,created_at,updated_at)
		VALUES('cred_legacy_password','legacy','password','root','legacy-password','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureLegacySecretsMigrated(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.Resolve(context.Background(), "cred_legacy_password")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Password != "legacy-password" {
		t.Fatalf("password = %q", resolved.Password)
	}
	var legacyPassword string
	if err := store.AppDB().QueryRow(`SELECT password_secret FROM credentials WHERE id='cred_legacy_password'`).Scan(&legacyPassword); err != nil {
		t.Fatal(err)
	}
	if legacyPassword != "" {
		t.Fatal("legacy password plaintext was not cleared")
	}
}

func TestCredentialDetailIncludesKeySummary(t *testing.T) {
	svc, _ := newCredentialService(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "deploy@example")
	if err != nil {
		t.Fatal(err)
	}
	cred, err := svc.Create(context.Background(), CreateRequest{
		Name: "prod", Type: TypePrivateKey, Username: "root", PrivateKey: string(pem.EncodeToMemory(block)),
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetWithSummary(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.KeySummary == nil {
		t.Fatal("expected key summary for private key credential")
	}
	if detail.KeySummary.Algorithm != "ED25519" || detail.KeySummary.Bits != 256 {
		t.Fatalf("key summary = %#v, want ED25519/256", detail.KeySummary)
	}
	if detail.KeySummary.Comment != "deploy@example" {
		t.Fatalf("key comment = %q, want deploy@example", detail.KeySummary.Comment)
	}
	if !strings.HasPrefix(detail.KeySummary.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want SHA256 prefix", detail.KeySummary.Fingerprint)
	}
	b, _ := json.Marshal(detail)
	if strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatalf("credential detail leaked private key material: %s", b)
	}
}

func TestCredentialDetailPasswordHasNoKeySummary(t *testing.T) {
	svc, _ := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{Name: "lab", Type: TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetWithSummary(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.KeySummary != nil {
		t.Fatalf("password credential should not carry a key summary: %#v", detail.KeySummary)
	}
}

func TestUpdatePrivateKeyCredentialKeepsPassphraseWhenKeyBlank(t *testing.T) {
	svc, _ := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{
		Name: "prod", Type: TypePrivateKey, Username: "root",
		PrivateKey: "old-private-key", Passphrase: "old-passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Editing metadata without supplying a new private key must preserve both
	// the key and its passphrase.
	updated, err := svc.Update(context.Background(), cred.ID, UpdateRequest{
		Name: "prod2", Type: TypePrivateKey, Username: "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "prod2" || updated.Username != "deploy" {
		t.Fatalf("unexpected updated credential: %#v", updated)
	}
	resolved, err := svc.Resolve(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved.PrivateKey) != "old-private-key" || resolved.Passphrase != "old-passphrase" {
		t.Fatalf("blank private key update overwrote secrets: %#v", resolved)
	}

	// Supplying a new private key is the only way to change the passphrase.
	if _, err := svc.Update(context.Background(), cred.ID, UpdateRequest{
		Name: "prod2", Type: TypePrivateKey, Username: "deploy",
		PrivateKey: "new-private-key", Passphrase: "new-passphrase",
	}); err != nil {
		t.Fatal(err)
	}
	resolved, err = svc.Resolve(context.Background(), cred.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(resolved.PrivateKey) != "new-private-key" || resolved.Passphrase != "new-passphrase" {
		t.Fatalf("new private key update did not overwrite secrets: %#v", resolved)
	}
}

func TestCredentialDetailOmitsKeySummaryWhenDecryptFails(t *testing.T) {
	svc, store := newCredentialService(t)
	cred, err := svc.Create(context.Background(), CreateRequest{
		Name: "prod", Type: TypePrivateKey, Username: "root", PrivateKey: "some-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE credentials SET secret_ciphertext='corrupt' WHERE id=?`, cred.ID); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.GetWithSummary(context.Background(), cred.ID)
	if err != nil {
		t.Fatalf("expected metadata to still be returned when decrypt fails: %v", err)
	}
	if detail.ID != cred.ID || detail.Name != "prod" || detail.Username != "root" {
		t.Fatalf("unexpected credential detail: %#v", detail)
	}
	if detail.KeySummary != nil {
		t.Fatalf("expected key summary to be omitted, got %#v", detail.KeySummary)
	}
}