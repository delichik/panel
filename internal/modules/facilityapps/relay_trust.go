package facilityapps

import (
	"crypto/x509"
	"encoding/pem"
	"strings"

	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/modules/certificates/proxycert"
)

const (
	// Host CA bundle used for public/ACME AnyAccess relay verify.
	// Bound from the gateway host into the nginx container — never vendored
	// in the Panel git repo. Path matches Debian/Ubuntu/Alpine ca-certificates.
	proxyHostCABundleHostPath  = "/etc/ssl/certs/ca-certificates.crt"
	proxyHostCABundleMountPath = "/etc/ssl/certs/ca-certificates.crt"
)

func relayTrustedCertificatePath(cert *proxycert.Certificate) string {
	if cert == nil {
		return ""
	}
	if isSelfSignedProxyCertificate(cert) {
		return certPath(cert.ID, "trusted")
	}
	return proxyHostCABundleMountPath
}

func proxyHostCABundleMount() appruntime.Mount {
	return appruntime.Mount{
		Type:     "host",
		Source:   proxyHostCABundleHostPath,
		Target:   proxyHostCABundleMountPath,
		ReadOnly: true,
	}
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
