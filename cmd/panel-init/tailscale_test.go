package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"panel/internal/platform/tailscale"
)

// fakeTailscaledScript 是一个只负责创建控制 socket 并等待终止信号的守护进程
// 替身：supervisor 的就绪判定以 socket 路径出现为准，真实可用性由状态读取判定。
const fakeTailscaledScript = `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    --socket=*) socket="${arg#--socket=}" ;;
  esac
done
[ -n "$socket" ] || exit 1
: > "$socket"
trap 'exit 0' INT TERM
while true; do sleep 0.2; done
`

// fakeTailscaleScript 模拟 tailscale CLI：未登录时报告 NeedsLogin，只有带上
// TS_AUTHKEY 的 up 才会写入登录标记，从而让测试能验证密钥经环境变量传递。
const fakeTailscaleScript = `#!/bin/sh
command=""
for arg in "$@"; do
  case "$arg" in
    status|up) command="$arg" ;;
  esac
done
case "$command" in
  status)
    if [ -f "$FAKE_TS_MARKER" ]; then
      echo '{"BackendState":"Running","Version":"1.80.0","Self":{"HostName":"seamark-panel","TailscaleIPs":["100.64.0.9","fd7a:115c:a1e0::9","192.168.1.5"]}}'
    else
      echo '{"BackendState":"NeedsLogin","Version":"1.80.0"}'
    fi
    ;;
  up)
    if [ -z "$TS_AUTHKEY" ]; then
      echo "no auth key" >&2
      exit 1
    fi
    : > "$FAKE_TS_MARKER"
    echo "logged in"
    ;;
  *)
    echo "unsupported command" >&2
    exit 1
    ;;
esac
exit 0
`

func writeFakeBinary(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTailscaleSupervisorReportsUnavailableWithoutBinaries(t *testing.T) {
	root := t.TempDir()
	if err := tailscale.WriteConfig(root, tailscale.Config{Enabled: true, AuthKey: "tskey-auth-test"}); err != nil {
		t.Fatal(err)
	}
	supervisor := newTailscaleSupervisor(root, "", "", log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	status := supervisor.Status()
	if status.Available {
		t.Fatal("missing tailscale binaries must report the feature as unavailable")
	}
	if !strings.Contains(status.LastError, "not installed") {
		t.Fatalf("last error = %q", status.LastError)
	}
}

func TestTailscaleSupervisorStaysIdleWithoutConfig(t *testing.T) {
	root := t.TempDir()
	supervisor := newTailscaleSupervisor(root, "/nonexistent/tailscale", "/nonexistent/tailscaled", log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	status := supervisor.Status()
	if !status.Available || status.Running || status.LastError != "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTailscaleSupervisorStaysIdleWithoutAuthKey(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	// 没有认证密钥就无法首次加入 tailnet。期望态必须被规范化为“未启用”，
	// supervisor 不得擅自启动 tailscaled 或尝试登录。
	if err := tailscale.WriteConfig(root, tailscale.Config{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	supervisor := newTailscaleSupervisor(root, binaries.ts, binaries.daemon, log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	status := supervisor.Status()
	if status.Running || status.LoggedIn {
		t.Fatalf("supervisor must stay idle without an auth key: %#v", status)
	}
	if status.LastError != "" {
		t.Fatalf("missing auth key is not an error state: %q", status.LastError)
	}
	if _, err := os.Stat(tailscale.SocketPath(root)); err == nil {
		t.Fatal("tailscaled must not be started without an auth key")
	}
}

func TestTailscaleSupervisorStartsDaemonAndReportsTailnetAddresses(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	if err := tailscale.WriteConfig(root, tailscale.Config{
		Enabled: true,
		AuthKey: "tskey-auth-test",
		Tags:    []string{"tag:server"},
	}); err != nil {
		t.Fatal(err)
	}

	supervisor := newTailscaleSupervisor(root, binaries.ts, binaries.daemon, log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	status := supervisor.Status()
	if !status.Available || !status.Running || !status.LoggedIn {
		t.Fatalf("status = %#v", status)
	}
	if status.IPv4 != "100.64.0.9" || status.IPv6 != "fd7a:115c:a1e0::9" {
		t.Fatalf("tailnet addresses = %q %q", status.IPv4, status.IPv6)
	}
	if status.Hostname != "seamark-panel" || status.BackendState != "Running" {
		t.Fatalf("status = %#v", status)
	}
	// 非 tailnet 网段的自身地址绝不能被当成互联地址。
	if strings.Contains(status.IPv4, "192.168") || strings.Contains(status.IPv6, "192.168") {
		t.Fatalf("non tailnet address leaked into status: %#v", status)
	}
	if marker := os.Getenv("FAKE_TS_MARKER"); marker != "" {
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("expected the auth key to be delivered through TS_AUTHKEY: %v", err)
		}
	}
	supervisor.Stop()
	if status := supervisor.Status(); status.Running {
		t.Fatalf("stopping the supervisor must stop tailscaled: %#v", status)
	}
}

func TestTailscaleSupervisorStopsProcessWhenDisabled(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	if err := tailscale.WriteConfig(root, tailscale.Config{Enabled: true, AuthKey: "tskey-auth-test"}); err != nil {
		t.Fatal(err)
	}
	supervisor := newTailscaleSupervisor(root, binaries.ts, binaries.daemon, log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	if !supervisor.Status().Running {
		t.Fatal("expected tailscaled to be running before disabling")
	}
	if err := tailscale.WriteConfig(root, tailscale.Config{}); err != nil {
		t.Fatal(err)
	}
	supervisor.reconcile(context.Background())
	status := supervisor.Status()
	if status.Running || status.LoggedIn {
		t.Fatalf("disabled config must stop tailscaled: %#v", status)
	}
	supervisor.Stop()
}

func TestTailscaleSupervisorReportsInvalidConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(tailscale.Dir(root), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tailscale.ConfigPath(root), []byte("{"), 0o640); err != nil {
		t.Fatal(err)
	}
	supervisor := newTailscaleSupervisor(root, "/nonexistent/tailscale", "/nonexistent/tailscaled", log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	if got := supervisor.Status().LastError; !strings.Contains(got, "invalid tailscale config") {
		t.Fatalf("last error = %q", got)
	}
}

type fakeTailscaleBinaries struct {
	ts     string
	daemon string
}

func newFakeTailscaleEnvironment(t *testing.T) (string, fakeTailscaleBinaries) {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "logged-in")
	t.Setenv("FAKE_TS_MARKER", marker)
	return root, fakeTailscaleBinaries{
		ts:     writeFakeBinary(t, binDir, "tailscale", fakeTailscaleScript),
		daemon: writeFakeBinary(t, binDir, "tailscaled", fakeTailscaledScript),
	}
}

func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tailscaled substitute requires a POSIX shell")
	}
}

// 保证测试不会因为 supervisor 的收敛循环残留 goroutine 而阻塞退出。
func TestTailscaleSupervisorStopIsIdempotent(t *testing.T) {
	root := t.TempDir()
	supervisor := newTailscaleSupervisor(root, "", "", log.New(os.Stderr, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	supervisor.Start(ctx)
	cancel()
	supervisor.Stop()
	supervisor.Stop()
	if supervisor.Status().Running {
		t.Fatal("stopped supervisor must not report a running tailscaled")
	}
	if supervisor.Status().Available {
		t.Fatal("supervisor without binaries must report the feature as unavailable")
	}
	time.Sleep(10 * time.Millisecond)
}
