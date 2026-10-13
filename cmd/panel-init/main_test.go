package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net"
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
	"panel/internal/platform/panelsupervision"
)

var (
	controlTokenShapePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

	// helperSupervision 由替身进程按 panel-init 下发的命令行解析填充。flag 合同
	// 与真实 Panel 共用 panelsupervision（TS-INIT-005），替身不再手工镜像参数。
	helperSupervision panelsupervision.Config
)

func TestMain(m *testing.M) {
	if os.Getenv("PANEL_INIT_HELPER_PROCESS") == "1" {
		// The test helper runs before testing.Main parses flags, so parse the
		// supervisor-supplied arguments before reading the flag values.
		panelsupervision.Register(flag.CommandLine, &helperSupervision)
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

// newTestControlSocket 为控制面用例返回一个临时 socket 路径：真实部署的 socket
// 位于持久卷的固定路径上，测试必须各自使用独立路径避免互相干扰。
func newTestControlSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "panel-control.sock")
}

func TestPanelInitRestartListenerRequiresValidToken(t *testing.T) {
	socketPath := newTestControlSocket(t)
	restarts := make(chan string, 1)
	server, err := startControlListener(socketPath, restarts, "secret-token", nil)
	if err != nil {
		t.Fatalf("startControlListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, socketPath, "wrong-token", `{"mode":"restore"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
	}
	assertNoRestart(t, restarts)
}

func TestPanelInitRestartListenerRejectsMalformedRestartRequest(t *testing.T) {
	socketPath := newTestControlSocket(t)
	restarts := make(chan string, 1)
	server, err := startControlListener(socketPath, restarts, "secret-token", nil)
	if err != nil {
		t.Fatalf("startControlListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, socketPath, "secret-token", `{`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", status, http.StatusBadRequest)
	}
	assertNoRestart(t, restarts)
}

func TestPanelInitRestartListenerAcceptsAuthorizedRestartRequest(t *testing.T) {
	socketPath := newTestControlSocket(t)
	restarts := make(chan string, 1)
	server, err := startControlListener(socketPath, restarts, "secret-token", nil)
	if err != nil {
		t.Fatalf("startControlListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, socketPath, "secret-token", `{"mode":"restore"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
	}
	assertRestart(t, restarts, backups.MaintenanceModeRestore)
}

func TestPanelInitRestartListenerFallsBackToNormalForInvalidMode(t *testing.T) {
	socketPath := newTestControlSocket(t)
	restarts := make(chan string, 1)
	server, err := startControlListener(socketPath, restarts, "secret-token", nil)
	if err != nil {
		t.Fatalf("startControlListener() error = %v", err)
	}
	defer server.Close()

	status := postRestart(t, socketPath, "secret-token", `{"mode":"invalid"}`)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
	}
	assertRestart(t, restarts, backups.MaintenanceModeNormal)
}

// TS-INIT-005：控制面 socket 位于持久卷，容器异常退出会留下同路径的 socket
// 文件；启动必须先清理再绑定，否则控制面在容器重启后永久不可用。
func TestPanelInitControlListenerReplacesStaleSocket(t *testing.T) {
	socketPath := newTestControlSocket(t)
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale socket file: %v", err)
	}
	restarts := make(chan string, 1)
	server, err := startControlListener(socketPath, restarts, "secret-token", nil)
	if err != nil {
		t.Fatalf("startControlListener() error = %v", err)
	}
	defer server.Close()

	if status := postRestart(t, socketPath, "secret-token", `{"mode":"restore"}`); status != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", status, http.StatusAccepted)
	}
	assertRestart(t, restarts, backups.MaintenanceModeRestore)
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(socketPath); err != nil || info.Mode()&os.ModeSocket == 0 {
			t.Fatalf("control socket was not recreated as a socket: %v", err)
		}
	}
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

func TestPanelInitControlTokenIsURLSafeRandomSecret(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 8; i++ {
		token, err := randomControlToken()
		if err != nil {
			t.Fatalf("randomControlToken() error = %v", err)
		}
		if !controlTokenShapePattern.MatchString(token) {
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
	signals := make(chan os.Signal, 1)
	result := make(chan struct {
		code int
		mode string
		err  error
	}, 1)

	controlSocket := filepath.Join(helperDir, "panel-control.sock")
	go func() {
		code, mode, err := runPanel(panelLaunch{
			path:   os.Args[0],
			socket: controlSocket,
			token:  "control-token",
			mode:   backups.MaintenanceModeNormal,
		}, restarts, signals)
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
		"socket=" + controlSocket,
		"token=control-token",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("helper record missing %q:\n%s", want, text)
		}
	}
}

func postRestart(t *testing.T, socketPath, token, body string) int {
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	req, err := http.NewRequest(http.MethodPost, "http://panel-init"+backups.InitRestartPath, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(panelsupervision.ControlTokenHeader, token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST via %s error = %v", socketPath, err)
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
		_ = os.WriteFile(recordPath, []byte(fmt.Sprintf("mode=%s\nsocket=%s\ntoken=%s\n", helperSupervision.MaintenanceMode, helperSupervision.ControlSocket, helperSupervision.ControlToken)), 0o644)
	}
	if os.Getenv("PANEL_INIT_HELPER_WAIT") != "1" {
		os.Exit(0)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	<-signals
	os.Exit(0)
}
