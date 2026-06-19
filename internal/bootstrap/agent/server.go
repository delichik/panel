// Package agent assembles the standalone Panel Agent process.
package agent

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentserver "panel/internal/agent/server"
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

func NewServer(cfg Config) (*http.Server, error) {
	if err := agentcontract.ValidateGeneratedHash(); err != nil {
		return nil, err
	}
	tlsConfig, err := serverTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           agentserver.NewHandler(agentserver.HandlerConfig{DockerHost: cfg.DockerHost}),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}, nil
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
