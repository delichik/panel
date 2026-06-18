package security

import (
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
