package dns

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/secrets"
)

func TestDomainListRedactsCredentialsAndResolveKeepsToken(t *testing.T) {
	svc, closeStore := newDomainTestService(t)
	defer closeStore()

	domain, err := svc.CreateDomain(context.Background(), SaveDomainRequest{Name: "example.com", Provider: ProviderCloudflare, APIToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "example.com" {
		t.Fatalf("domains = %#v", rows)
	}
	resolved, err := svc.ResolveDomain(context.Background(), domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIToken != "secret" {
		t.Fatalf("resolved API token was not preserved")
	}
	var ciphertext string
	if err := svc.db.QueryRow(`SELECT provider_secret_ciphertext FROM dns_domains WHERE id=?`, domain.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || strings.Contains(ciphertext, "secret") {
		t.Fatalf("provider credentials were not encrypted: %q", ciphertext)
	}
}

func TestCreateDomainValidatesProviderBeforePersisting(t *testing.T) {
	svc, closeStore := newDomainTestService(t)
	defer closeStore()
	svc.providerFactory = func(resolved ResolvedDomain) Provider {
		if resolved.Name != "example.com" || resolved.APIToken != "bad-token" {
			t.Fatalf("unexpected validation domain = %#v", resolved)
		}
		return &fakeDNSProvider{listErr: errors.New("cloudflare token rejected")}
	}

	_, err := svc.CreateDomain(context.Background(), SaveDomainRequest{Name: "example.com", Provider: ProviderCloudflare, APIToken: "bad-token"})
	if err == nil {
		t.Fatal("expected provider validation error")
	}
	rows, err := svc.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("domain should not be persisted when provider validation fails: %#v", rows)
	}
}

func TestRecordOperationsUseResolvedCloudflareProvider(t *testing.T) {
	svc, closeStore := newDomainTestService(t)
	defer closeStore()

	domain, err := svc.CreateDomain(context.Background(), SaveDomainRequest{Name: "example.com", Provider: ProviderCloudflare, APIToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeDNSProvider{records: []Record{{ID: "rec_1", Name: "www.example.com", Type: "A", Value: "192.0.2.1", TTL: 120}}}
	svc.providerFactory = func(resolved ResolvedDomain) Provider {
		if resolved.APIToken != "secret" {
			t.Fatalf("resolved provider credentials = %#v", resolved)
		}
		return fake
	}
	if err := svc.refreshRecords(context.Background(), domain.ID); err != nil {
		t.Fatal(err)
	}

	records, err := svc.ListRecords(context.Background(), domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != "rec_1" {
		t.Fatalf("records = %#v", records)
	}
	proxied := true
	created, err := svc.CreateRecord(context.Background(), domain.ID, RecordInput{Name: "app", Type: "a", Value: "192.0.2.2", TTL: 1, Proxied: &proxied})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "app" || created.Type != "A" || !created.Proxied {
		t.Fatalf("created = %#v", created)
	}
	updated, err := svc.UpdateRecord(context.Background(), domain.ID, "rec_1", RecordInput{Name: "www", Type: "CNAME", Value: "target.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != "rec_1" || updated.Type != "CNAME" {
		t.Fatalf("updated = %#v", updated)
	}
	if err := svc.DeleteRecord(context.Background(), domain.ID, "rec_1"); err != nil {
		t.Fatal(err)
	}
	if fake.deletedID != "rec_1" {
		t.Fatalf("deletedID = %q", fake.deletedID)
	}
}

func TestRecordInputValidation(t *testing.T) {
	tests := []struct {
		name string
		in   RecordInput
	}{
		{name: "bad name", in: RecordInput{Name: "-bad", Type: "A", Value: "192.0.2.1"}},
		{name: "bad type", in: RecordInput{Name: "www", Type: "SOA", Value: "ns.example.com"}},
		{name: "empty value", in: RecordInput{Name: "www", Type: "A", Value: ""}},
		{name: "negative ttl", in: RecordInput{Name: "www", Type: "A", Value: "192.0.2.1", TTL: -1}},
		{name: "proxied unsupported", in: RecordInput{Name: "txt", Type: "TXT", Value: "hello", Proxied: boolPtr(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := validateRecordInput(tt.in); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func newDomainTestService(t *testing.T) (*Service, func()) {
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
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), secrets)
	svc.providerFactory = func(ResolvedDomain) Provider { return &fakeDNSProvider{} }
	return svc, func() { _ = store.Close() }
}

type fakeDNSProvider struct {
	records   []Record
	deletedID string
	listErr   error
}

func (p *fakeDNSProvider) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.records, nil
}

func (p *fakeDNSProvider) CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error) {
	return Record{ID: "rec_new", Name: record.Name, Type: record.Type, Value: record.Value, TTL: record.TTL, Proxied: record.Proxied != nil && *record.Proxied}, nil
}

func (p *fakeDNSProvider) UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error) {
	return Record{ID: id, Name: record.Name, Type: record.Type, Value: record.Value, TTL: record.TTL, Proxied: record.Proxied != nil && *record.Proxied}, nil
}

func (p *fakeDNSProvider) DeleteRecord(ctx context.Context, zone string, id string) error {
	p.deletedID = id
	return nil
}
