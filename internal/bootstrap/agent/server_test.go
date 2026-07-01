package agent

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	agentsecurity "panel/internal/agent/security"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("PANEL_AGENT_LISTEN_ADDRESS", "")
	t.Setenv("PANEL_AGENT_DOCKER_HOST", "")

	cfg := LoadConfig()
	if cfg.ListenAddress != "0.0.0.0:9786" {
		t.Fatalf("listen address = %q", cfg.ListenAddress)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Fatalf("docker host = %q", cfg.DockerHost)
	}
}

func TestServerTLSConfigRequiresVerifiedClientCertificate(t *testing.T) {
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := assets.IssueServerCertificate("panel-agent", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(t.TempDir(), "server.pem")
	keyFile := filepath.Join(t.TempDir(), "server-key.pem")
	if err := os.WriteFile(certFile, serverCert.CertPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, serverCert.KeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := serverTLSConfig(Config{CertFile: certFile, KeyFile: keyFile, CAFile: assets.CAPath})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("min TLS version = %d, want %d", cfg.MinVersion, tls.VersionTLS12)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("client auth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("certificates = %d, want 1", len(cfg.Certificates))
	}
	if cfg.ClientCAs == nil {
		t.Fatal("client CA pool should be configured")
	}
}

func TestNewServerRejectsMissingTLSFiles(t *testing.T) {
	_, err := NewServer(Config{
		ListenAddress: "127.0.0.1:0",
		CertFile:      filepath.Join(t.TempDir(), "missing.pem"),
		KeyFile:       filepath.Join(t.TempDir(), "missing-key.pem"),
		CAFile:        filepath.Join(t.TempDir(), "missing-ca.pem"),
	})
	if err == nil {
		t.Fatal("expected missing TLS files to fail")
	}
}
