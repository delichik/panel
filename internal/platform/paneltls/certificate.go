package paneltls

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var mu sync.Mutex
var updateMu sync.Mutex
var cachedCertificate atomic.Pointer[tls.Certificate]

// AssetReader supplies the certificate and private key selected for Panel.
// The caller owns persistence and decryption of the asset.
type AssetReader interface {
	ReadFile(context.Context, string, string) ([]byte, string, error)
}

type certificatePairTransaction struct {
	HasPreviousPair bool `json:"hasPreviousPair"`
}

// FixedPairSnapshot is an in-memory copy of the listener pair before a
// settings or asset transaction updates it.
type FixedPairSnapshot struct {
	certificatePEM []byte
	privateKeyPEM  []byte
}

// WithUpdate serializes a fixed-pair activation with the persistence that
// selects it. It is process-local because the listener and cache are process-local.
func WithUpdate(fn func() error) error {
	updateMu.Lock()
	defer updateMu.Unlock()
	return fn()
}

// SnapshotFixedPair returns the current valid fixed pair, if any.
func SnapshotFixedPair(dataRoot string) (FixedPairSnapshot, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := recoverCertificatePairLocked(dataRoot); err != nil {
		return FixedPairSnapshot{}, err
	}
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	certificatePEM, certErr := os.ReadFile(certPath)
	privateKeyPEM, keyErr := os.ReadFile(keyPath)
	if errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		return FixedPairSnapshot{}, nil
	}
	if certErr != nil {
		return FixedPairSnapshot{}, certErr
	}
	if keyErr != nil {
		return FixedPairSnapshot{}, keyErr
	}
	if _, err := tls.X509KeyPair(certificatePEM, privateKeyPEM); err != nil {
		return FixedPairSnapshot{}, err
	}
	return FixedPairSnapshot{certificatePEM: certificatePEM, privateKeyPEM: privateKeyPEM}, nil
}

// RestoreFixedPair restores a pair captured by SnapshotFixedPair and clears
// the listener cache so the restored pair is loaded for subsequent handshakes.
func RestoreFixedPair(dataRoot string, snapshot FixedPairSnapshot) error {
	mu.Lock()
	defer mu.Unlock()
	if err := recoverCertificatePairLocked(dataRoot); err != nil {
		return err
	}
	if len(snapshot.certificatePEM) == 0 && len(snapshot.privateKeyPEM) == 0 {
		certPath, keyPath := fixedCertificatePaths(dataRoot)
		if err := os.Remove(certPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		invalidateCertificate()
		return nil
	}
	if err := writeCertificatePair(dataRoot, snapshot.certificatePEM, snapshot.privateKeyPEM); err != nil {
		return err
	}
	invalidateCertificate()
	return nil
}

// FixedCertificate loads the certificate pair used by the HTTP listener. The
// pair is loaded from the fixed listener location. Startup synchronizes this
// location from the managed key asset before the listener accepts traffic.
func FixedCertificate(dataRoot, fallbackDomain string) (tls.Certificate, error) {
	if certificate := cachedCertificate.Load(); certificate != nil {
		return *certificate, nil
	}
	mu.Lock()
	defer mu.Unlock()
	if certificate := cachedCertificate.Load(); certificate != nil {
		return *certificate, nil
	}

	if err := recoverCertificatePairLocked(dataRoot); err != nil {
		return tls.Certificate{}, err
	}
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, errors.Join(errors.New("Panel TLS certificate pair is unavailable"), err)
	}
	if err := ValidateListenerCertificate(certificate, fallbackDomain); err != nil {
		return tls.Certificate{}, err
	}
	cacheCertificate(certificate)
	return certificate, nil
}

