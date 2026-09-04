package keyassets

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
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
	panelerr "panel/internal/platform/errors"
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
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app_1','web',0,'name: web
image: nginx
mounts:
  - type: panel_file
    source: key_asset:` + sshAsset.ID + `:private_key
    target: /root/.ssh/id_ed25519
','all','[]',1,'hash','job','default','now','now')`); err != nil {
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

func TestOverwriteImportRejectsSelectedPanelCertificateOutsideDomain(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Panel CA", CommonName: "panel-ca.internal", Algorithm: AlgorithmRSA, KeySize: 2048})
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Panel HTTPS",
		ParentAssetID: ca.ID,
		CommonName:    "panel.example.test",
		DNSNames:      []string{"panel.example.test"},
		Algorithm:     AlgorithmRSA,
		KeySize:       2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := setPanelTLSSelection(ctx, store, "panel.example.test", active.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncPanelTLS(ctx, "panel.example.test", active.ID); err != nil {
		t.Fatal(err)
	}
	activeCertificate, err := os.ReadFile(filepath.Join(svc.cfg.DataRoot, "tls", "panel.crt"))
	if err != nil {
		t.Fatal(err)
	}
	activeStoredCertificate, _, err := svc.ReadFile(ctx, active.ID, "certificate")
	if err != nil {
		t.Fatal(err)
	}

	incoming, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Wrong Panel HTTPS",
		ParentAssetID: ca.ID,
		CommonName:    "other.example.test",
		DNSNames:      []string{"other.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	incomingCertificate, _, err := svc.ReadFile(ctx, incoming.ID, "certificate")
	if err != nil {
		t.Fatal(err)
	}
	incomingKey, _, err := svc.ReadFile(ctx, incoming.ID, "private_key")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := encryptArchive("very-secret-12", archivePayload{Assets: []archiveAsset{{
		ID:             active.ID,
		Type:           TypeTLSCertificate,
		Name:           active.Name,
		ParentAssetID:  ca.ID,
		CommonName:     "other.example.test",
		Algorithm:      incoming.Algorithm,
		KeySize:        incoming.KeySize,
		CertificatePEM: string(incomingCertificate),
		PrivateKeyPEM:  string(incomingKey),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := svc.PreflightImport(ctx, ImportPreflightRequest{ArchiveBase64: encodeBase64(archive), Password: "very-secret-12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteImport(ctx, preflight.PlanID, ImportExecuteRequest{
		Strategy:                  "overwrite_existing",
		ConfirmOverwriteInUse:     true,
		ConfirmDangerousOverwrite: true,
	}); err == nil {
		t.Fatal("expected Panel-domain validation failure")
	}
	currentCertificate, err := os.ReadFile(filepath.Join(svc.cfg.DataRoot, "tls", "panel.crt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(currentCertificate) != string(activeCertificate) {
		t.Fatal("failed overwrite import changed the active Panel certificate")
	}
	storedCertificate, _, err := svc.ReadFile(ctx, active.ID, "certificate")
	if err != nil {
		t.Fatal(err)
	}
	if string(storedCertificate) != string(activeStoredCertificate) {
		t.Fatal("failed overwrite import changed the stored Panel certificate")
	}
}

func TestReissueSelectedPanelCertificateSynchronizesFixedPair(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Panel CA", CommonName: "panel-ca.internal", Algorithm: AlgorithmRSA, KeySize: 2048})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Panel HTTPS",
		ParentAssetID: ca.ID,
		CommonName:    "panel.example.test",
		DNSNames:      []string{"panel.example.test"},
		Algorithm:     AlgorithmRSA,
		KeySize:       2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := setPanelTLSSelection(ctx, store, "panel.example.test", asset.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncPanelTLS(ctx, "panel.example.test", asset.ID); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ReissueTLS(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixedCertificate, err := tls.LoadX509KeyPair(
		filepath.Join(svc.cfg.DataRoot, "tls", "panel.crt"),
		filepath.Join(svc.cfg.DataRoot, "tls", "panel.key"),
	)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(fixedCertificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("panel.example.test") != nil {
		t.Fatalf("reissued fixed certificate does not cover Panel domain: %v", leaf.VerifyHostname("panel.example.test"))
	}
	if certificateFingerprint(leaf) != result.Asset.Fingerprint {
		t.Fatalf("fixed certificate fingerprint = %q, want %q", certificateFingerprint(leaf), result.Asset.Fingerprint)
	}
	if fixedCertificate.PrivateKey == nil {
		t.Fatal("fixed Panel private key is empty")
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

func TestSummariesExcludeSystemManagedAssets(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	if _, err := svc.EnsureAgentTLSAssets(ctx); err != nil {
		t.Fatal(err)
	}
	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Internal CA", CommonName: "panel.internal"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTLS(ctx, CreateTLSRequest{Name: "Web", ParentAssetID: ca.ID, DNSNames: []string{"web.internal"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GenerateSSH(ctx, GenerateSSHRequest{Name: "Deploy", Algorithm: AlgorithmEd25519}); err != nil {
		t.Fatal(err)
	}

	isSystem := func(asset Asset) bool {
		return asset.ID == SystemAgentCAAssetID || asset.ID == SystemAgentClientAssetID || strings.HasPrefix(asset.ID, "agent-server-")
	}

	summaries, err := svc.ListSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range summaries {
		if isSystem(asset) {
			t.Fatalf("system managed asset leaked into ListSummaries: %#v", asset)
		}
	}
	if len(summaries) != 3 {
		t.Fatalf("ListSummaries count = %d, want 3 user assets", len(summaries))
	}

	page, err := svc.ListSummaryPage(ctx, 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range page.Items {
		if isSystem(asset) {
			t.Fatalf("system managed asset leaked into ListSummaryPage: %#v", asset)
		}
	}
	if page.Total != 3 {
		t.Fatalf("ListSummaryPage total = %d, want 3", page.Total)
	}

	certPage, err := svc.ListSummaryPageByTypes(ctx, 1, 50, "", []string{TypeCACertificate, TypeTLSCertificate})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range certPage.Items {
		if isSystem(asset) {
			t.Fatalf("system managed asset leaked into ListSummaryPageByTypes: %#v", asset)
		}
	}
	if certPage.Total != 2 {
		t.Fatalf("ListSummaryPageByTypes total = %d, want 2 user CA/TLS assets", certPage.Total)
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
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app_refs','Referenced app',0,?,'all','[]',1,'hash','job','default','now','now')`,
		"name: refs\nmounts:\n  - type: panel_file\n    source: key_asset:"+sshAsset.ID+":private_key\n    target: /root/.ssh/id_ed25519\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO reverse_proxy_routes(domain,app_id,origin_server_ids,any_access_json,target_port,paths_json,created_at,updated_at) VALUES('api.example.com','app_refs','[]','{}',0,'[]','now','now')`); err != nil {
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

func TestExportRecordsUseLogDBAndExpiredCleanupRemovesFile(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Export Root", CommonName: "export.internal"})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := svc.CreateExport(ctx, ExportRequest{AssetIDs: []string{ca.ID}, Password: "very-secret-12"})
	if err != nil {
		t.Fatal(err)
	}

	var filePath string
	if err := store.LogDB().QueryRow(`SELECT file_path FROM key_asset_exports WHERE task_id=?`, exported.TaskID).Scan(&filePath); err != nil {
		t.Fatalf("expected export record in log db: %v", err)
	}
	var appDBFilePath string
	if err := store.AppDB().QueryRow(`SELECT file_path FROM key_asset_exports WHERE task_id=?`, exported.TaskID).Scan(&appDBFilePath); err == nil {
		t.Fatal("expected key_asset_exports to be absent from app db")
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected export file: %v", err)
	}

	expiredAt := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := store.LogDB().Exec(`UPDATE key_asset_exports SET expires_at=? WHERE task_id=?`, expiredAt, exported.TaskID); err != nil {
		t.Fatal(err)
	}
	svc.cleanupExpiredExports(ctx, time.Now().UTC())

	var count int
	if err := store.LogDB().QueryRow(`SELECT COUNT(*) FROM key_asset_exports WHERE task_id=?`, exported.TaskID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired export rows = %d, want 0", count)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected export file removed, stat err=%v", err)
	}
}

func newTestService(t *testing.T) (*Service, *storage.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	svc := NewService(store.AppDB(), cfg, secrets, taskSvc, WithLogDB(store.LogDB()))
	svc.RegisterTasks(taskSvc)
	return svc, store, func() { _ = store.Close() }
}

func setPanelTLSSelection(ctx context.Context, store *storage.Store, domain, assetID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, value := range map[string]string{
		"panel.domain":           domain,
		"panel.tlsCertificateId": assetID,
	} {
		if _, err := store.AppDB().ExecContext(ctx, `
			INSERT INTO runtime_settings(key, value, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
		`, key, value, now); err != nil {
			return err
		}
	}
	return nil
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
	serial, err := randomSerial()
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
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

func TestBeginAssetTaskRejectsConcurrentSameResourceOperation(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	taskID, fail, _, err := svc.beginAssetTask(ctx, TaskTypeTLSReissue, "asset-1", "first reissue")
	if err != nil {
		t.Fatal(err)
	}
	if taskID == "" {
		t.Fatal("expected a task id")
	}
	// A second operation on the same asset must be rejected while the first
	// task is still running (reissue and regenerate share the resource key).
	_, _, _, err = svc.beginAssetTask(ctx, TaskTypeSSHRegenerate, "asset-1", "second regenerate")
	if err == nil {
		t.Fatal("expected concurrent same-resource operation to be rejected")
	}
	_ = fail(panelerr.Validation("test", "test"))
}

func TestDownloadExportRejectsPathOutsideExportDir(t *testing.T) {
	svc, store, closeFn := newTestService(t)
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
	outside := filepath.Join(t.TempDir(), "evil.panel")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LogDB().Exec(`UPDATE key_asset_exports SET file_path=? WHERE task_id=?`, outside, exported.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.DownloadExport(ctx, exported.TaskID); err == nil {
		t.Fatal("expected export path outside export dir to be rejected")
	}
}

func TestImportPlanFilesPersistExpiryAndCleanup(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM, err := marshalPrivateKeyPEM(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encryptArchive("correct horse battery staple", archivePayload{
		Assets: []archiveAsset{{
			ID: "asset-1", Type: TypeSSHKeyPair, Name: "key", Algorithm: AlgorithmEd25519, PrivateKeyPEM: string(privateKeyPEM),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.PreflightImport(ctx, ImportPreflightRequest{
		ArchiveBase64: base64.StdEncoding.EncodeToString(raw),
		Password:      "correct horse battery staple",
	})
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(svc.importDir, result.PlanID+".json")
	metaRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("expected persisted plan file: %v", err)
	}
	if !strings.Contains(string(metaRaw), result.PlanID) || !strings.Contains(string(metaRaw), `"expiresAt"`) {
		t.Fatalf("persisted plan metadata missing fields: %s", string(metaRaw))
	}

	expiresAt := parseTime(metaExpiresAt(t, string(metaRaw)))
	svc.cleanupExpiredImportPlans(ctx, expiresAt.Add(time.Minute))
	if _, err := os.Stat(planPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired plan file should be removed, stat err=%v", err)
	}

	orphan := filepath.Join(svc.importDir, "orphan.json")
	if err := os.WriteFile(orphan, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	svc.cleanupExpiredImportPlans(ctx, time.Now().UTC())
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed orphan plan file should be removed, stat err=%v", err)
	}
}

func metaExpiresAt(t *testing.T, raw string) string {
	t.Helper()
	const marker = `"expiresAt": "`
	idx := strings.Index(raw, marker)
	if idx < 0 {
		t.Fatalf("expiresAt not found in %s", raw)
	}
	rest := raw[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("expiresAt value unterminated in %s", raw)
	}
	return rest[:end]
}
