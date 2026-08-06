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
	"io"
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
	waitCertificateTaskTerminal(t, svc.tasks, result.TaskID)
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
	certVar := certsMap[result.Certificate.ID].(map[string]any)
	if certVar["certificatePem"] == "" || certVar["privateKeyPem"] == "" {
		t.Fatalf("certificate variable missing PEM values: %#v", certVar)
	}
	if certVar["certificate_pem"] == "" || certVar["private_key_pem"] == "" {
		t.Fatalf("snake_case certificate variables missing PEM values: %#v", certVar)
	}
}

func TestIssueMultiplePrefixesExpandsDomainsAndKeepsLegacyScopeCompatible(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()

	result, err := svc.Issue(context.Background(), IssueRequest{DomainID: "dnsdom_1", Prefixes: []string{"@", "api", "*", "*.api", "api"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com", "api.example.com", "*.example.com", "*.api.example.com"}
	if result.Certificate.Scope != ScopePrefixes || result.Certificate.Prefix != "@,api,*,*.api" {
		t.Fatalf("certificate = %#v", result.Certificate)
	}
	if !equalStrings(result.Certificate.Domains, want) {
		t.Fatalf("stored domains=%#v want %#v", result.Certificate.Domains, want)
	}
	waitCertificateTaskTerminal(t, svc.tasks, result.TaskID)
	if !equalStrings(fake.last.Domains, want) {
		t.Fatalf("provider domains=%#v want %#v", fake.last.Domains, want)
	}

	legacy, err := svc.Issue(context.Background(), IssueRequest{DomainID: "dnsdom_1", Prefix: "@", Scope: ScopeWildcard})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Certificate.Scope != ScopeWildcard || !equalStrings(legacy.Certificate.Domains, []string{"example.com", "*.example.com"}) {
		t.Fatalf("legacy wildcard certificate = %#v", legacy.Certificate)
	}
}

func TestCertificateDomainMatchesWildcardOnlyOneLabel(t *testing.T) {
	tests := []struct {
		pattern string
		domain  string
		want    bool
	}{
		{pattern: "*.test.com", domain: "a.test.com", want: true},
		{pattern: "*.test.com", domain: "test.com", want: false},
		{pattern: "*.test.com", domain: "a.b.test.com", want: false},
	}
	for _, tt := range tests {
		if got := certificateDomainMatches(tt.pattern, tt.domain); got != tt.want {
			t.Fatalf("certificateDomainMatches(%q, %q)=%v want %v", tt.pattern, tt.domain, got, tt.want)
		}
	}
}

func TestIssuedCertificatesExposeApplicationInternalFiles(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	issuedResult, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "@", Scope: ScopeWildcard})
	if err != nil {
		t.Fatal(err)
	}
	waitCertificateTaskTerminal(t, svc.tasks, issuedResult.TaskID)
	fake.err = errors.New("provider unavailable")
	unavailableResult, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "api"})
	if err != nil {
		t.Fatal(err)
	}
	waitCertificateTaskTerminal(t, svc.tasks, unavailableResult.TaskID)

	catalog, err := svc.InternalFileCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundCert := false
	foundKey := false
	for _, item := range catalog {
		if item.ResourceID == unavailableResult.Certificate.ID {
			t.Fatalf("unissued certificate exposed as application internal file: %#v", item)
		}
		if item.Source == "certificate:"+issuedResult.Certificate.ID+":certificate" {
			foundCert = true
		}
		if item.Source == "certificate:"+issuedResult.Certificate.ID+":private_key" {
			foundKey = true
		}
	}
	if !foundCert || !foundKey {
		t.Fatalf("catalog missing issued certificate files: %#v", catalog)
	}

	reader, info, err := svc.OpenInternalFile(ctx, "certificate:"+issuedResult.Certificate.ID+":private_key")
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(keyPEM) != string(fake.bundle.PrivateKeyPEM) || info.Mode != "0600" || info.Size == 0 {
		t.Fatalf("private key internal file mismatch info=%#v", info)
	}
	if _, _, err := svc.OpenInternalFile(ctx, "certificate:"+unavailableResult.Certificate.ID+":certificate"); err == nil {
		t.Fatal("expected unissued certificate internal file read to be rejected")
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app_cert_file','cert file app',0,?,'all','[]','[]',1,'hash','job','default','now','now')`,
		"name: cert-file\nimage: nginx\nmounts:\n  - type: panel_file\n    source: certificate:"+issuedResult.Certificate.ID+":private_key\n    target: /etc/ssl/private/key.pem\n"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, issuedResult.Certificate.ID); err == nil {
		t.Fatal("expected certificate delete to be blocked while panel_file is in use")
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

func TestReverseProxyCertificatesReturnsOnlyIssuedPEM(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	result, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "@", Scope: ScopeWildcard})
	if err != nil {
		t.Fatal(err)
	}
	waitCertificateTaskTerminal(t, svc.tasks, result.TaskID)
	fake.err = errors.New("provider unavailable")
	unavailable, err := svc.Issue(ctx, IssueRequest{DomainID: "dnsdom_1", Prefix: "api"})
	if err != nil {
		t.Fatal(err)
	}
	waitCertificateTaskTerminal(t, svc.tasks, unavailable.TaskID)

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

