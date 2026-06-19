package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/keyassets"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/secrets"
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
	if result.TaskID == "" || result.Certificate.Status != StatusPending {
		t.Fatalf("expected queued issue task, got %#v", result)
	}
	if !equalStrings(result.Certificate.Domains, want) {
		t.Fatalf("stored domains=%#v want %#v", result.Certificate.Domains, want)
	}
	if len(fake.last.Domains) != 0 {
		t.Fatalf("provider should not be called before background task runs")
	}
	task, err := svc.tasks.Get(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunIssueTask(tasks.TaskContext{Context: context.Background(), Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Get(context.Background(), result.Certificate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Status != StatusIssued || !equalStrings(fake.last.Domains, want) {
		t.Fatalf("issued=%#v provider=%#v want domains %#v", issued, fake.last.Domains, want)
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
	if certVar["certificate_pem"] == "" || certVar["private_key_pem"] == "" {
		t.Fatalf("snake_case certificate variables missing PEM values: %#v", certVar)
	}
}

func TestSelfSignedCAIsReusableAndProtectedWhileChildrenExist(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	ca, err := svc.CreateSelfSignedCA(ctx, SelfSignedCARequest{Name: "Internal CA", CommonName: "panel.internal", Years: 5})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateSelfSignedLeaf(ctx, SelfSignedLeafRequest{
		Name: "API", CAID: ca.ID, CommonName: "api.internal",
		DNSNames: []string{"api.internal"}, IPAddresses: []string{"10.0.0.10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateSelfSignedLeaf(ctx, SelfSignedLeafRequest{
		Name: "Web", CAID: ca.ID, CommonName: "web.internal", DNSNames: []string{"web.internal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ParentCAID != ca.ID || second.ParentCAID != ca.ID || first.Fingerprint == "" {
		t.Fatalf("unexpected reusable CA result ca=%#v first=%#v second=%#v", ca, first, second)
	}
	renewed, err := svc.RenewSelfSignedLeaf(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Fingerprint == first.Fingerprint {
		t.Fatal("expected reissued certificate to have a new fingerprint")
	}
	renewTasks, err := svc.tasks.List(ctx, tasks.ListFilter{Type: keyassets.TaskTypeTLSReissue, Status: tasks.StatusCompleted, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(renewTasks.Items) != 1 || renewTasks.Items[0].ResourceID != first.ID {
		t.Fatalf("expected completed self-signed renewal task, got %#v", renewTasks.Items)
	}
	if err := svc.DeleteSelfSigned(ctx, ca.ID); err == nil {
		t.Fatal("expected CA deletion to be blocked while child certificates exist")
	}
	if err := svc.DeleteSelfSigned(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSelfSigned(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSelfSigned(ctx, ca.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPanelFileCatalogReturnsReferencesWithoutPrivateKeyContent(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	ca, err := svc.CreateSelfSignedCA(ctx, SelfSignedCARequest{Name: "Internal CA", CommonName: "panel.internal"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := svc.CreateSelfSignedLeaf(ctx, SelfSignedLeafRequest{Name: "Web", CAID: ca.ID, CommonName: "web.internal", DNSNames: []string{"web.internal"}})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := svc.PanelFileCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range catalog {
		if item.ResourceID == leaf.ID && item.Kind == "private_key" {
			found = !strings.Contains(item.Source, "BEGIN")
		}
	}
	if !found {
		t.Fatalf("private key reference missing from catalog: %#v", catalog)
	}
	content, err := svc.ReadPanelFile(ctx, "certificate:"+leaf.ID+":private_key")
	if err != nil || !strings.Contains(string(content), "PRIVATE KEY") {
		t.Fatalf("private key read failed: err=%v content=%q", err, content)
	}
}

func TestReverseProxyCertificatesReturnsOnlyIssuedPEM(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	result, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "@", Scope: ScopeWildcard})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Get(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunIssueTask(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "api"}); err != nil {
		t.Fatal(err)
	}

	certs, err := svc.ReverseProxyCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 || certs[0].ID != result.Certificate.ID {
		t.Fatalf("expected only issued cert, got %#v", certs)
	}
	if certs[0].CertificatePEM != string(fake.bundle.CertificatePEM) || certs[0].PrivateKeyPEM != string(fake.bundle.PrivateKeyPEM) {
		t.Fatalf("reverse proxy cert PEM mismatch: %#v", certs[0])
	}
	if !equalStrings(certs[0].Domains, []string{"example.com", "*.example.com"}) {
		t.Fatalf("reverse proxy cert domains = %#v", certs[0].Domains)
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

func TestRenewFailureRecordsLastErrorAndTask(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	result, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "api"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Get(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunIssueTask(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	fake.err = errors.New("renew failed")

	if err := svc.Renew(ctx, result.Certificate.ID); err == nil {
		t.Fatal("expected renewal error")
	}
	cert, err := svc.Get(ctx, result.Certificate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Status != StatusIssued || !strings.Contains(cert.LastError, "renew failed") {
		t.Fatalf("expected issued certificate with renewal error, got %#v", cert)
	}
	tasksResult, err := svc.tasks.List(ctx, tasks.ListFilter{Type: TaskTypeRenew, Status: tasks.StatusFailed, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasksResult.Items) != 1 || !strings.Contains(tasksResult.Items[0].Error, "renew failed") {
		t.Fatalf("expected failed renewal task, got %#v", tasksResult.Items)
	}
	steps, err := svc.tasks.Steps(ctx, tasksResult.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 || steps[0].Step != "acme_order" {
		t.Fatalf("expected ACME progress step, got %#v", steps)
	}
	logs, _, err := svc.tasks.Logs(ctx, tasksResult.Items[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundLog := false
	for _, log := range logs {
		if strings.Contains(log.Line, "Creating fake ACME order") {
			foundLog = true
			break
		}
	}
	if !foundLog {
		t.Fatalf("expected ACME progress log, got %#v", logs)
	}
}

func newTestService(t *testing.T) (*Service, *fakeProvider, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO dns_domains(id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at) VALUES('dnsdom_1','example.com','cloudflare','{}','','now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	keyAssetSvc := keyassets.NewService(store.AppDB(), cfg, secrets, taskSvc)
	keyAssetSvc.RegisterTasks(taskSvc)
	fake := &fakeProvider{bundle: testBundle(t)}
	svc := NewServiceWithProvider(store.AppDB(), cfg, fake, taskSvc, WithKeyAssetProvider(keyAssetSvc))
	svc.RegisterTasks(taskSvc)
	return svc, fake, func() { _ = store.Close() }
}

type fakeProvider struct {
	last   Request
	bundle Bundle
	err    error
}

func (f *fakeProvider) Issue(ctx context.Context, req Request) (Bundle, error) {
	if req.Progress != nil {
		req.Progress(ctx, ACMEProgress{Stage: "acme_order", Message: "Creating fake ACME order", Metadata: map[string]any{"domains": req.Domains}})
	}
	if f.err != nil {
		return Bundle{}, f.err
	}
	f.last = req
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
