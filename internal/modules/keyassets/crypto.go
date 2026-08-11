package keyassets

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	panelerr "panel/internal/platform/errors"
)

type keyMaterial struct {
	algorithm string
	keySize   int
	private   crypto.PrivateKey
	public    crypto.PublicKey
}

type certificateMaterial struct {
	certificate    *x509.Certificate
	certificatePEM []byte
	privateKeyPEM  []byte
	publicKeyPEM   []byte
	keyMaterial    keyMaterial
}

type sshMaterial struct {
	publicKeyText string
	privateKeyPEM []byte
	keyMaterial   keyMaterial
}

func normalizeKeyAlgorithm(algorithm string, keySize int) (string, int, error) {
	algorithm = strings.ToLower(strings.TrimSpace(algorithm))
	switch algorithm {
	case "", AlgorithmEd25519:
		return AlgorithmEd25519, 0, nil
	case AlgorithmRSA:
		switch keySize {
		case 0, 3072:
			return AlgorithmRSA, 3072, nil
		case 2048, 4096:
			return AlgorithmRSA, keySize, nil
		default:
			return "", 0, panelerr.Validation("key_asset_type_invalid", "RSA key size must be 2048, 3072, or 4096 bits")
		}
	default:
		return "", 0, panelerr.Validation("key_asset_type_invalid", "Key asset algorithm must be ed25519 or rsa")
	}
}

func generateKeyMaterial(algorithm string, keySize int) (keyMaterial, error) {
	algorithm, keySize, err := normalizeKeyAlgorithm(algorithm, keySize)
	if err != nil {
		return keyMaterial{}, err
	}
	switch algorithm {
	case AlgorithmEd25519:
		publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return keyMaterial{}, err
		}
		return keyMaterial{algorithm: algorithm, private: privateKey, public: publicKey}, nil
	case AlgorithmRSA:
		privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
		if err != nil {
			return keyMaterial{}, err
		}
		return keyMaterial{algorithm: algorithm, keySize: keySize, private: privateKey, public: privateKey.Public()}, nil
	default:
		return keyMaterial{}, panelerr.Validation("key_asset_type_invalid", "Key asset algorithm must be ed25519 or rsa")
	}
}

func marshalPrivateKeyPEM(privateKey crypto.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func marshalPublicKeyPEM(publicKey crypto.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

func certificateFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func sshFingerprint(publicKey ssh.PublicKey) string {
	return ssh.FingerprintSHA256(publicKey)
}

func parseCertificatePEM(certificatePEM string) (*x509.Certificate, []byte, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(certificatePEM)))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, panelerr.Validation("key_asset_type_invalid", "Certificate PEM is invalid")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, panelerr.Validation("key_asset_type_invalid", "Certificate PEM is invalid")
	}
	return cert, pem.EncodeToMemory(block), nil
}

func parsePrivateKeyPEM(privateKeyPEM string) (keyMaterial, []byte, error) {
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if privateKeyPEM == "" {
		return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is required")
	}
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block != nil {
		if strings.EqualFold(block.Type, "ENCRYPTED PRIVATE KEY") || x509.IsEncryptedPEMBlock(block) || strings.EqualFold(block.Headers["Proc-Type"], "4,ENCRYPTED") {
			return keyMaterial{}, nil, panelerr.Validation("key_asset_encrypted_private_key_unsupported", "Encrypted private keys are not supported")
		}
		switch block.Type {
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is invalid")
			}
			return materialFromPrivateKey(key, pem.EncodeToMemory(block))
		case "RSA PRIVATE KEY":
			key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is invalid")
			}
			return materialFromPrivateKey(key, pem.EncodeToMemory(block))
		case "EC PRIVATE KEY":
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err != nil {
				return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is invalid")
			}
			return materialFromPrivateKey(key, pem.EncodeToMemory(block))
		case "OPENSSH PRIVATE KEY":
			key, err := ssh.ParseRawPrivateKey([]byte(privateKeyPEM))
			if err != nil {
				var passErr *ssh.PassphraseMissingError
				if errors.As(err, &passErr) {
					return keyMaterial{}, nil, panelerr.Validation("key_asset_encrypted_private_key_unsupported", "Encrypted private keys are not supported")
				}
				return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is invalid")
			}
			return materialFromPrivateKey(key, []byte(privateKeyPEM))
		}
	}
	key, err := ssh.ParseRawPrivateKey([]byte(privateKeyPEM))
	if err != nil {
		var passErr *ssh.PassphraseMissingError
		if errors.As(err, &passErr) {
			return keyMaterial{}, nil, panelerr.Validation("key_asset_encrypted_private_key_unsupported", "Encrypted private keys are not supported")
		}
		return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Private key PEM is invalid")
	}
	return materialFromPrivateKey(key, []byte(privateKeyPEM))
}

