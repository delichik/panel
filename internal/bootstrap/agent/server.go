// Package agent assembles the standalone Panel Agent process.
package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"

	agentcontract "panel/internal/agent/contract"
	agentrpc "panel/internal/agent/rpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	ListenAddress string
	CertFile      string
	KeyFile       string
	CAFile        string
	DockerHost    string
}

func LoadConfig() Config {
	return Config{
		ListenAddress: envDefault("PANEL_AGENT_LISTEN_ADDRESS", "0.0.0.0:9786"),
		CertFile:      os.Getenv("PANEL_AGENT_CERT_FILE"),
		KeyFile:       os.Getenv("PANEL_AGENT_KEY_FILE"),
		CAFile:        os.Getenv("PANEL_AGENT_CA_FILE"),
		DockerHost:    envDefault("PANEL_AGENT_DOCKER_HOST", agentcontract.DefaultDockerHost),
	}
}

type Server struct {
	Addr string
	grpc *grpc.Server
}

func NewServer(cfg Config) (*Server, error) {
	if err := agentcontract.ValidateGeneratedHash(); err != nil {
		return nil, err
	}
	tlsConfig, err := serverTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	handler := agentrpc.NewHandler(agentrpc.HandlerConfig{DockerHost: cfg.DockerHost})
	agentrpc.RegisterAgentService(grpcServer, handler)
	agentrpc.RegisterAgentReportService(grpcServer, handler)
	return &Server{Addr: cfg.ListenAddress, grpc: grpcServer}, nil
}

func (s *Server) Serve() error {
	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	return s.grpc.Serve(listener)
}

func (s *Server) GracefulStop() {
	s.grpc.GracefulStop()
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func serverTLSConfig(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("invalid panel agent ca pem")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    roots,
	}, nil
}
