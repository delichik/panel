package keyassets

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/secrets"
)

func TestCreateAssetsExposeInternalFilesAndProtectDeletes(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Internal CA", CommonName: "panel.internal"})
	if err != nil {
		t.Fatal(err)
	}
	tlsAsset, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Web",
		ParentAssetID: ca.ID,
		CommonName:    "web.internal",
		DNSNames:      []string{"web.internal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sshAsset, err := svc.GenerateSSH(ctx, GenerateSSHRequest{Name: "Deploy", Algorithm: AlgorithmEd25519})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,resolved_variables_json,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app_1','web',0,'name: web
image: nginx
mounts:
  - type: panel_file
    source: key_asset:` + sshAsset.ID + `:private_key
    target: /root/.ssh/id_ed25519
','{}','{}','all','[]','[]',1,'hash','job','default','now','now')`); err != nil {
		t.Fatal(err)
	}

	catalog, err := svc.InternalFileCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundTLS := false
	foundSSH := false
	for _, item := range catalog {
		if item.Source == "key_asset:"+tlsAsset.ID+":private_key" {
			foundTLS = true
		}
		if item.Source == "key_asset:"+sshAsset.ID+":ssh_public_key" {
			foundSSH = true
		}
	}
	if !foundTLS || !foundSSH {
		t.Fatalf("panel file catalog missing expected items: %#v", catalog)
	}
	privateKey, err := readPanelFileForTest(ctx, svc, "key_asset:"+sshAsset.ID+":private_key")
	if err != nil || !strings.Contains(string(privateKey), "PRIVATE KEY") {
		t.Fatalf("ssh private key read failed: err=%v", err)
	}
	proxyCerts, err := svc.ReverseProxyCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(proxyCerts) != 1 || proxyCerts[0].ID != tlsAsset.ID {
		t.Fatalf("reverse proxy certs = %#v", proxyCerts)
	}
	if err := svc.Delete(ctx, ca.ID); err == nil {
		t.Fatal("expected CA delete to be blocked while child exists")
	}
	if err := svc.Delete(ctx, sshAsset.ID); err == nil {
		t.Fatal("expected in-use SSH asset delete to be blocked")
	}
}

