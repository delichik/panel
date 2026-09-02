package paneltls

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type testAssetReader struct {
	certificate []byte
	privateKey  []byte
}

func (r testAssetReader) ReadFile(_ context.Context, _ string, kind string) ([]byte, string, error) {
	if kind == "certificate" {
		return r.certificate, "certificate.pem", nil
	}
	return r.privateKey, "private.key", nil
}

func TestFixedCertificateReloadsSyncedPairAndFallsBackWhenMissing(t *testing.T) {
	invalidateCertificate()
	dataRoot := t.TempDir()
	initial, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Certificate) == 0 {
		t.Fatal("default certificate is empty")
	}

	certificatePEM, privateKeyPEM, err := newCertificate("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncCertificate(context.Background(), dataRoot, "panel.example.test", "asset-1", testAssetReader{
		certificate: certificatePEM,
		privateKey:  privateKeyPEM,
	}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(reloaded.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "panel.example.test" {
		t.Fatalf("reloaded certificate DNS names = %v", leaf.DNSNames)
	}

	secondCertificatePEM, secondPrivateKeyPEM, err := newCertificate("second.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "tls", "panel.crt"), secondCertificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "tls", "panel.key"), secondPrivateKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	stillCached, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	stillCachedLeaf, err := x509.ParseCertificate(stillCached.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(stillCachedLeaf.DNSNames) != 1 || stillCachedLeaf.DNSNames[0] != "panel.example.test" {
		t.Fatalf("certificate was reloaded without invalidation: %v", stillCachedLeaf.DNSNames)
	}

	invalidateCertificate()
	if err := os.Remove(filepath.Join(dataRoot, "tls", "panel.key")); err != nil {
		t.Fatal(err)
	}
	fallback, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	fallbackLeaf, err := x509.ParseCertificate(fallback.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if fallbackLeaf.VerifyHostname("localhost") != nil {
		t.Fatalf("fallback certificate does not cover localhost: %v", fallbackLeaf.VerifyHostname("localhost"))
	}
}

func TestSyncCertificateRejectsMismatchedPair(t *testing.T) {
	invalidateCertificate()
	dataRoot := t.TempDir()
	if _, err := FixedCertificate(dataRoot, "localhost"); err != nil {
		t.Fatal(err)
	}
	certificatePEM, _, err := newCertificate("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	_, otherKeyPEM, err := newCertificate("other.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncCertificate(context.Background(), dataRoot, "panel.example.test", "asset-1", testAssetReader{
		certificate: certificatePEM,
		privateKey:  otherKeyPEM,
	}); err == nil {
		t.Fatal("mismatched certificate pair was accepted")
	}

	current, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(current.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("localhost") != nil {
		t.Fatal("failed sync should not replace the active certificate")
	}
}

func TestRestoreFixedPairRestoresSnapshotWithoutAssetLookup(t *testing.T) {
	invalidateCertificate()
	dataRoot := t.TempDir()
	oldCertificatePEM, oldPrivateKeyPEM, err := newCertificate("old.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncCertificate(context.Background(), dataRoot, "old.example.test", "asset-old", testAssetReader{
		certificate: oldCertificatePEM,
		privateKey:  oldPrivateKeyPEM,
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := SnapshotFixedPair(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	newCertificatePEM, newPrivateKeyPEM, err := newCertificate("new.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncCertificate(context.Background(), dataRoot, "new.example.test", "asset-new", testAssetReader{
		certificate: newCertificatePEM,
		privateKey:  newPrivateKeyPEM,
	}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreFixedPair(dataRoot, snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(restored.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("old.example.test") != nil {
		t.Fatalf("restored certificate does not match snapshot: %v", leaf.VerifyHostname("old.example.test"))
	}
}

func TestFixedCertificateRecoversInterruptedPairReplacement(t *testing.T) {
	invalidateCertificate()
	dataRoot := t.TempDir()
	oldCertificatePEM, oldPrivateKeyPEM, err := newCertificate("old.example.test")
	if err != nil {
		t.Fatal(err)
	}
	newCertificatePEM, _, err := newCertificate("new.example.test")
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	previousCertPath, previousKeyPath, markerPath := certificatePairTransactionPaths(dataRoot)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{certPath, newCertificatePEM, 0o644},
		{keyPath, oldPrivateKeyPEM, 0o600},
		{previousCertPath, oldCertificatePEM, 0o644},
		{previousKeyPath, oldPrivateKeyPEM, 0o600},
	} {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	marker, err := json.Marshal(certificatePairTransaction{HasPreviousPair: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		t.Fatal(err)
	}

	recovered, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(recovered.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "old.example.test" {
		t.Fatalf("recovered certificate DNS names = %v", leaf.DNSNames)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("transaction marker remains after recovery: %v", err)
	}
}

func TestFixedCertificateKeepsValidPairWhenTransactionMarkerIsCorrupt(t *testing.T) {
	invalidateCertificate()
	dataRoot := t.TempDir()
	certificatePEM, privateKeyPEM, err := newCertificate("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	_, _, markerPath := certificatePairTransactionPaths(dataRoot)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, privateKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	recovered, err := FixedCertificate(dataRoot, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(recovered.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("panel.example.test") != nil {
		t.Fatalf("corrupt marker discarded valid certificate: %v", leaf.VerifyHostname("panel.example.test"))
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("transaction marker remains after recovery: %v", err)
	}
}
