package remoteops

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/ssh"
)

type LogSink interface {
	AppendLog(ctx context.Context, stream, line string) error
}

type Runner struct {
	Exec   sshx.RemoteExecutor
	Target sshx.Target
	Log    LogSink
}

type UFWRule struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	From     string `json:"from"`
}

type UFWStatus struct {
	Installed bool
	Active    bool
	Status    string
	Default   string
	Rules     []UFWRuleStatus
	Raw       string
}

type UFWRuleStatus struct {
	Number int
	To     string
	Action string
	From   string
}

var (
	packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`)
	fileModePattern    = regexp.MustCompile(`^[0-7]{3,4}$`)
	ufwRulePattern     = regexp.MustCompile(`^\[\s*([0-9]+)\]\s+(.+?)\s{2,}([A-Z]+(?:\s+(?:IN|OUT))?)\s{2,}(.+)$`)
)

func (r Runner) RunSudoLogged(ctx context.Context, command string, timeout time.Duration) (sshx.CommandResult, error) {
	if r.Exec == nil {
		return sshx.CommandResult{}, panelerr.Validation("remote_executor_unavailable", "Remote executor is unavailable")
	}
	var stdoutStreamed atomic.Bool
	var stderrStreamed atomic.Bool
	res, err := r.Exec.ExecSudo(ctx, r.Target, sshx.CommandSpec{
		Command: command,
		Timeout: timeout,
		OnStdout: func(line string) {
			stdoutStreamed.Store(true)
			AppendBufferedLog(ctx, r.Log, "stdout", line)
		},
		OnStderr: func(line string) {
			stderrStreamed.Store(true)
			AppendBufferedLog(ctx, r.Log, "stderr", line)
		},
	})
	if !stdoutStreamed.Load() {
		AppendBufferedLog(ctx, r.Log, "stdout", res.Stdout)
	}
	if !stderrStreamed.Load() {
		AppendBufferedLog(ctx, r.Log, "stderr", res.Stderr)
	}
	return res, err
}

func (r Runner) WriteFileSudo(ctx context.Context, remotePath string, content []byte, mode string, timeout time.Duration) error {
	script, err := WriteFileSudoScript(remotePath, content, mode)
	if err != nil {
		return err
	}
	_, err = r.RunSudoLogged(ctx, script, timeout)
	return err
}

func AppendBufferedLog(ctx context.Context, log LogSink, stream, out string) {
	if log == nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			_ = log.AppendLog(ctx, stream, line)
		}
	}
}

func APTInstallScript(packages []string) (string, error) {
	commands, err := APTInstallCommands(packages)
	if err != nil {
		return "", err
	}
	return aptScriptPrelude() + commands, nil
}

func MustAPTInstallScript(packages ...string) string {
	script, err := APTInstallScript(packages)
	if err != nil {
		panic(err)
	}
	return script
}

func APTInstallCommands(packages []string) (string, error) {
	if len(packages) == 0 {
		return "", panelerr.Validation("packages_required", "At least one package is required")
	}
	parts := make([]string, 0, len(packages))
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if !packageNamePattern.MatchString(pkg) {
			return "", panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
		}
		parts = append(parts, pkg)
	}
	return "apt_get update\napt_get install -y " + strings.Join(parts, " ") + "\n", nil
}

func MustAPTInstallCommands(packages ...string) string {
	commands, err := APTInstallCommands(packages)
	if err != nil {
		panic(err)
	}
	return commands
}

func WriteFileSudoScript(remotePath string, content []byte, mode string) (string, error) {
	remotePath = strings.TrimSpace(remotePath)
	mode = strings.TrimSpace(mode)
	if !strings.HasPrefix(remotePath, "/") {
		return "", panelerr.Validation("remote_path_invalid", "Remote path must be absolute")
	}
	if mode == "" {
		mode = "0644"
	}
	if !fileModePattern.MatchString(mode) {
		return "", panelerr.Validation("remote_file_mode_invalid", "Remote file mode is invalid")
	}
	dir := path.Dir(remotePath)
	encoded := base64.StdEncoding.EncodeToString(content)
	return fmt.Sprintf(`set -eu
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
install -d -m 0755 %s
base64 -d > "$tmp" <<'EOF'
%s
EOF
install -m %s "$tmp" %s`, ShellQuote(dir), encoded, mode, ShellQuote(remotePath)), nil
}

func UFWStatusScript() string {
	return `if ! command -v ufw >/dev/null 2>&1; then
  echo "panel_ufw_installed=false"
  exit 0
fi
echo "panel_ufw_installed=true"
ufw status verbose || true
echo "panel_ufw_numbered_begin"
ufw status numbered || true`
}

func RestartScript() string {
	return `set -eu
echo "[panel] scheduling server restart"
if command -v systemctl >/dev/null 2>&1; then
  nohup sh -c 'sleep 1; systemctl reboot' >/dev/null 2>&1 &
elif command -v shutdown >/dev/null 2>&1; then
  nohup sh -c 'sleep 1; shutdown -r now' >/dev/null 2>&1 &
else
  echo "[panel] no supported restart command found" >&2
  exit 1
fi`
}

func UFWAllowScript(rules []UFWRule) (string, error) {
	if len(rules) == 0 {
		return "", panelerr.Validation("ufw_rule_required", "At least one UFW rule is required")
	}
	commands := []string{
		`if command -v ufw >/dev/null 2>&1; then`,
		`  echo "[panel] allowing UFW rules"`,
	}
	for _, rule := range rules {
		command, err := ufwAllowCommand(rule)
		if err != nil {
			return "", err
		}
		commands = append(commands, "  "+command)
	}
	commands = append(commands,
		`else`,
		`  echo "[panel] UFW is not installed; skipping local UFW rules"`,
		`fi`,
	)
	return strings.Join(commands, "\n"), nil
}

func UFWEnableScript(sshPort int) (string, error) {
	allowSSH, err := ufwAllowCommand(UFWRule{Port: sshPort, Protocol: "tcp"})
	if err != nil {
		return "", err
	}
	return strings.Join([]string{
		"set -eu",
		`if ! command -v ufw >/dev/null 2>&1; then`,
		`  echo "[panel] UFW is not installed" >&2`,
		`  exit 1`,
		`fi`,
		`echo "[panel] ensuring SSH access before enabling UFW"`,
		allowSSH,
		`echo "[panel] enabling UFW"`,
		`ufw --force enable`,
	}, "\n"), nil
}

func MustUFWAllowScript(rules ...UFWRule) string {
	script, err := UFWAllowScript(rules)
	if err != nil {
		panic(err)
	}
	return script
}

func UFWDeleteRuleScript(number int) (string, error) {
	if number <= 0 {
		return "", panelerr.Validation("ufw_rule_number_invalid", "UFW rule number must be positive")
	}
	return "set -eu\nufw --force delete " + strconv.Itoa(number), nil
}

func ParseUFWStatus(out string) UFWStatus {
	status := UFWStatus{Raw: out, Status: "not_installed"}
	inNumbered := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if line == "panel_ufw_installed=true" {
			status.Installed = true
			status.Status = "inactive"
			continue
		}
		if line == "panel_ufw_installed=false" {
			status.Installed = false
			status.Active = false
			status.Status = "not_installed"
			continue
		}
		if line == "panel_ufw_numbered_begin" {
			inNumbered = true
			continue
		}
		if strings.HasPrefix(line, "Status:") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Status:")))
			if value != "" {
				status.Status = value
				status.Active = value == "active"
			}
			continue
		}
		if strings.HasPrefix(line, "Default:") {
			status.Default = strings.TrimSpace(strings.TrimPrefix(line, "Default:"))
			continue
		}
		if !inNumbered {
			continue
		}
		if rule, ok := parseUFWRuleLine(line); ok {
			status.Rules = append(status.Rules, rule)
		}
	}
	return status
}

func ufwAllowCommand(rule UFWRule) (string, error) {
	if rule.Port <= 0 || rule.Port > 65535 {
		return "", panelerr.Validation("ufw_port_invalid", "UFW port must be between 1 and 65535")
	}
	protocol := strings.ToLower(strings.TrimSpace(rule.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "udp" && protocol != "any" {
		return "", panelerr.Validation("ufw_protocol_invalid", "UFW protocol must be tcp, udp, or any")
	}
	from := strings.TrimSpace(rule.From)
	if from == "" || strings.EqualFold(from, "anywhere") {
		if protocol == "any" {
			return "ufw allow " + strconv.Itoa(rule.Port), nil
		}
		return "ufw allow " + strconv.Itoa(rule.Port) + "/" + protocol, nil
	}
	command := "ufw allow from " + ShellQuote(from) + " to any port " + strconv.Itoa(rule.Port)
	if protocol != "any" {
		command += " proto " + protocol
	}
	return command, nil
}

func parseUFWRuleLine(line string) (UFWRuleStatus, bool) {
	match := ufwRulePattern.FindStringSubmatch(line)
	if len(match) != 5 {
		return UFWRuleStatus{}, false
	}
	number, _ := strconv.Atoi(match[1])
	return UFWRuleStatus{
		Number: number,
		To:     strings.TrimSpace(match[2]),
		Action: strings.TrimSpace(match[3]),
		From:   strings.TrimSpace(match[4]),
	}, true
}

func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func aptScriptPrelude() string {
	return `set -eu
export DEBIAN_FRONTEND=noninteractive
panel_timeout() {
  seconds="$1"
  shift
  if command -v timeout >/dev/null 2>&1; then
    timeout "$seconds" "$@"
  else
    "$@"
  fi
}
apt_get() {
  panel_timeout 900 apt-get -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold "$@"
}
`
}
