package facilityapps

import (
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"strings"

	"panel/internal/modules/certificates/proxycert"
)

const (
	proxyCABundleFileName = "ca-certificates.crt"
	proxyCABundleSource   = "certs/_ca_bundle/" + proxyCABundleFileName
	proxyCABundlePath     = proxyTLSMountRoot + "/_ca_bundle/" + proxyCABundleFileName
)

//go:embed ca/ca-certificates.crt
var proxyPublicCABundle []byte

func relayTrustedCertificatePath(cert *proxycert.Certificate) string {
	if cert == nil {
		return ""
	}
	if isSelfSignedProxyCertificate(cert) {
		return certPath(cert.ID, "trusted")
	}
	return proxyCABundlePath
}

func isSelfSignedProxyCertificate(cert *proxycert.Certificate) bool {
	return strings.TrimSpace(cert.Source) == "self_signed_certificate"
}

func relayTrustCertificatePEM(cert *proxycert.Certificate) []byte {
	if cert == nil {
		return nil
	}
	extracted := extractRelayTrustPEM([]byte(cert.CertificatePEM))
	if len(extracted) > 0 {
		return extracted
	}
	return []byte(cert.CertificatePEM)
}

func extractRelayTrustPEM(certificatePEM []byte) []byte {
	var out []byte
	rest := certificatePEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		// Trust anchors for nginx verify: issuing CAs, or a self-signed leaf.
		selfSigned := parsed.CheckSignatureFrom(parsed) == nil
		if !parsed.IsCA && !selfSigned {
			continue
		}
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})...)
	}
	return out
}
