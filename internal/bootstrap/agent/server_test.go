package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
	agentrpc "panel/internal/agent/rpc"
	agentsecurity "panel/internal/agent/security"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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

func TestAgentServerRejectsNodeCertificateAsClient(t *testing.T) {
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := assets.IssueServerCertificate("panel-agent", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.pem")
	keyFile := filepath.Join(dir, "server-key.pem")
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(cfg)))
	agentrpc.RegisterAgentService(server, agentrpc.NewHandler(agentrpc.HandlerConfig{DockerHost: agentcontract.DefaultDockerHost}))
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(assets.CACertificatePEM()) {
		t.Fatal("invalid agent CA pem")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// A node certificate (ServerAuth+ClientAuth) must not be accepted as a
	// client certificate even though it is signed by the agent CA.
	nodeKeyPair, err := tls.X509KeyPair(serverCert.CertPEM, serverCert.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	nodeClient, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pool,
		Certificates: []tls.Certificate{nodeKeyPair},
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeClient.Close()
	if _, err := agentpb.NewAgentServiceClient(nodeClient).Health(ctx, &agentpb.Empty{}); err == nil {
		t.Fatal("expected node certificate to be rejected as a client")
	}

	// The Panel client certificate (ClientAuth only) still works.
	panelKeyPair, err := tls.X509KeyPair(assets.ClientCertPEM, assets.ClientKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	panelClient, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      pool,
		Certificates: []tls.Certificate{panelKeyPair},
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer panelClient.Close()
	if _, err := agentpb.NewAgentServiceClient(panelClient).Health(ctx, &agentpb.Empty{}); err != nil {
		t.Fatalf("expected panel client certificate to be accepted: %v", err)
	}
}