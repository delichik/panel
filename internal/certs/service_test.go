package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/storage"
)

func TestIssueWildcardCertificateExpandsDomainsAndRegistersBuiltinVariable(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()

	result, err := svc.Issue(context.Background(), IssueRequest{DomainID: "dnsdom_1", Prefix: "@", Scope: ScopeWildcard})
	if err != nil {
		t.Fatal(err)
	}
	if result.Certificate.Scope != ScopeWildcard {
		t.Fatalf("certificate = %#v", result.Certificate)
	}
	want := []string{"example.com", "*.example.com"}
	if !equalStrings(fake.last.Domains, want) || !equalStrings(result.Certificate.Domains, want) {
		t.Fatalf("domains issued=%#v stored=%#v want %#v", fake.last.Domains, result.Certificate.Domains, want)
	}
	vars, err := svc.BuiltinVariables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	certsMap := vars["certs"].(map[string]any)
	certVar := certsMap["example_com"].(map[string]any)
	if certVar["certificatePem"] == "" || certVar["privateKeyPem"] == "" {
		t.Fatalf("certificate variable missing PEM values: %#v", certVar)
	}
}

func TestIssueRejectsInvalidVariableName(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()

	_, err := svc.Issue(context.Background(), IssueRequest{DomainID: "dnsdom_1", Prefix: "@", VariableName: "bad-name"})
	if err == nil {
		t.Fatal("expected invalid variable name error")
	}
}

func newTestService(t *testing.T) (*Service, *fakeProvider, func()) {
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
	if _, err := store.AppDB().Exec(`INSERT INTO dns_domains(id,name,provider,api_token_secret,account_id,created_at,updated_at) VALUES('dnsdom_1','example.com','cloudflare','token','acct_1','now','now')`); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{bundle: testBundle(t)}
	return NewServiceWithProvider(store.AppDB(), cfg, fake, nil), fake, func() { _ = store.Close() }
}

type fakeProvider struct {
	last   Request
	bundle Bundle
}

func (f *fakeProvider) Issue(ctx context.Context, req Request) (Bundle, error) {
	f.last = req
	return f.bundle, nil
}

func (f *fakeProvider) Renew(ctx context.Context, certID string) (Bundle, error) {
	return f.bundle, nil
}

func testBundle(t *testing.T) Bundle {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		DNSNames:     []string{"example.com", "*.example.com"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{
		CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		PrivateKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
