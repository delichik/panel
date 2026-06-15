package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"panel/internal/agent"
)

func main() {
	cfg := loadConfig()
	tlsConfig, err := serverTLSConfig(cfg)
	if err != nil {
		log.Fatalf("load tls config: %v", err)
	}
	server := &http.Server{
		Addr:              cfg.listenAddress,
		Handler:           agent.NewHandler(agent.HandlerConfig{DockerHost: cfg.dockerHost}),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("panel agent listening on https://%s", cfg.listenAddress)
		errCh <- server.ListenAndServeTLS("", "")
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

type config struct {
	listenAddress string
	certFile      string
	keyFile       string
	caFile        string
	dockerHost    string
}

func loadConfig() config {
	return config{
		listenAddress: envDefault("PANEL_AGENT_LISTEN_ADDRESS", "0.0.0.0:9443"),
		certFile:      os.Getenv("PANEL_AGENT_CERT_FILE"),
		keyFile:       os.Getenv("PANEL_AGENT_KEY_FILE"),
		caFile:        os.Getenv("PANEL_AGENT_CA_FILE"),
		dockerHost:    envDefault("PANEL_AGENT_DOCKER_HOST", agent.DefaultDockerHost),
	}
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func serverTLSConfig(cfg config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.certFile, cfg.keyFile)
	if err != nil {
		return nil, err
	}
	caPEM, err := os.ReadFile(cfg.caFile)
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
