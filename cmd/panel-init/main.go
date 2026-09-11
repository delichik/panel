package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"panel/internal/modules/backups"
)

type restartRequest struct {
	Mode string `json:"mode"`
}

func main() {
	panelPath := flag.String("panel", defaultPanelPath(), "panel binary path")
	flag.Parse()

	restarts := make(chan string, 1)
	token, err := randomRestartToken()
	if err != nil {
		log.Fatalf("generate restart token failed: %v", err)
	}
	server, restartURL, err := startRestartListener(restarts, token)
	if err != nil {
		log.Fatalf("start restart listener failed: %v", err)
	}
	defer server.Close()

	mode := backups.MaintenanceModeNormal
	for {
		code, restartMode, err := runPanel(*panelPath, restartURL, token, mode, restarts)
		if err != nil {
			log.Fatalf("panel failed: %v", err)
		}
		if restartMode == "" {
			os.Exit(code)
		}
		mode = restartMode
	}
}

func startRestartListener(restarts chan<- string, token string) (*http.Server, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(backups.InitRestartTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req restartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mode := normalizeMode(req.Mode)
		select {
		case restarts <- mode:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("restart listener failed: %v", err)
		}
	}()
	return server, "http://" + listener.Addr().String() + "/restart", nil
}

func runPanel(panelPath, restartURL, token, mode string, restarts <-chan string) (int, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, panelPath, "--maintenance-mode", mode, "--init-restart-url", restartURL, "--init-restart-token", token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return 1, "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	for {
		select {
		case err := <-done:
			// A child that exits on its own may only trigger a queued restart
			// when it exited cleanly. A non-zero exit is a failure and must not
			// be masked by a pending restart request; that mode is only adopted
			// when the supervisor actively stopped the child below.
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return exitErr.ExitCode(), "", nil
				}
				return 1, "", err
			}
			select {
			case nextMode := <-restarts:
				return 0, nextMode, nil
			default:
			}
			return 0, "", nil
		case nextMode := <-restarts:
			// The child may have already exited on its own before the restart
			// request won the select. Its exit status takes precedence: only a
			// clean exit or an active stop may adopt the queued restart.
			select {
			case err := <-done:
				if err != nil {
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) {
						return exitErr.ExitCode(), "", nil
					}
					return 1, "", err
				}
				return 0, nextMode, nil
			default:
			}
			if err := stopPanel(cmd, done); err != nil {
				return 1, "", err
			}
			return 0, nextMode, nil
		}
	}
}

func randomRestartToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func stopPanel(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return err
		}
	case <-timer.C:
		_ = cmd.Process.Kill()
		err := <-done
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return err
		}
	}
	return nil
}

func normalizeMode(mode string) string {
	switch mode {
	case backups.MaintenanceModeExport, backups.MaintenanceModeRestore, backups.MaintenanceModeNormal:
		return mode
	default:
		return backups.MaintenanceModeNormal
	}
}

func defaultPanelPath() string {
	if value := os.Getenv("PANEL_INIT_PANEL_PATH"); value != "" {
		return value
	}
	name := "panel"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), name)
	}
	return name
}
