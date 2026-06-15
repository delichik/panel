package agent

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TLSAssets struct {
	mu             sync.RWMutex
	Dir            string
	CAPath         string
	CAKeyPath      string
	ClientCertPath string
	ClientKeyPath  string
	CAPEM          []byte
	ClientCertPEM  []byte
	ClientKeyPEM   []byte
}

type ServerCertificate struct {
	CertPEM []byte
	KeyPEM  []byte
}

type CertificateInfo struct {
	Fingerprint string
	CommonName  string
	NotBefore   time.Time
	NotAfter    time.Time
}

type tlsFile struct {
	path string
	data []byte
}

func EnsureTLSAssets(dataRoot string) (*TLSAssets, error) {
	assets := tlsAssetsPaths(dataRoot)
	if err := os.MkdirAll(assets.Dir, 0o700); err != nil {
		return nil, err
	}
	if tlsAssetsExist(assets) {
		return loadTLSAssets(assets)
	}
	if err := generateTLSAssets(assets); err != nil {
		return nil, err
	}
	return loadTLSAssets(assets)
}

func tlsAssetsPaths(dataRoot string) *TLSAssets {
	dir := filepath.Join(dataRoot, "agent", "tls")
	return &TLSAssets{
		Dir:            dir,
		CAPath:         filepath.Join(dir, "ca.pem"),
		CAKeyPath:      filepath.Join(dir, "ca-key.pem"),
		ClientCertPath: filepath.Join(dir, "panel-client.pem"),
		ClientKeyPath:  filepath.Join(dir, "panel-client-key.pem"),
	}
}

func tlsAssetsExist(assets *TLSAssets) bool {
	for _, path := range []string{assets.CAPath, assets.CAKeyPath, assets.ClientCertPath, assets.ClientKeyPath} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func loadTLSAssets(assets *TLSAssets) (*TLSAssets, error) {
	var err error
	if assets.CAPEM, err = os.ReadFile(assets.CAPath); err != nil {
		return nil, err
	}
	if assets.ClientCertPEM, err = os.ReadFile(assets.ClientCertPath); err != nil {
		return nil, err
	}
	if assets.ClientKeyPEM, err = os.ReadFile(assets.ClientKeyPath); err != nil {
		return nil, err
	}
	return assets, nil
}

func generateTLSAssets(assets *TLSAssets) error {
	caKey, caCert, caPEM, caKeyPEM, err := generateCA()
	if err != nil {
		return err
	}
	clientCertPEM, clientKeyPEM, err := generateLeaf(caCert, caKey, leafRequest{
		CommonName: "panel-agent-client",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return err
	}
	return writeTLSFiles([]tlsFile{
		{assets.CAPath, caPEM},
		{assets.CAKeyPath, caKeyPEM},
		{assets.ClientCertPath, clientCertPEM},
		{assets.ClientKeyPath, clientKeyPEM},
	})
}

func writeTLSFiles(files []tlsFile) error {
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (a *TLSAssets) ClientTLSConfig() (*tls.Config, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cert, err := tls.LoadX509KeyPair(a.ClientCertPath, a.ClientKeyPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(a.CAPEM) {
		return nil, errors.New("invalid agent ca pem")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
	}, nil
}

func (a *TLSAssets) CAInfo() (CertificateInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return ParseCertificateInfo(a.CAPEM)
}

func (a *TLSAssets) ClientInfo() (CertificateInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return ParseCertificateInfo(a.ClientCertPEM)
}

func (a *TLSAssets) CACertificatePEM() []byte {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]byte(nil), a.CAPEM...)
}

func (a *TLSAssets) ResetAll() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	caKey, caCert, caPEM, caKeyPEM, err := generateCA()
	if err != nil {
		return err
	}
	clientCertPEM, clientKeyPEM, err := generateLeaf(caCert, caKey, leafRequest{
		CommonName: "panel-agent-client",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return err
	}
	if err := writeTLSFiles([]tlsFile{
		{a.CAPath, caPEM},
		{a.CAKeyPath, caKeyPEM},
		{a.ClientCertPath, clientCertPEM},
		{a.ClientKeyPath, clientKeyPEM},
	}); err != nil {
		return err
	}
	a.CAPEM = caPEM
	a.ClientCertPEM = clientCertPEM
	a.ClientKeyPEM = clientKeyPEM
	return nil
}

func (a *TLSAssets) ResetClientCertificate() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	caCert, caKey, err := loadCA(a.CAPEM, a.CAKeyPath)
	if err != nil {
		return err
	}
	clientCertPEM, clientKeyPEM, err := generateLeaf(caCert, caKey, leafRequest{
		CommonName: "panel-agent-client",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return err
	}
	if err := writeTLSFiles([]tlsFile{
		{a.ClientCertPath, clientCertPEM},
		{a.ClientKeyPath, clientKeyPEM},
	}); err != nil {
		return err
	}
	a.ClientCertPEM = clientCertPEM
	a.ClientKeyPEM = clientKeyPEM
	return nil
}

func (a *TLSAssets) IssueServerCertificate(commonName string, hosts []string) (ServerCertificate, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	caCert, caKey, err := loadCA(a.CAPEM, a.CAKeyPath)
	if err != nil {
		return ServerCertificate{}, err
	}
	req := leafRequest{CommonName: commonName, Usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			req.IPAddrs = append(req.IPAddrs, ip)
		} else {
			req.DNSNames = append(req.DNSNames, strings.ToLower(host))
		}
	}
	certPEM, keyPEM, err := generateLeaf(caCert, caKey, req)
	if err != nil {
		return ServerCertificate{}, err
	}
	return ServerCertificate{CertPEM: certPEM, KeyPEM: keyPEM}, nil
}

type leafRequest struct {
	CommonName string
	DNSNames   []string
	IPAddrs    []net.IP
	Usages     []x509.ExtKeyUsage
}

func generateCA() (crypto.Signer, *x509.Certificate, []byte, []byte, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: "Panel Agent CA", Organization: []string{"Panel"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, key.Public(), key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return key, cert, certPEM, keyPEM, nil
}

func loadCA(caPEM []byte, keyPath string) (*x509.Certificate, crypto.Signer, error) {
	block, _ := pem.Decode(caPEM)
	if block == nil {
		return nil, nil, errors.New("invalid agent ca pem")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, errors.New("invalid agent ca key pem")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, nil, errors.New("agent ca key is not a signer")
	}
	return caCert, signer, nil
}

func generateLeaf(caCert *x509.Certificate, caKey crypto.Signer, req leafRequest) ([]byte, []byte, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: strings.TrimSpace(req.CommonName), Organization: []string{"Panel"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(3, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           req.Usages,
		BasicConstraintsValid: true,
		DNSNames:              req.DNSNames,
		IPAddresses:           req.IPAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		nil
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}

func ParseCertificateInfo(certificatePEM []byte) (CertificateInfo, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return CertificateInfo{}, errors.New("invalid certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertificateInfo{}, err
	}
	sum := sha256.Sum256(cert.Raw)
	return CertificateInfo{
		Fingerprint: fmt.Sprintf("%X", sum[:]),
		CommonName:  cert.Subject.CommonName,
		NotBefore:   cert.NotBefore,
		NotAfter:    cert.NotAfter,
	}, nil
}
