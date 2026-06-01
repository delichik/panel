package nomad

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"panel/internal/config"
)

func TestEnsureTLSAssetsGeneratesAndReloadsFiles(t *testing.T) {
	dir := t.TempDir()
	assets, err := EnsureTLSAssets(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(assets.CAPEM) == 0 || len(assets.AgentCertPEM) == 0 || len(assets.ClientCertPEM) == 0 {
		t.Fatalf("expected generated TLS assets, got %#v", assets)
	}
	reloaded, err := EnsureTLSAssets(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if string(assets.CAPEM) != string(reloaded.CAPEM) || string(assets.ClientCertPEM) != string(reloaded.ClientCertPEM) {
		t.Fatal("expected TLS assets to be reused when files already exist")
	}
}

func TestClientConnectsWithManagedMTLSAssets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	assets, err := EnsureTLSAssets(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := tls.LoadX509KeyPair(assets.AgentCertPath, assets.AgentKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	clientRoots := x509.NewCertPool()
	if !clientRoots.AppendCertsFromPEM(assets.CAPEM) {
		t.Fatal("failed to append CA")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode("leader.example.internal:4647")
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientRoots,
	}
	server.StartTLS()
	defer server.Close()

	client := NewClient(Config{
		Address: server.URL,
		TLS: &TLSConfig{
			CAFile:             assets.CAPath,
			CertFile:           assets.ClientCertPath,
			KeyFile:            assets.ClientKeyPath,
			SkipVerifyHostname: true,
		},
	}, nil)

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.Leader != "\"leader.example.internal:4647\"\n" {
		t.Fatalf("status = %#v", status)
	}
}