// SyncCertificate writes an already validated, managed certificate pair to the
// fixed listener location. The caller owns the database-backed asset source.
func SyncCertificate(ctx context.Context, dataRoot, domain, assetID string, reader AssetReader) error {
	mu.Lock()
	defer mu.Unlock()
	if err := recoverCertificatePairLocked(dataRoot); err != nil {
		return err
	}

	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return errors.New("Panel TLS asset ID is required")
	}
	if reader == nil {
		return errors.New("Panel TLS asset reader is unavailable")
	}
	certificatePEM, _, err := reader.ReadFile(ctx, assetID, "certificate")
	if err != nil {
		return err
	}
	privateKeyPEM, _, err := reader.ReadFile(ctx, assetID, "private_key")
	if err != nil {
		return err
	}
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return err
	}
	if err := ValidateListenerCertificate(pair, domain); err != nil {
		return err
	}
	if err := writeCertificatePair(dataRoot, certificatePEM, privateKeyPEM); err != nil {
		return err
	}
	invalidateCertificate()
	return nil
}

func cacheCertificate(certificate tls.Certificate) {
	cached := certificate
	cachedCertificate.Store(&cached)
}

func invalidateCertificate() {
	cachedCertificate.Store(nil)
}

func fixedCertificatePaths(dataRoot string) (string, string) {
	dir := filepath.Join(dataRoot, "tls")
	return filepath.Join(dir, "panel.crt"), filepath.Join(dir, "panel.key")
}

func writeCertificatePair(dataRoot string, certificatePEM, privateKeyPEM []byte) error {
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	if _, err := tls.X509KeyPair(certificatePEM, privateKeyPEM); err != nil {
		return err
	}
	if err := recoverCertificatePairLocked(dataRoot); err != nil {
		return err
	}
	previousCertPath, previousKeyPath, markerPath := certificatePairTransactionPaths(dataRoot)
	oldCertificatePEM, certificateErr := os.ReadFile(certPath)
	oldPrivateKeyPEM, keyErr := os.ReadFile(keyPath)
	hasPreviousPair := certificateErr == nil && keyErr == nil
	if hasPreviousPair {
		if _, err := tls.X509KeyPair(oldCertificatePEM, oldPrivateKeyPEM); err != nil {
			hasPreviousPair = false
		}
	}
	if hasPreviousPair {
		if err := os.WriteFile(previousCertPath, oldCertificatePEM, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(previousKeyPath, oldPrivateKeyPEM, 0o600); err != nil {
			return err
		}
	}
	marker, err := json.Marshal(certificatePairTransaction{HasPreviousPair: hasPreviousPair})
	if err != nil {
		return err
	}
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		return err
	}
	certTemporary := certPath + ".new"
	keyTemporary := keyPath + ".new"
	if err := os.WriteFile(certTemporary, certificatePEM, 0o644); err != nil {
		_ = recoverCertificatePairLocked(dataRoot)
		return err
	}
	if err := os.WriteFile(keyTemporary, privateKeyPEM, 0o600); err != nil {
		_ = recoverCertificatePairLocked(dataRoot)
		return err
	}
	if err := replaceFile(certTemporary, certPath); err != nil {
		_ = recoverCertificatePairLocked(dataRoot)
		return err
	}
	if err := replaceFile(keyTemporary, keyPath); err != nil {
		_ = recoverCertificatePairLocked(dataRoot)
		return err
	}
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(previousCertPath)
	_ = os.Remove(previousKeyPath)
	return nil
}

func certificatePairTransactionPaths(dataRoot string) (string, string, string) {
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	return certPath + ".previous", keyPath + ".previous", filepath.Join(filepath.Dir(certPath), "panel.pair-transaction.json")
}

func recoverCertificatePairLocked(dataRoot string) error {
	certPath, keyPath := fixedCertificatePaths(dataRoot)
	previousCertPath, previousKeyPath, markerPath := certificatePairTransactionPaths(dataRoot)
	marker, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	transaction := certificatePairTransaction{}
	if err := json.Unmarshal(marker, &transaction); err != nil {
		if certificatePairIsValid(previousCertPath, previousKeyPath) {
			transaction.HasPreviousPair = true
		} else if certificatePairIsValid(certPath, keyPath) {
			return clearCertificatePairTransaction(certPath, keyPath, previousCertPath, previousKeyPath, markerPath)
		} else {
			return err
		}
	}
	previousCertificatePEM, certErr := os.ReadFile(previousCertPath)
	previousPrivateKeyPEM, keyErr := os.ReadFile(previousKeyPath)
	restored := false
	if transaction.HasPreviousPair && certErr == nil && keyErr == nil {
		if _, err := tls.X509KeyPair(previousCertificatePEM, previousPrivateKeyPEM); err == nil {
			if err := replaceFileContents(certPath, previousCertificatePEM, 0o644); err != nil {
				return err
			}
			if err := replaceFileContents(keyPath, previousPrivateKeyPEM, 0o600); err != nil {
				return err
			}
			restored = true
		}
	}
	if !restored {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}
	return clearCertificatePairTransaction(certPath, keyPath, previousCertPath, previousKeyPath, markerPath)
}