func TestSystemManagedAssetsAreNotApplicationInternalFiles(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()
	if _, err := svc.EnsureAgentTLSAssets(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := svc.InternalFileCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog {
		if item.ResourceID == SystemAgentCAAssetID || item.ResourceID == SystemAgentClientAssetID {
			t.Fatalf("system managed asset exposed as application panel file: %#v", item)
		}
	}
	if _, err := readPanelFileForTest(ctx, svc, "key_asset:"+SystemAgentCAAssetID+":certificate"); err == nil {
		t.Fatal("expected system managed asset panel file read to be rejected")
	}
}

func TestListReportsExactPanelFileAndReverseProxyReferences(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Internal CA", CommonName: "panel.internal"})
	if err != nil {
		t.Fatal(err)
	}
	tlsAsset, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Web",
		ParentAssetID: ca.ID,
		DNSNames:      []string{"*.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sshAsset, err := svc.GenerateSSH(ctx, GenerateSSHRequest{Name: "Deploy", Algorithm: AlgorithmEd25519})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,resolved_variables_json,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app_refs','Referenced app',0,?,'{}','{}','all','[]',?,1,'hash','job','default','now','now')`,
		"name: refs\nmounts:\n  - type: panel_file\n    source: key_asset:"+sshAsset.ID+":private_key\n    target: /root/.ssh/id_ed25519\n",
		`[{"domain":"api.example.com"}]`); err != nil {
		t.Fatal(err)
	}

	assets, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Asset{}
	for _, asset := range assets {
		byID[asset.ID] = asset
	}
	assertReference := func(asset Asset, relation string) {
		t.Helper()
		if !asset.InUse || len(asset.References) != 1 {
			t.Fatalf("asset references = %#v", asset)
		}
		reference := asset.References[0]
		if reference.ResourceType != "application" || reference.ResourceID != "app_refs" ||
			reference.ResourceName != "Referenced app" || reference.Relation != relation {
			t.Fatalf("reference = %#v", reference)
		}
	}
	assertReference(byID[sshAsset.ID], "panel_file")
	assertReference(byID[tlsAsset.ID], "reverse_proxy")
	if byID[ca.ID].ChildCount != 1 {
		t.Fatalf("CA child count = %d", byID[ca.ID].ChildCount)
	}
}

func readPanelFileForTest(ctx context.Context, svc *Service, source string) ([]byte, error) {
	reader, _, err := svc.OpenInternalFile(ctx, source)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func TestImportRejectsEncryptedPrivateKeyAndSupportsArchiveFlows(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Root", CommonName: "root.internal"})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := svc.CreateExport(ctx, ExportRequest{AssetIDs: []string{ca.ID}, Password: "very-secret-12"})
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes, _, err := svc.DownloadExport(ctx, exported.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := svc.PreflightImport(ctx, ImportPreflightRequest{ArchiveBase64: encodeBase64(archiveBytes), Password: "very-secret-12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteImport(ctx, preflight.PlanID, ImportExecuteRequest{Strategy: "skip_existing"}); err != nil {
		t.Fatal(err)
	}
	preflight, err = svc.PreflightImport(ctx, ImportPreflightRequest{ArchiveBase64: encodeBase64(archiveBytes), Password: "very-secret-12"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.ExecuteImport(ctx, preflight.PlanID, ImportExecuteRequest{Strategy: "generate_new_id"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatalf("expected task id, got %#v", result)
	}
	items, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("expected cloned asset after import, got %#v", items)
	}

	certPEM, keyPEM := testCertificatePair(t)
	_, err = svc.Import(ctx, ImportRequest{
		Type:           TypeTLSCertificate,
		Name:           "bad",
		CertificatePEM: certPEM,
		PrivateKeyPEM:  "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAA==\n-----END ENCRYPTED PRIVATE KEY-----",
	})
	if err == nil || !strings.Contains(err.Error(), "Encrypted private keys") {
		t.Fatalf("expected encrypted private key rejection, got %v", err)
	}
	_ = keyPEM
}

func TestEnsureLegacySelfSignedMigrated(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	certPEM, keyPEM := testCertificatePair(t)
	pubPEM := testPublicKeyPEM(t, keyPEM)
	legacyDir := filepath.Join(svc.cfg.DataRoot, "certs", "self-signed", "ca_legacy")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(legacyDir, "certificate.pem")
	keyPath := filepath.Join(legacyDir, "private-key.pem")
	pubPath := filepath.Join(legacyDir, "public-key.pem")
	if err := os.WriteFile(certPath, []byte(certPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte(keyPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath, []byte(pubPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO self_signed_certificates(id,parent_ca_id,kind,name,common_name,dns_names_json,ip_addresses_json,certificate_path,private_key_path,public_key_path,fingerprint,not_before,not_after,created_at,updated_at) VALUES('ca_legacy','','ca','Legacy CA','legacy.internal','[]','[]',?,?,?,'','','',?,?)`,
		certPath, keyPath, pubPath, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureLegacySelfSignedMigrated(ctx); err != nil {
		t.Fatal(err)
	}
	asset, err := svc.Get(ctx, "ca_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if asset.Type != TypeCACertificate {
		t.Fatalf("migrated asset = %#v", asset)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("expected legacy dir removed, stat err=%v", err)
	}
}

func TestEnsureAgentTLSAssetsPersistsInKeyAssets(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	first, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstCA, err := first.CAInfo()
	if err != nil {
		t.Fatal(err)
	}
	firstClient, err := first.ClientInfo()
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondCA, _ := second.CAInfo()
	secondClient, _ := second.ClientInfo()
	if secondCA.Fingerprint != firstCA.Fingerprint {
		t.Fatal("agent CA changed across EnsureAgentTLSAssets calls")
	}
	if secondClient.Fingerprint != firstClient.Fingerprint {
		t.Fatal("agent client certificate changed across EnsureAgentTLSAssets calls")
	}
	ca, err := svc.Get(ctx, SystemAgentCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if ca.Metadata["systemScope"] != systemAgentScope || ca.Metadata["systemManaged"] != true {
		t.Fatalf("agent CA metadata = %#v", ca.Metadata)
	}
	if ca.NotAfter.Sub(ca.NotBefore) < 29*365*24*time.Hour {
		t.Fatalf("agent CA lifetime too short: %s", ca.NotAfter.Sub(ca.NotBefore))
	}
	client, err := svc.Get(ctx, SystemAgentClientAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if lifetime := client.NotAfter.Sub(client.NotBefore); lifetime < 29*24*time.Hour || lifetime > 32*24*time.Hour {
		t.Fatalf("agent client lifetime = %s", lifetime)
	}
}

func TestEnsureAgentTLSAssetsRecreatesMissingCAAndClientTogether(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	first, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstCA, _ := first.CAInfo()
	if _, err := store.AppDB().Exec(`DELETE FROM key_assets WHERE id=?`, SystemAgentCAAssetID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondCA, _ := second.CAInfo()
	if secondCA.Fingerprint == firstCA.Fingerprint {
		t.Fatal("expected missing CA to regenerate the agent CA")
	}
	client, err := svc.Get(ctx, SystemAgentClientAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if client.ParentAssetID != SystemAgentCAAssetID {
		t.Fatalf("client parent after CA regeneration = %q", client.ParentAssetID)
	}
}

func TestEnsureAgentTLSAssetsReissuesMissingClientOnly(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	first, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstCA, _ := first.CAInfo()
	firstClient, _ := first.ClientInfo()
	if _, err := store.AppDB().Exec(`DELETE FROM key_assets WHERE id=?`, SystemAgentClientAssetID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsureAgentTLSAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondCA, _ := second.CAInfo()
	secondClient, _ := second.ClientInfo()
	if secondCA.Fingerprint != firstCA.Fingerprint {
		t.Fatal("missing client certificate changed the agent CA")
	}
	if secondClient.Fingerprint == firstClient.Fingerprint {
		t.Fatal("expected missing client certificate to be reissued")
	}
}

func TestRefreshApplicationsAttemptsReverseProxyWhenEnabledRedeployFails(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	refresher := &fakeApplicationRefresher{redeployEnabledErr: errors.New("application redeploy failed")}
	svc.applications = refresher

	err := svc.refreshApplications(context.Background())
	if err == nil || !strings.Contains(err.Error(), "application redeploy failed") {
		t.Fatalf("refresh error = %v", err)
	}
	if refresher.redeployEnabledCalls != 1 || refresher.reconcileReverseProxyCalls != 1 {
		t.Fatalf("refresher calls enabled=%d proxy=%d", refresher.redeployEnabledCalls, refresher.reconcileReverseProxyCalls)
	}
}

func newTestService(t *testing.T) (*Service, *storage.Store, func()) {
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
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.TaskDB())
	svc := NewService(store.AppDB(), cfg, secrets, taskSvc)
	svc.RegisterTasks(taskSvc)
	return svc, store, func() { _ = store.Close() }
}

func encodeBase64(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

type fakeApplicationRefresher struct {
	redeployEnabledErr         error
	reconcileReverseProxyErr   error
	redeployEnabledCalls       int
	reconcileReverseProxyCalls int
}

func (f *fakeApplicationRefresher) RedeployEnabledApplications(context.Context) (int, error) {
	f.redeployEnabledCalls++
	return 0, f.redeployEnabledErr
}

func (f *fakeApplicationRefresher) ReconcileReverseProxy(context.Context) error {
	f.reconcileReverseProxyCalls++
	return f.reconcileReverseProxyErr
}

func testCertificatePair(t *testing.T) (string, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "legacy.internal"},
		NotBefore:             time.Now().UTC().Add(-time.Hour),
		NotAfter:              time.Now().UTC().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(privateKeyPEM)
}

func testPublicKeyPEM(t *testing.T, privateKeyPEM string) string {
	t.Helper()
	key, _, err := parsePrivateKeyPEM(privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyPEM, err := marshalPublicKeyPEM(key.public)
	if err != nil {
		t.Fatal(err)
	}
	return string(publicKeyPEM)
}
