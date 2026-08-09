package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentcli "panel/internal/agent/cli"
	agentbootstrap "panel/internal/bootstrap/agent"

	"google.golang.org/grpc"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--cli" {
		os.Exit(agentcli.Run(os.Args[2:]))
	}
	cfg := agentbootstrap.LoadConfig()
	server, err := agentbootstrap.NewServer(cfg)
	if err != nil {
		log.Fatalf("initialize agent server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("panel agent listening on grpc://%s", cfg.ListenAddress)
		errCh <- server.Serve()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		if !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("server error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("shutdown timeout: %v", ctx.Err())
	}
}
