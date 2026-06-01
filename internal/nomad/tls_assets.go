package nomad

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type TLSAssets struct {
	Dir            string
	CAPath         string
	CAKeyPath      string
	AgentCertPath  string
	AgentKeyPath   string
	ClientCertPath string
	ClientKeyPath  string
	CAPEM          []byte
	AgentCertPEM   []byte
	AgentKeyPEM    []byte
	ClientCertPEM  []byte
	ClientKeyPEM   []byte
}

func EnsureTLSAssets(dataRoot string) (*TLSAssets, error) {
	assets := tlsAssetsPaths(dataRoot)
	if err := os.MkdirAll(assets.Dir, 0o700); err != nil {
		return nil, err
	}
	if assetsExist(assets) {
		return loadTLSAssets(assets)
	}
	if err := generateTLSAssets(assets); err != nil {
		return nil, err
	}
	return loadTLSAssets(assets)
}

func tlsAssetsPaths(dataRoot string) *TLSAssets {
	dir := filepath.Join(dataRoot, "nomad", "tls")
	return &TLSAssets{
		Dir:            dir,
		CAPath:         filepath.Join(dir, "ca.pem"),
		CAKeyPath:      filepath.Join(dir, "ca-key.pem"),
		AgentCertPath:  filepath.Join(dir, "agent.pem"),
		AgentKeyPath:   filepath.Join(dir, "agent-key.pem"),
		ClientCertPath: filepath.Join(dir, "panel-client.pem"),
		ClientKeyPath:  filepath.Join(dir, "panel-client-key.pem"),
	}
}

func assetsExist(assets *TLSAssets) bool {
	for _, path := range []string{
		assets.CAPath,
		assets.CAKeyPath,
		assets.AgentCertPath,
		assets.AgentKeyPath,
		assets.ClientCertPath,
		assets.ClientKeyPath,
	} {
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
	if assets.AgentCertPEM, err = os.ReadFile(assets.AgentCertPath); err != nil {
		return nil, err
	}
	if assets.AgentKeyPEM, err = os.ReadFile(assets.AgentKeyPath); err != nil {
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
	caKey, caCertPEM, caKeyPEM, err := generateCA()
	if err != nil {
		return err
	}
	agentCertPEM, agentKeyPEM, err := generateLeafCert(caKey, caCertPEM, leafCertRequest{
		CommonName: "panel-nomad-cluster",
		DNSNames:   []string{"localhost", "nomad.service", "panel-nomad-cluster"},
		IPAddrs:    []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return err
	}
	clientCertPEM, clientKeyPEM, err := generateLeafCert(caKey, caCertPEM, leafCertRequest{
		CommonName: "panel-nomad-client",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return err
	}
	files := []struct {
		path string
		data []byte
	}{
		{assets.CAPath, caCertPEM},
		{assets.CAKeyPath, caKeyPEM},
		{assets.AgentCertPath, agentCertPEM},
		{assets.AgentKeyPath, agentKeyPEM},
		{assets.ClientCertPath, clientCertPEM},
		{assets.ClientKeyPath, clientKeyPEM},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, file.data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

type leafCertRequest struct {
	CommonName string
	DNSNames   []string
	IPAddrs    []net.IP
	Usages     []x509.ExtKeyUsage
}

func generateCA() (*ecdsa.PrivateKey, []byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject: pkix.Name{
			CommonName:   "Panel Nomad Local CA",
			Organization: []string{"Panel"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	return key,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		nil
}

func generateLeafCert(caKey *ecdsa.PrivateKey, caCertPEM []byte, req leafCertRequest) ([]byte, []byte, error) {
	block, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject: pkix.Name{
			CommonName:   req.CommonName,
			Organization: []string{"Panel"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           req.Usages,
		BasicConstraintsValid: true,
		DNSNames:              req.DNSNames,
		IPAddresses:           req.IPAddrs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
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

func (a *TLSAssets) ClientTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(a.ClientCertPath, a.ClientKeyPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(a.CAPEM) {
		return nil, errors.New("invalid nomad tls ca pem")
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		RootCAs:            roots,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("nomad tls peer certificate missing")
			}
			leaf, intermediates, err := parsePeerCerts(rawCerts)
			if err != nil {
				return err
			}
			_, err = leaf.Verify(x509.VerifyOptions{
				Roots:         roots,
				Intermediates: intermediates,
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			})
			return err
		},
	}, nil
}

func parsePeerCerts(rawCerts [][]byte) (*x509.Certificate, *x509.CertPool, error) {
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, nil, err
	}
	intermediates := x509.NewCertPool()
	for _, raw := range rawCerts[1:] {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return nil, nil, err
		}
		intermediates.AddCert(cert)
	}
	return leaf, intermediates, nil
}
