package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/backups"
)

var (
	helperMaintenanceMode    = flag.String("maintenance-mode", "", "panel-init helper maintenance mode")
	helperInitRestartURL     = flag.String("init-restart-url", "", "panel-init helper restart URL")
	helperInitRestartToken   = flag.String("init-restart-token", "", "panel-init helper restart token")
	restartTokenShapePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

func TestMain(m *testing.M) {
	if os.Getenv("PANEL_INIT_HELPER_PROCESS") == "1" {
		// The test helper runs before testing.Main parses flags, so parse the
		// supervisor-supplied arguments before reading the flag values.
		flag.Parse()
		runPanelInitHelper()
		return
	}
	os.Exit(m.Run())
}

func TestRestartModeContractAllowsOnlyMaintenanceModes(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "normal", mode: backups.MaintenanceModeNormal, want: backups.MaintenanceModeNormal},
		{name: "export", mode: backups.MaintenanceModeExport, want: backups.MaintenanceModeExport},
		{name: "restore", mode: backups.MaintenanceModeRestore, want: backups.MaintenanceModeRestore},
		{name: "empty defaults normal", mode: "", want: backups.MaintenanceModeNormal},
		{name: "unknown defaults normal", mode: "surprise", want: backups.MaintenanceModeNormal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMode(tt.mode); got != tt.want {
				t.Fatalf("normalizeMode(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestPanelInitRestartListenerRequiresValidToken(t *testing.T) {
	restarts := make(chan string, 1)
	server, restartURL, err := startRestartListener(restarts, "secret-token")
	if err != nil {
		t.Fatalf("startRestartListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, restartURL, "wrong-token", `{"mode":"restore"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
	}
	assertNoRestart(t, restarts)
}

func TestPanelInitRestartListenerRejectsMalformedRestartRequest(t *testing.T) {
	restarts := make(chan string, 1)
	server, restartURL, err := startRestartListener(restarts, "secret-token")
	if err != nil {
		t.Fatalf("startRestartListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, restartURL, "secret-token", `{`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
	}
	assertNoRestart(t, restarts)
}

func TestPanelInitRestartListenerAcceptsAuthorizedRestartRequest(t *testing.T) {
	restarts := make(chan string, 1)
	server, restartURL, err := startRestartListener(restarts, "secret-token")
	if err != nil {
		t.Fatalf("startRestartListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, restartURL, "secret-token", `{"mode":"restore"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
	}
	assertRestart(t, restarts, backups.MaintenanceModeRestore)
}

func TestPanelInitRestartListenerFallsBackToNormalForInvalidMode(t *testing.T) {
	restarts := make(chan string, 1)
	server, restartURL, err := startRestartListener(restarts, "secret-token")
	if err != nil {
		t.Fatalf("startRestartListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, restartURL, "secret-token", `{"mode":"invalid"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
	}
	assertRestart(t, restarts, backups.MaintenanceModeNormal)
}

func TestPanelInitPanelPathCanBeOverriddenByEnvironment(t *testing.T) {
	want := filepath.Join("custom", "panel-binary")
	t.Setenv("PANEL_INIT_PANEL_PATH", want)

	if got := defaultPanelPath(); got != want {
		t.Fatalf("defaultPanelPath() = %q, want %q", got, want)
	}
}

func TestPanelInitPanelPathDefaultsNextToSupervisorExecutable(t *testing.T) {
	t.Setenv("PANEL_INIT_PANEL_PATH", "")

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	name := "panel"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	want := filepath.Join(filepath.Dir(exe), name)

	if got := defaultPanelPath(); got != want {
		t.Fatalf("defaultPanelPath() = %q, want %q", got, want)
	}
}

func TestPanelInitRestartTokenIsURLSafeRandomSecret(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 8; i++ {
		token, err := randomRestartToken()
		if err != nil {
			t.Fatalf("randomRestartToken() error = %v", err)
		}
		if !restartTokenShapePattern.MatchString(token) {
			t.Fatalf("token %q does not match expected URL-safe raw base64 shape", token)
		}
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token %q did not decode as raw URL base64: %v", token, err)
		}
		if len(raw) != 32 {
			t.Fatalf("decoded token length = %d, want 32", len(raw))
		}
		if _, ok := seen[token]; ok {
			t.Fatalf("randomRestartToken() returned duplicate token %q", token)
		}
		seen[token] = struct{}{}
	}
}

func TestPanelInitRestartRequestStopsChildAndSwitchesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt is not a fast, portable child-process stop signal on Windows")
	}

	helperDir := panelInitTestTmpDir(t)
	recordPath := filepath.Join(helperDir, "helper.txt")
	t.Setenv("PANEL_INIT_HELPER_PROCESS", "1")
	t.Setenv("PANEL_INIT_HELPER_WAIT", "1")
	t.Setenv("PANEL_INIT_HELPER_RECORD", recordPath)

	restarts := make(chan string, 1)
	result := make(chan struct {
		code int
		mode string
		err  error
	}, 1)

	go func() {
		code, mode, err := runPanel(os.Args[0], "http://127.0.0.1/restart", "restart-token", backups.MaintenanceModeNormal, restarts)
		result <- struct {
			code int
			mode string
			err  error
		}{code: code, mode: mode, err: err}
	}()

	waitForFile(t, recordPath)
	restarts <- backups.MaintenanceModeRestore

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("runPanel() error = %v", got.err)
		}
		if got.code != 0 || got.mode != backups.MaintenanceModeRestore {
			t.Fatalf("restart request returned child exit code %d and next mode %q, want code 0 and mode %q", got.code, got.mode, backups.MaintenanceModeRestore)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runPanel() did not return after restart request")
	}

	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read helper record: %v", err)
	}
	text := string(record)
	for _, want := range []string{
		"mode=" + backups.MaintenanceModeNormal,
		"restartURL=http://127.0.0.1/restart",
		"token=restart-token",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("helper record missing %q:\n%s", want, text)
		}
	}
}

func postRestart(t *testing.T, restartURL, token, body string) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, restartURL, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(backups.InitRestartTokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s error = %v", restartURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func assertRestart(t *testing.T, restarts <-chan string, want string) {
	t.Helper()

	select {
	case got := <-restarts:
		if got != want {
			t.Fatalf("restart mode = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for restart mode %q", want)
	}
}

func assertNoRestart(t *testing.T, restarts <-chan string) {
	t.Helper()

	select {
	case got := <-restarts:
		t.Fatalf("unexpected restart mode %q", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func panelInitTestTmpDir(t *testing.T) string {
	t.Helper()

	name := strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(t.Name())
	dir := filepath.Join("tmp", "panel-init", name)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove temp dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func runPanelInitHelper() {
	if recordPath := os.Getenv("PANEL_INIT_HELPER_RECORD"); recordPath != "" {
		_ = os.WriteFile(recordPath, []byte(fmt.Sprintf("mode=%s\nrestartURL=%s\ntoken=%s\n", *helperMaintenanceMode, *helperInitRestartURL, *helperInitRestartToken)), 0o644)
	}
	if os.Getenv("PANEL_INIT_HELPER_WAIT") != "1" {
		os.Exit(0)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	<-signals
	os.Exit(0)
}
