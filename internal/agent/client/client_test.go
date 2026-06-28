package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	agentsecurity "panel/internal/agent/security"
	agentserver "panel/internal/agent/server"
	panelerr "panel/internal/platform/errors"
)

func TestHTTPClientHealthCapturesPeerCertificateInfo(t *testing.T) {
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := assets.IssueServerCertificate("panel-agent-test", []string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	keyPair, err := tls.X509KeyPair(serverCert.CertPEM, serverCert.KeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(assets.CACertificatePEM()) {
		t.Fatal("invalid test CA")
	}
	server := httptest.NewUnstartedServer(agentserver.NewHandler(agentserver.HandlerConfig{DockerHost: "unix:///tmp/panel-test.sock"}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{keyPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}
	server.StartTLS()
	defer server.Close()
	client, err := NewHTTPClient(assets, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	health, err := client.Health(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if health.Certificate == nil {
		t.Fatal("expected health response to include peer certificate info")
	}
	issued, err := agentsecurity.ParseCertificateInfo(serverCert.CertPEM)
	if err != nil {
		t.Fatal(err)
	}
	if health.Certificate.Fingerprint != issued.Fingerprint || !health.Certificate.NotAfter.Equal(issued.NotAfter) {
		t.Fatalf("unexpected peer certificate info: got %#v want %#v", health.Certificate, issued)
	}
}

func TestDecodeAgentResponseWrapsRemoteErrorAsBadGateway(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Code = http.StatusBadGateway
	rec.Body.WriteString(`{"error":"ufw: permission denied"}`)

	err := decodeAgentResponse(rec.Result(), nil)
	if err == nil {
		t.Fatal("expected agent error")
	}
	var domain *panelerr.Error
	if !errors.As(err, &domain) {
		t.Fatalf("expected platform error, got %T: %v", err, err)
	}
	if domain.HTTPStatus != http.StatusBadGateway || domain.Code != "agent_request_failed" {
		t.Fatalf("unexpected wrapped error: %#v", domain)
	}
	if domain.Message != "Agent request failed: ufw: permission denied" {
		t.Fatalf("unexpected message: %q", domain.Message)
	}
}
