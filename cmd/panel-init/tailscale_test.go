package main

import (
	"context"
	"errors"
	"fmt"
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

// fakeTailscaleScript 模拟 tailscale CLI：未登录时报告 NeedsLogin；up 只接受
// --auth-key=file:<path>（与真实 CLI 一致，不读取 TS_AUTHKEY 环境变量），读取
// 文件内容作为密钥并写入登录标记，同时把参数、密钥文件路径与权限记录到
// FAKE_TS_RECORD，供测试断言密钥没有进入参数列表且文件权限为 0600。
const fakeTailscaleScript = `#!/bin/sh
command=""
for arg in "$@"; do
  case "$arg" in
    status|up|logout) command="$arg" ;;
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
    if [ -n "$FAKE_TS_UP_SLEEP" ]; then
      sleep "$FAKE_TS_UP_SLEEP" >/dev/null 2>&1 </dev/null
    fi
    keypath=""
    for arg in "$@"; do
      case "$arg" in
        --auth-key=file:*) keypath="${arg#--auth-key=file:}" ;;
      esac
    done
    if [ -z "$keypath" ] || [ ! -f "$keypath" ]; then
      echo "no auth key file" >&2
      exit 1
    fi
    key=$(cat "$keypath")
    case "$*" in
      *"$key"*) echo "auth key leaked into arguments" >&2; exit 1 ;;
    esac
    if [ -n "$FAKE_TS_RECORD" ]; then
      {
        echo "args=$*"
        echo "keyfile=$keypath"
        echo "keymode=$(ls -ld "$keypath" | cut -c1-10)"
        echo "keylength=${#key}"
      } >> "$FAKE_TS_RECORD"
    fi
    : > "$FAKE_TS_MARKER"
    echo "logged in"
    ;;
  logout)
    if [ -n "$FAKE_TS_RECORD" ]; then
      echo "logout=1" >> "$FAKE_TS_RECORD"
    fi
    rm -f "$FAKE_TS_MARKER"
    echo "logged out"
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
			t.Fatalf("expected the login to complete via the auth key file: %v", err)
		}
	}
	supervisor.Stop()
	if status := supervisor.Status(); status.Running {
		t.Fatalf("stopping the supervisor must stop tailscaled: %#v", status)
	}
}

// TS-INIT-006：认证密钥必须经仅 root 可读的 0600 临时文件与 --auth-key=file:
// 引用传递给 tailscale up，不进入命令行参数；命令结束后文件必须被删除。
func TestTailscaleSupervisorDeliversAuthKeyThroughPrivateFile(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	recordPath := filepath.Join(root, "up-record")
	t.Setenv("FAKE_TS_RECORD", recordPath)
	const authKey = "tskey-auth-test-key"
	if err := tailscale.WriteConfig(root, tailscale.Config{Enabled: true, AuthKey: authKey}); err != nil {
		t.Fatal(err)
	}

	supervisor := newTailscaleSupervisor(root, binaries.ts, binaries.daemon, log.New(os.Stderr, "", 0))
	supervisor.reconcile(context.Background())
	if status := supervisor.Status(); !status.Running || !status.LoggedIn {
		t.Fatalf("status = %#v", status)
	}
	supervisor.Stop()

	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read up record: %v", err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(record)), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			fields[name] = value
		}
	}
	if !strings.Contains(fields["args"], "--auth-key=file:") {
		t.Fatalf("up args = %q, want an --auth-key=file: reference", fields["args"])
	}
	if !strings.Contains(fields["args"], "--accept-dns=false") || !strings.Contains(fields["args"], "--accept-routes=false") {
		t.Fatalf("up args = %q, want --accept-dns=false --accept-routes=false", fields["args"])
	}
	if strings.Contains(fields["args"], authKey) {
		t.Fatalf("auth key leaked into up args: %q", fields["args"])
	}
	if fields["keymode"] != "-rw-------" {
		t.Fatalf("auth key file mode = %q, want -rw-------", fields["keymode"])
	}
	if fields["keylength"] != fmt.Sprint(len(authKey)) {
		t.Fatalf("auth key length = %q, want %d", fields["keylength"], len(authKey))
	}
	if path := fields["keyfile"]; path == "" {
		t.Fatal("up record is missing the auth key file path")
	} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth key file %q must be removed after up (stat err = %v)", path, err)
	}
}

// TS-INIT-006：登录必须有界；tailscale up 挂起（例如等待交互式认证）时不得
// 无限阻塞收敛循环，必须按超时失败并写入 lastError。
func TestTailscaleSupervisorLoginTimesOut(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	t.Setenv("FAKE_TS_UP_SLEEP", "5")
	if err := tailscale.WriteConfig(root, tailscale.Config{Enabled: true, AuthKey: "tskey-auth-test"}); err != nil {
		t.Fatal(err)
	}
	originalTimeout := tailscaleUpTimeout
	tailscaleUpTimeout = 200 * time.Millisecond
	t.Cleanup(func() { tailscaleUpTimeout = originalTimeout })

	supervisor := newTailscaleSupervisor(root, binaries.ts, binaries.daemon, log.New(os.Stderr, "", 0))
	start := time.Now()
	supervisor.reconcile(context.Background())
	elapsed := time.Since(start)
	status := supervisor.Status()
	supervisor.Stop()
	if !strings.Contains(status.LastError, "timed out") {
		t.Fatalf("last error = %q, want a timeout error", status.LastError)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("reconcile took %s, want it bounded by the up timeout", elapsed)
	}
}

func TestTailscaleSupervisorStopsProcessWhenDisabled(t *testing.T) {
	skipWithoutPOSIXShell(t)
	root, binaries := newFakeTailscaleEnvironment(t)
	recordPath := filepath.Join(root, "disable-record")
	t.Setenv("FAKE_TS_RECORD", recordPath)
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
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "logout=1") {
		t.Fatalf("disable must logout from the tailnet before stopping: %q", record)
	}
	if marker := os.Getenv("FAKE_TS_MARKER"); marker != "" {
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("logout must clear the login marker (stat err = %v)", err)
		}
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