func certificatePairIsValid(certPath, keyPath string) bool {
	certificatePEM, certErr := os.ReadFile(certPath)
	privateKeyPEM, keyErr := os.ReadFile(keyPath)
	if certErr != nil || keyErr != nil {
		return false
	}
	_, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	return err == nil
}

func clearCertificatePairTransaction(certPath, keyPath, previousCertPath, previousKeyPath, markerPath string) error {
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(previousCertPath)
	_ = os.Remove(previousKeyPath)
	_ = os.Remove(certPath + ".new")
	_ = os.Remove(keyPath + ".new")
	return nil
}

func replaceFileContents(path string, contents []byte, mode os.FileMode) error {
	temporary := path + ".recover"
	if err := os.WriteFile(temporary, contents, mode); err != nil {
		return err
	}
	return replaceFile(temporary, path)
}

func replaceFile(source, target string) error {
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, target)
}

// ValidateListenerCertificate enforces the compatibility baseline for the
// public Panel HTTPS listener. Agent mTLS certificates intentionally do not
// use this check and may continue using Ed25519.
func ValidateListenerCertificate(certificate tls.Certificate, domain string) error {
	if len(certificate.Certificate) == 0 {
		return errors.New("Panel TLS certificate is empty")
	}
	parsed := make([]*x509.Certificate, 0, len(certificate.Certificate))
	for _, der := range certificate.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return errors.New("Panel TLS certificate chain is invalid")
		}
		parsed = append(parsed, cert)
	}
	leaf := parsed[0]
	now := time.Now()
	if leaf.IsCA || now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return errors.New("Panel TLS certificate is expired or not yet valid")
	}
	if rsaKey, ok := leaf.PublicKey.(*rsa.PublicKey); !ok || rsaKey.N.BitLen() < 2048 {
		return errors.New("Panel TLS certificate must use an RSA key of at least 2048 bits")
	}
	if !listenerSignatureAlgorithm(leaf.SignatureAlgorithm) {
		return errors.New("Panel TLS certificate uses an unsupported signature algorithm")
	}
	if len(leaf.ExtKeyUsage) > 0 {
		serverAuth := false
		for _, usage := range leaf.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth {
				serverAuth = true
				break
			}
		}
		if !serverAuth {
			return errors.New("Panel TLS certificate is not valid for server authentication")
		}
	}
	if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" && leaf.VerifyHostname(domain) != nil {
		return errors.New("Panel TLS certificate does not cover the Panel domain")
	}
	for index := 1; index < len(parsed); index++ {
		parent := parsed[index]
		if !parent.IsCA || now.Before(parent.NotBefore) || now.After(parent.NotAfter) || !listenerSignatureAlgorithm(parent.SignatureAlgorithm) || parent.KeyUsage&x509.KeyUsageCertSign == 0 {
			return errors.New("Panel TLS certificate chain is not compatible")
		}
		if parsed[index-1].CheckSignatureFrom(parent) != nil {
			return errors.New("Panel TLS certificate chain signature is invalid")
		}
	}
	return nil
}

func listenerSignatureAlgorithm(algorithm x509.SignatureAlgorithm) bool {
	switch algorithm {
	case x509.SHA256WithRSA, x509.SHA384WithRSA, x509.SHA512WithRSA:
		return true
	default:
		return false
	}
}

func newCertificate(domain string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	publicKey := privateKey.Public()
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{domain}
		if domain == "localhost" {
			template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
		}
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), nil
}
