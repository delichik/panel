package facilityapps

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"panel/internal/modules/certificates/proxycert"
)

func TestRelayTrustedCertificatePathUsesCABundleForDomainCerts(t *testing.T) {
	cert := &proxycert.Certificate{ID: "acme-1", Source: "domain_certificate"}
	if got := relayTrustedCertificatePath(cert); got != proxyCABundlePath {
		t.Fatalf("domain cert trust path = %q, want %q", got, proxyCABundlePath)
	}
	selfSigned := &proxycert.Certificate{ID: "ss-1", Source: "self_signed_certificate"}
	if got := relayTrustedCertificatePath(selfSigned); got != certPath("ss-1", "trusted") {
		t.Fatalf("self-signed trust path = %q", got)
	}
}

func TestExtractRelayTrustPEMKeepsCAAndSelfSignedOnly(t *testing.T) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "leaf.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     []string{"leaf.example.test"},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	bundle := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...)
	trust := extractRelayTrustPEM(bundle)
	block, rest := pem.Decode(trust)
	if block == nil || len(pem.EncodeToMemory(block)) == 0 {
		t.Fatal("expected CA in trust PEM")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.IsCA || parsed.Subject.CommonName != "test-ca" {
		t.Fatalf("trusted cert = %#v", parsed.Subject)
	}
	if next, _ := pem.Decode(rest); next != nil {
		t.Fatal("leaf must not appear in trust PEM")
	}
}
