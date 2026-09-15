package security

import (
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"
)

func TestTLSAssetsResetClientCertificateKeepsCA(t *testing.T) {
	assets, err := EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	caBefore, err := assets.CAInfo()
	if err != nil {
		t.Fatal(err)
	}
	clientBefore, err := assets.ClientInfo()
	if err != nil {
		t.Fatal(err)
	}

	if err := assets.ResetClientCertificate(); err != nil {
		t.Fatal(err)
	}

	caAfter, err := assets.CAInfo()
	if err != nil {
		t.Fatal(err)
	}
	clientAfter, err := assets.ClientInfo()
	if err != nil {
		t.Fatal(err)
	}
	if caAfter.Fingerprint != caBefore.Fingerprint {
		t.Fatal("resetting the client certificate changed the agent CA")
	}
	if clientAfter.Fingerprint == clientBefore.Fingerprint {
		t.Fatal("expected a new client certificate fingerprint")
	}
}

func TestTLSAssetsResetAllChangesCAAndClient(t *testing.T) {
	assets, err := EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	caBefore, _ := assets.CAInfo()
	clientBefore, _ := assets.ClientInfo()

	if err := assets.ResetAll(); err != nil {
		t.Fatal(err)
	}

	caAfter, _ := assets.CAInfo()
	clientAfter, _ := assets.ClientInfo()
	if caAfter.Fingerprint == caBefore.Fingerprint {
		t.Fatal("expected a new agent CA fingerprint")
	}
	if clientAfter.Fingerprint == clientBefore.Fingerprint {
		t.Fatal("expected a new client certificate fingerprint")
	}
	if _, err := assets.ClientTLSConfig(); err != nil {
		t.Fatalf("reloaded TLS config: %v", err)
	}
}

func TestRejectServerAuthClientCertificates(t *testing.T) {
	assets, err := EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	clientLeaf := parseTestLeaf(t, assets.ClientCertPEM)
	serverCert, err := assets.IssueServerCertificate("panel-agent-test", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	serverLeaf := parseTestLeaf(t, serverCert.CertPEM)

	if err := RejectServerAuthClientCertificates(nil, [][]*x509.Certificate{{clientLeaf}}); err != nil {
		t.Fatalf("panel client certificate should be accepted: %v", err)
	}
	if err := RejectServerAuthClientCertificates(nil, [][]*x509.Certificate{{serverLeaf}}); err == nil {
		t.Fatal("expected node certificate with ServerAuth to be rejected as a client")
	}
	if err := RejectServerAuthClientCertificates(nil, nil); err == nil {
		t.Fatal("expected missing verified chains to be rejected")
	}
}

func parseTestLeaf(t *testing.T, certificatePEM []byte) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("invalid certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}