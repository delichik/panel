package keyassets

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"
	"testing"

	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/paneltls"
)

func TestEnsurePanelTLSAssetsCreatesManagedRSAChain(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	leaf, err := svc.EnsurePanelTLSAssets(ctx, "panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if leaf.ID != SystemPanelTLSAssetID || leaf.ParentAssetID != SystemPanelCAAssetID || leaf.Algorithm != AlgorithmRSA || leaf.KeySize != 2048 {
		t.Fatalf("unexpected managed Panel leaf: %#v", leaf)
	}
	ca, err := svc.getStoredAsset(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	storedLeaf, err := svc.getStoredAsset(ctx, SystemPanelTLSAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if !panelCARecordValid(ca) || !svc.validPanelLeaf(storedLeaf, ca, "panel.example.test") {
		t.Fatalf("managed Panel chain is invalid: ca=%#v leaf=%#v", ca.Asset, storedLeaf.Asset)
	}
	caCertificate, err := ca.certificate()
	if err != nil {
		t.Fatal(err)
	}
	leafCertificate, err := storedLeaf.certificate()
	if err != nil {
		t.Fatal(err)
	}
	if key, ok := caCertificate.PublicKey.(*rsa.PublicKey); !ok || key.N.BitLen() != 2048 {
		t.Fatalf("Panel CA key = %#v, want RSA-2048", caCertificate.PublicKey)
	}
	if key, ok := leafCertificate.PublicKey.(*rsa.PublicKey); !ok || key.N.BitLen() != 2048 {
		t.Fatalf("Panel leaf key = %#v, want RSA-2048", leafCertificate.PublicKey)
	}
	if leafCertificate.CheckSignatureFrom(caCertificate) != nil {
		t.Fatal("Panel leaf is not signed by the managed Panel CA")
	}
	if len(leafCertificate.ExtKeyUsage) != 1 || leafCertificate.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("Panel leaf usages = %#v, want ServerAuth only", leafCertificate.ExtKeyUsage)
	}
	if leafCertificate.VerifyHostname("panel.example.test") != nil {
		t.Fatal("Panel leaf does not cover its configured domain")
	}
	var encryptedPrivateKey string
	if err := store.AppDB().QueryRowContext(ctx, `SELECT private_key_ciphertext FROM key_assets WHERE id=?`, SystemPanelTLSAssetID).Scan(&encryptedPrivateKey); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encryptedPrivateKey, "BEGIN PRIVATE KEY") {
		t.Fatal("managed Panel private key was persisted as PEM instead of encrypted material")
	}

	if err := svc.SyncPanelTLS(ctx, "panel.example.test", ""); err != nil {
		t.Fatal(err)
	}
	fixed, err := paneltls.FixedCertificate(svc.cfg.DataRoot, "panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixed.Certificate) != 2 {
		t.Fatalf("fixed Panel listener chain length = %d, want 2", len(fixed.Certificate))
	}
	if err := paneltls.ValidateListenerCertificate(fixed, "panel.example.test"); err != nil {
		t.Fatalf("fixed Panel listener certificate is invalid: %v", err)
	}
}

