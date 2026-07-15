package installation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const controlSocketName = "panel-control.sock"

type ControlServer struct {
	path     string
	listener net.Listener
	server   *http.Server
}

func ControlSocketPath(dataRoot string) string {
	return filepath.Join(dataRoot, "run", controlSocketName)
}

func StartControlServer(dataRoot string, setup *SetupService) (*ControlServer, error) {
	if setup == nil {
		return nil, errors.New("setup service is required")
	}
	path := ControlSocketPath(dataRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /setup", func(w http.ResponseWriter, r *http.Request) {
		var input SetupInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		result, err := setup.Run(ctx, input)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	control := &ControlServer{path: path, listener: listener, server: server}
	go func() { _ = server.Serve(listener) }()
	return control, nil
}

func (s *ControlServer) Close() error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.server.Shutdown(ctx)
	_ = s.listener.Close()
	_ = os.Remove(s.path)
	return err
}

func RunSetupThroughControl(ctx context.Context, dataRoot string, input SetupInput) (SetupResult, error) {
	path := ControlSocketPath(dataRoot)
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	body, err := json.Marshal(input)
	if err != nil {
		return SetupResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://panel-control/setup", bytes.NewReader(body))
	if err != nil {
		return SetupResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return SetupResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var failure struct{ Error string `json:"error"` }
		if err := json.NewDecoder(res.Body).Decode(&failure); err != nil {
			return SetupResult{}, err
		}
		return SetupResult{}, errors.New(failure.Error)
	}
	var result SetupResult
	return result, json.NewDecoder(res.Body).Decode(&result)
}