func materialFromPrivateKey(privateKey any, encoded []byte) (keyMaterial, []byte, error) {
	switch key := privateKey.(type) {
	case ed25519.PrivateKey:
		return keyMaterial{algorithm: AlgorithmEd25519, private: key, public: key.Public()}, encoded, nil
	case *rsa.PrivateKey:
		return keyMaterial{algorithm: AlgorithmRSA, keySize: key.N.BitLen(), private: key, public: key.Public()}, encoded, nil
	default:
		return keyMaterial{}, nil, panelerr.Validation("key_asset_type_invalid", "Only ed25519 and rsa key assets are supported")
	}
}

func parsePublicKeyText(publicKey string) (string, crypto.PublicKey, error) {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return "", nil, nil
	}
	if strings.Contains(publicKey, "BEGIN ") {
		block, _ := pem.Decode([]byte(publicKey))
		if block == nil {
			return "", nil, panelerr.Validation("key_asset_type_invalid", "Public key is invalid")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return "", nil, panelerr.Validation("key_asset_type_invalid", "Public key is invalid")
		}
		return string(pem.EncodeToMemory(block)), parsed, nil
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", nil, panelerr.Validation("key_asset_type_invalid", "Public key is invalid")
	}
	cryptoPub, ok := parsed.(ssh.CryptoPublicKey)
	if !ok {
		return "", nil, panelerr.Validation("key_asset_type_invalid", "Public key is invalid")
	}
	return string(ssh.MarshalAuthorizedKey(parsed)), cryptoPub.CryptoPublicKey(), nil
}

func generateCertificate(template, parent *x509.Certificate, publicKey crypto.PublicKey, signer crypto.PrivateKey) ([]byte, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, parent, publicKey, signer)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

func buildCertificateMaterial(cert *x509.Certificate, certificatePEM []byte, key keyMaterial, privateKeyPEM []byte) (certificateMaterial, error) {
	publicKeyPEM, err := marshalPublicKeyPEM(key.public)
	if err != nil {
		return certificateMaterial{}, err
	}
	return certificateMaterial{
		certificate:    cert,
		certificatePEM: certificatePEM,
		privateKeyPEM:  privateKeyPEM,
		publicKeyPEM:   publicKeyPEM,
		keyMaterial:    key,
	}, nil
}

func buildSSHMaterial(key keyMaterial, privateKeyPEM []byte, comment string) (sshMaterial, error) {
	sshPublic, err := ssh.NewPublicKey(key.public)
	if err != nil {
		return sshMaterial{}, err
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))
	if comment = strings.TrimSpace(comment); comment != "" {
		authorized += " " + comment
	}
	return sshMaterial{
		publicKeyText: authorized,
		privateKeyPEM: privateKeyPEM,
		keyMaterial:   key,
	}, nil
}

func ensurePublicKeyMatches(expected, actual crypto.PublicKey) error {
	if expected == nil || actual == nil {
		return nil
	}
	expectedDER, err := x509.MarshalPKIXPublicKey(expected)
	if err != nil {
		return err
	}
	actualDER, err := x509.MarshalPKIXPublicKey(actual)
	if err != nil {
		return err
	}
	if hex.EncodeToString(expectedDER) != hex.EncodeToString(actualDER) {
		return panelerr.Validation("key_asset_key_pair_mismatch", "Public key does not match the private key")
	}
	return nil
}

func parseIPs(values []string) ([]string, []net.IP, error) {
	seen := map[string]struct{}{}
	text := make([]string, 0, len(values))
	ips := make([]net.IP, 0, len(values))
	for _, value := range values {
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil {
			return nil, nil, panelerr.Validation("key_asset_type_invalid", "IP address is invalid")
		}
		normalized := ip.String()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		text = append(text, normalized)
		ips = append(ips, ip)
	}
	return text, ips, nil
}

func normalizeDNSNames(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func defaultTLSCommonName(name string, dnsNames []string, ipAddresses []string) string {
	if len(dnsNames) > 0 {
		return dnsNames[0]
	}
	if len(ipAddresses) > 0 {
		return ipAddresses[0]
	}
	return strings.TrimSpace(name)
}

func newCertificateTemplate(commonName string, notBefore, notAfter time.Time, isCA bool, dnsNames []string, ips []net.IP) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		Subject:               pkixName(commonName),
	}
	if isCA {
		template.IsCA = true
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
		return template, nil
	}
	template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	template.DNSNames = dnsNames
	template.IPAddresses = ips
	return template, nil
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: strings.TrimSpace(commonName), Organization: []string{"Panel"}}
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	return serial, nil
}

func fileKindsForAsset(assetType string) []string {
	switch assetType {
	case TypeCACertificate:
		return []string{"certificate", "private_key", "public_key"}
	case TypeTLSCertificate:
		return []string{"certificate", "private_key", "public_key"}
	case TypeSSHKeyPair:
		return []string{"private_key", "ssh_public_key"}
	default:
		return nil
	}
}

func publicKeyDisplayForCertificate(publicKeyPEM []byte) string {
	return string(publicKeyPEM)
}

func certificateFileName(asset Asset, kind string) string {
	base := strings.ReplaceAll(strings.TrimSpace(asset.Name), " ", "-")
	if base == "" {
		base = asset.ID
	}
	return fmt.Sprintf("%s-%s.pem", base, kind)
}