func TestEnsurePanelTLSAssetsReissuesLeafForDomainChange(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	first, err := svc.EnsurePanelTLSAssets(ctx, "first.example.test")
	if err != nil {
		t.Fatal(err)
	}
	caBefore, err := svc.Get(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnsurePanelTLSAssets(ctx, "second.example.test")
	if err != nil {
		t.Fatal(err)
	}
	caAfter, err := svc.Get(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("Panel leaf was not reissued after the configured domain changed")
	}
	if caBefore.Fingerprint != caAfter.Fingerprint {
		t.Fatal("Panel CA changed during a leaf-only domain reissue")
	}
	if second.CommonName != "second.example.test" || len(second.DNSNames) != 1 || second.DNSNames[0] != "second.example.test" {
		t.Fatalf("reissued Panel leaf domains = %#v", second)
	}
}

func TestEnsurePanelTLSAssetsRebuildsCorruptCAAndLeaf(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	if _, err := svc.EnsurePanelTLSAssets(ctx, "panel.example.test"); err != nil {
		t.Fatal(err)
	}
	previousCA, err := svc.Get(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	previousLeaf, err := svc.Get(ctx, SystemPanelTLSAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().ExecContext(ctx, `UPDATE key_assets SET private_key_ciphertext='corrupt' WHERE id=?`, SystemPanelCAAssetID); err != nil {
		t.Fatal(err)
	}
	leaf, err := svc.EnsurePanelTLSAssets(ctx, "panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	currentCA, err := svc.Get(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if currentCA.Fingerprint == previousCA.Fingerprint || leaf.Fingerprint == previousLeaf.Fingerprint {
		t.Fatal("corrupt Panel CA did not rebuild the complete managed chain")
	}
	storedCA, err := svc.getStoredAsset(ctx, SystemPanelCAAssetID)
	if err != nil {
		t.Fatal(err)
	}
	storedLeaf, err := svc.getStoredAsset(ctx, SystemPanelTLSAssetID)
	if err != nil {
		t.Fatal(err)
	}
	if !svc.validPanelLeaf(storedLeaf, storedCA, "panel.example.test") {
		t.Fatal("rebuilt Panel leaf is invalid")
	}
}

func TestSyncPanelTLSRejectsEd25519Certificate(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()

	ca, err := svc.CreateCA(ctx, CreateCARequest{Name: "Ed25519 CA", CommonName: "ed25519-ca.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := svc.CreateTLS(ctx, CreateTLSRequest{
		Name:          "Ed25519 Panel HTTPS",
		ParentAssetID: ca.ID,
		CommonName:    "panel.example.test",
		DNSNames:      []string{"panel.example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SyncPanelTLS(ctx, "panel.example.test", certificate.ID)
	var typed *panelerr.Error
	if !errors.As(err, &typed) || typed.Code != "invalid_panel_tls_certificate" {
		t.Fatalf("Ed25519 Panel certificate error = %v, want invalid_panel_tls_certificate", err)
	}
}

func TestPanelSystemAssetsAreNotUserMutableOrProxyCertificates(t *testing.T) {
	svc, _, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()
	leaf, err := svc.EnsurePanelTLSAssets(ctx, "panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, leaf.ID); err == nil {
		t.Fatal("expected system Panel certificate delete to be rejected")
	}
	if _, err := svc.ReissueTLS(ctx, leaf.ID); err == nil {
		t.Fatal("expected system Panel certificate reissue to be rejected")
	}
	if _, _, err := svc.ReadFile(ctx, leaf.ID, "certificate"); err == nil {
		t.Fatal("expected system Panel certificate file download to be rejected")
	}
	if _, err := svc.CreateExport(ctx, ExportRequest{AssetIDs: []string{leaf.ID}, Password: "very-secret-12"}); err == nil {
		t.Fatal("expected system Panel certificate export to be rejected")
	}
	proxyCertificates, err := svc.ReverseProxyCertificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, certificate := range proxyCertificates {
		if certificate.ID == SystemPanelTLSAssetID {
			t.Fatal("system Panel certificate leaked into reverse proxy certificate list")
		}
	}
	if _, err := svc.CreateTLS(ctx, CreateTLSRequest{ParentAssetID: SystemPanelCAAssetID, DNSNames: []string{"custom.example.test"}, Algorithm: AlgorithmRSA, KeySize: 2048}); err == nil {
		t.Fatal("expected user certificate creation under system Panel CA to be rejected")
	}
}

func TestPanelTLSReconcileUsesExistingTaskMechanism(t *testing.T) {
	svc, store, closeFn := newTestService(t)
	defer closeFn()
	ctx := context.Background()
	if err := setPanelTLSSelection(ctx, store, "panel.example.test", ""); err != nil {
		t.Fatal(err)
	}

	batch, shouldRun, err := svc.CollectPanelTLSInputs(ctx, tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun || len(batch.Inputs) != 1 || batch.Inputs[0].Type != TaskTypePanelTLSReconcile || batch.Inputs[0].ResourceID != SystemPanelTLSAssetID {
		t.Fatalf("unexpected Panel TLS reconcile input: %#v, shouldRun=%t", batch, shouldRun)
	}
	if err := svc.RunPanelTLSReconcileTask(tasks.TaskContext{Context: ctx}); err != nil {
		t.Fatal(err)
	}
	if _, shouldRun, err := svc.CollectPanelTLSInputs(ctx, tasks.PeriodicTrigger{Type: "scheduler"}); err != nil || shouldRun {
		t.Fatalf("healthy Panel TLS assets should not schedule another reconciliation: shouldRun=%t err=%v", shouldRun, err)
	}
}
