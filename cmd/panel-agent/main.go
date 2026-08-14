package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentcli "panel/internal/agent/cli"
	agentbootstrap "panel/internal/bootstrap/agent"

	"google.golang.org/grpc"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches panel-agent modes. The agent service is only started with an
// explicit --srv flag so a bare invocation never accidentally starts a daemon.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--cli":
			return agentcli.Run(args[1:])
		case "--srv":
			return serveAgent()
		case "-h", "--help":
			printUsage(stdout)
			return exitOK
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "panel-agent: missing mode; use --srv to start the agent service or --cli for the read-only CLI")
	} else {
		fmt.Fprintf(stderr, "panel-agent: unknown argument %q\n", args[0])
	}
	printUsage(stderr)
	return exitUsage
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `panel-agent [--cli <command>... | --srv]

Modes:
  --cli <command> [args]   Run the read-only CLI (see "panel-agent --cli apps help")
  --srv                    Run the agent gRPC service (used by systemd)
  -h, --help               Show this help

Run "panel-agent --cli apps help" for apps command details.
`)
}

func serveAgent() int {
	cfg := agentbootstrap.LoadConfig()
	server, err := agentbootstrap.NewServer(cfg)
	if err != nil {
		log.Printf("initialize agent server: %v", err)
		return exitError
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
			log.Printf("server error: %v", err)
			return exitError
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
	return exitOK
}