func TestIssueUsesCertificateIDForBuiltinVariable(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()

	result, err := svc.Issue(context.Background(), IssueRequest{DomainID: "dnsdom_1", Prefix: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Certificate.VariableName != result.Certificate.ID {
		t.Fatalf("variable name = %q want certificate id %q", result.Certificate.VariableName, result.Certificate.ID)
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
	waitCertificateTaskTerminal(t, svc.tasks, result.TaskID)
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

func TestRefreshApplicationsAttemptsReverseProxyWhenApplicationRedeployFails(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	refresher := &fakeApplicationRefresher{redeployChangedErr: errors.New("application refresh failed")}
	svc.applications = refresher

	err := svc.refreshApplications(context.Background())
	if err == nil || !strings.Contains(err.Error(), "application refresh failed") {
		t.Fatalf("refresh error = %v", err)
	}
	if refresher.redeployChangedCalls != 1 || refresher.reconcileReverseProxyCalls != 1 {
		t.Fatalf("refresher calls changed=%d proxy=%d", refresher.redeployChangedCalls, refresher.reconcileReverseProxyCalls)
	}
}

func newTestService(t *testing.T) (*Service, *fakeProvider, func()) {
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
	if _, err := store.AppDB().Exec(`INSERT INTO dns_domains(id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at) VALUES('dnsdom_1','example.com','cloudflare','{}','','now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
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

func waitCertificateTaskTerminal(t *testing.T, taskSvc *tasks.Service, taskID string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		switch task.Status {
		case tasks.StatusCompleted, tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked, tasks.StatusCancelled:
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := taskSvc.Get(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("task did not reach terminal status: %#v", task)
	return tasks.Task{}
}

type fakeApplicationRefresher struct {
	redeployChangedErr         error
	redeployEnabledErr         error
	reconcileReverseProxyErr   error
	redeployChangedCalls       int
	redeployEnabledCalls       int
	reconcileReverseProxyCalls int
}

func (f *fakeApplicationRefresher) RedeployChangedApplications(context.Context) (int, error) {
	f.redeployChangedCalls++
	return 0, f.redeployChangedErr
}

func (f *fakeApplicationRefresher) RedeployEnabledApplications(context.Context) (int, error) {
	f.redeployEnabledCalls++
	return 0, f.redeployEnabledErr
}

func (f *fakeApplicationRefresher) ReconcileReverseProxy(context.Context) error {
	f.reconcileReverseProxyCalls++
	return f.reconcileReverseProxyErr
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
