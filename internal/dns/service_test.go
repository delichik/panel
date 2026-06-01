package dns

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/storage"
)

func TestDomainListRedactsAPITokenAndResolveKeepsSecret(t *testing.T) {
	svc, closeStore := newDomainTestService(t)
	defer closeStore()

	domain, err := svc.CreateDomain(context.Background(), SaveDomainRequest{Name: "example.com", Provider: ProviderCloudflare, APIToken: "secret", AccountID: "acct_1"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "example.com" || rows[0].AccountID != "acct_1" {
		t.Fatalf("domains = %#v", rows)
	}
	resolved, err := svc.ResolveDomain(context.Background(), domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIToken != "secret" {
		t.Fatalf("resolved API token was not preserved")
	}
	if resolved.AccountID != "acct_1" {
		t.Fatalf("resolved account id = %q", resolved.AccountID)
	}
}

func newDomainTestService(t *testing.T) (*Service, func()) {
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
	return NewService(store.AppDB()), func() { _ = store.Close() }
}
