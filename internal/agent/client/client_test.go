package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
	agentrpc "panel/internal/agent/rpc"
	agentsecurity "panel/internal/agent/security"
	panelerr "panel/internal/platform/errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

func TestGRPCClientHealthCapturesPeerCertificateInfo(t *testing.T) {
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{keyPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	})))
	agentrpc.RegisterAgentService(server, agentrpc.NewHandler(agentrpc.HandlerConfig{DockerHost: "unix:///tmp/panel-test.sock"}))
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Stop()
	client, err := NewGRPCClient(assets, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	health, err := client.Health(context.Background(), "https://"+listener.Addr().String())
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

func TestWrapAgentErrorAsBadGateway(t *testing.T) {
	err := wrapAgentError(status.Error(codes.Internal, "ufw: permission denied"))
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

type prepareRestartServerStub struct {
	agentpb.UnimplementedAgentServiceServer
	holdOnCount int
	holdOnDelay time.Duration
	block       bool
}

func (s *prepareRestartServerStub) PrepareRestart(_ *agentpb.Empty, stream agentpb.AgentService_PrepareRestartServer) error {
	ctx := stream.Context()
	if s.block {
		<-ctx.Done()
		return ctx.Err()
	}
	for i := 0; i < s.holdOnCount; i++ {
		if err := stream.Send(&agentpb.PrepareRestartResponse{State: agentcontract.PrepareRestartStateHoldOn}); err != nil {
			return err
		}
		if s.holdOnDelay > 0 {
			timer := time.NewTimer(s.holdOnDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return stream.Send(&agentpb.PrepareRestartResponse{State: agentcontract.PrepareRestartStateReady})
}

func newPrepareRestartTestClient(t *testing.T, impl agentpb.AgentServiceServer) (*GRPCClient, string, func()) {
	t.Helper()
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{keyPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	})))
	agentpb.RegisterAgentServiceServer(server, impl)
	go func() {
		_ = server.Serve(listener)
	}()
	client, err := NewGRPCClient(assets, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client, "https://" + listener.Addr().String(), func() { server.Stop() }
}

func TestGRPCClientPrepareRestartReadyImmediately(t *testing.T) {
	client, endpoint, stop := newPrepareRestartTestClient(t, &prepareRestartServerStub{})
	defer stop()
	if err := client.PrepareRestart(context.Background(), endpoint); err != nil {
		t.Fatalf("PrepareRestart returned error: %v", err)
	}
}

func TestGRPCClientPrepareRestartWaitsForReady(t *testing.T) {
	client, endpoint, stop := newPrepareRestartTestClient(t, &prepareRestartServerStub{holdOnCount: 2, holdOnDelay: 150 * time.Millisecond})
	defer stop()
	started := time.Now()
	if err := client.PrepareRestart(context.Background(), endpoint); err != nil {
		t.Fatalf("PrepareRestart returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 250*time.Millisecond {
		t.Fatalf("expected PrepareRestart to wait for holdon messages, returned after %v", elapsed)
	}
}

func TestGRPCClientPrepareRestartCancelled(t *testing.T) {
	client, endpoint, stop := newPrepareRestartTestClient(t, &prepareRestartServerStub{block: true})
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := client.PrepareRestart(ctx, endpoint)
	if err == nil {
		t.Fatal("expected PrepareRestart to fail when the stream is cancelled")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
