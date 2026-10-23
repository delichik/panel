package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTailscaleScript 模拟节点上的 tailscale CLI：up 只接受
// --auth-key=file:<path>（与真实 CLI 一致，不读取 TS_AUTHKEY 环境变量），读取
// 文件内容作为密钥，校验文件可读并记录参数与权限，供测试断言密钥没有进入参数
// 列表且文件权限为 0600。
const fakeTailscaleScript = `#!/bin/sh
command=""
for arg in "$@"; do
  case "$arg" in
    up|status|down) command="$arg" ;;
  esac
done
case "$command" in
  up)
    keypath=""
    for arg in "$@"; do
      case "$arg" in
        --auth-key=file:*) keypath="${arg#--auth-key=file:}" ;;
      esac
    done
    key=""
    if [ -n "$keypath" ] && [ -f "$keypath" ]; then
      key=$(cat "$keypath")
    fi
    if [ -n "$FAKE_TS_RECORD" ]; then
      {
        echo "args=$*"
        echo "keyfile=$keypath"
        if [ -n "$keypath" ] && [ -f "$keypath" ]; then
          echo "keymode=$(ls -ld "$keypath" | cut -c1-10)"
        else
          echo "keymode="
        fi
        echo "keylength=${#key}"
      } >> "$FAKE_TS_RECORD"
    fi
    if [ -z "$keypath" ] || [ ! -f "$keypath" ]; then
      echo "no auth key file" >&2
      exit 1
    fi
    if [ -n "$key" ]; then
      case "$*" in
        *"$key"*) echo "auth key leaked into arguments" >&2; exit 1 ;;
      esac
    fi
    echo "logged in"
    ;;
  *)
    echo "unsupported command" >&2
    exit 1
    ;;
esac
exit 0
`

func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tailscale substitute requires a POSIX shell")
	}
}

func installFakeTailscaleCLI(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func parseFakeRecord(t *testing.T, recordPath string) map[string]string {
	t.Helper()
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(record)), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			fields[name] = value
		}
	}
	return fields
}

// TS-INIT-006 / TS-AGT-003：节点侧加入必须同样经仅 root 可读的 0600 临时文件与
// --auth-key=file: 引用传递密钥（tailscale CLI 不读取 TS_AUTHKEY）；密钥不得进入
// 参数列表，命令结束后文件必须被删除。
func TestTailscaleUpDeliversAuthKeyThroughPrivateFile(t *testing.T) {
	skipWithoutPOSIXShell(t)
	installFakeTailscaleCLI(t, fakeTailscaleScript)
	recordPath := filepath.Join(t.TempDir(), "up-record")
	t.Setenv("FAKE_TS_RECORD", recordPath)

	const authKey = "tskey-auth-node-test"
	if err := tailscaleUp(context.Background(), authKey, []string{"tag:infra"}, "racknerd-4519e12"); err != nil {
		t.Fatalf("tailscaleUp() error = %v", err)
	}

	fields := parseFakeRecord(t, recordPath)
	if !strings.Contains(fields["args"], "--auth-key=file:") {
		t.Fatalf("up args = %q, want an --auth-key=file: reference", fields["args"])
	}
	if strings.Contains(fields["args"], authKey) {
		t.Fatalf("auth key leaked into up args: %q", fields["args"])
	}
	if !strings.Contains(fields["args"], "--advertise-tags=tag:infra") {
		t.Fatalf("up args = %q, want the ACL tag", fields["args"])
	}
	if !strings.Contains(fields["args"], "--accept-dns=false") || !strings.Contains(fields["args"], "--accept-routes=false") {
		t.Fatalf("up args = %q, want --accept-dns=false --accept-routes=false", fields["args"])
	}
	if fields["keymode"] != "-rw-------" {
		t.Fatalf("auth key file mode = %q, want -rw-------", fields["keymode"])
	}
	if fields["keylength"] != fmt.Sprint(len(authKey)) {
		t.Fatalf("auth key length = %q, want %d", fields["keylength"], len(authKey))
	}
	if path := fields["keyfile"]; path == "" {
		t.Fatal("record is missing the auth key file path")
	} else if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("auth key file %q must be removed after up (stat err = %v)", path, err)
	}
}

// TS-INIT-006：失败输出中的密钥必须被脱敏，不得出现在任务日志里。
func TestTailscaleUpRedactsAuthKeyInErrors(t *testing.T) {
	skipWithoutPOSIXShell(t)
	const authKey = "tskey-auth-node-test"
	installFakeTailscaleCLI(t, "#!/bin/sh\necho \"bad key "+authKey+"\" >&2\nexit 1\n")

	err := tailscaleUp(context.Background(), authKey, nil, "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), authKey) {
		t.Fatalf("auth key leaked into the error: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error = %v, want the key redacted", err)
	}
}

// TS-INIT-006：密钥为空时不得创建临时文件，也不得向 up 传递 --auth-key。
func TestTailscaleUpWithoutAuthKeyCreatesNoFile(t *testing.T) {
	skipWithoutPOSIXShell(t)
	installFakeTailscaleCLI(t, fakeTailscaleScript)
	recordPath := filepath.Join(t.TempDir(), "up-record")
	t.Setenv("FAKE_TS_RECORD", recordPath)

	// 没有密钥时替身 CLI 会拒绝 up；这里只验证参数里没有 --auth-key。
	_ = tailscaleUp(context.Background(), "  ", nil, "")

	fields := parseFakeRecord(t, recordPath)
	if strings.Contains(fields["args"], "--auth-key") {
		t.Fatalf("up args = %q, must not carry an --auth-key argument without a key", fields["args"])
	}
}

func TestWriteAuthKeyFileEmptyKey(t *testing.T) {
	path, err := writeAuthKeyFile("   ")
	if err != nil || path != "" {
		t.Fatalf("writeAuthKeyFile(blank) = %q, %v; want empty path and no error", path, err)
	}
}

// TS-AGT-004：关闭必须 logout 离开 tailnet，再 systemctl disable --now；不得只 down 保留身份。
func TestTailscaleDisableLogsOutAndStopsDaemon(t *testing.T) {
	skipWithoutPOSIXShell(t)
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "disable-record")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DISABLE_RECORD"
case "$1" in
  logout)
    exit 0
    ;;
  status)
    echo '{"BackendState":"NeedsLogin","Version":"1.80.0"}'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	systemctl := `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$FAKE_DISABLE_RECORD"
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(systemctl), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DISABLE_RECORD", recordPath)

	status, err := (LocalCollector{}).TailscaleDisable(context.Background())
	if err != nil {
		t.Fatalf("TailscaleDisable() error = %v", err)
	}
	if !status.Installed {
		t.Fatalf("status = %#v, want installed after disable", status)
	}
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(record)
	if !strings.Contains(text, "logout") {
		t.Fatalf("disable record = %q, want tailscale logout", text)
	}
	if strings.Contains(text, "\ndown\n") || strings.HasPrefix(text, "down\n") {
		t.Fatalf("disable must logout rather than only down: %q", text)
	}
	if !strings.Contains(text, "systemctl enable --now "+tailscaleServiceName) {
		t.Fatalf("disable record = %q, want systemctl enable --now before logout", text)
	}
	if !strings.Contains(text, "systemctl disable --now "+tailscaleServiceName) {
		t.Fatalf("disable record = %q, want systemctl disable --now %s", text, tailscaleServiceName)
	}
	if strings.Contains(text, "accept-dns") || strings.Contains(text, "accept-routes") || strings.Contains(text, " set ") {
		t.Fatalf("disable must not rewrite preferences: %q", text)
	}
}
