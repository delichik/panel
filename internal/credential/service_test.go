package credential

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/config"
	"panel/internal/storage"
)

func newCredentialService(t *testing.T) (*Service, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(store.AppDB(), cfg), store
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
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du',?,'now','now')`, cred.ID)
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
