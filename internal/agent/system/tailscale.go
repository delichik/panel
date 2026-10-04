package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/tailscale"
)

const (
	// tailscaleInstallTimeout 覆盖添加软件源并安装 tailscale 的完整过程。
	tailscaleInstallTimeout = 20 * time.Minute
	// tailscaleCommandTimeout 限制单条 tailscale 子命令的执行时间。
	tailscaleCommandTimeout = 3 * time.Minute
	// tailscaleLoginWait 是首次加入 tailnet 后等待后端进入 Running 的上限。
	tailscaleLoginWait = 45 * time.Second
	// tailscaleServiceName 是软件包提供的 systemd 单元名。
	tailscaleServiceName = "tailscaled"
)

var (
	tailscaleHostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	tailscaleCodenamePattern = regexp.MustCompile(`^[a-z]+$`)
)

// TailscaleStatus 读取节点本机 tailscale 事实。未安装时返回
// Installed=false 而不是错误，使 Panel 能区分“未安装”与“查询失败”。
func (LocalCollector) TailscaleStatus(ctx context.Context) (agentcontract.TailscaleStatus, error) {
	return readTailscaleStatus(ctx)
}

// TailscaleConfigure 保证节点已安装 tailscale 并加入 tailnet。
//
// 认证密钥只在节点尚未登录时使用，并通过 TS_AUTHKEY 环境变量传递：它既不会
// 出现在命令行参数（可能被本机其他用户通过进程列表读到），也不会写入磁盘。
func (LocalCollector) TailscaleConfigure(ctx context.Context, req agentcontract.TailscaleConfigureRequest) (agentcontract.TailscaleStatus, error) {
	tags, err := tailscale.NormalizeTags(req.Tags)
	if err != nil {
		return agentcontract.TailscaleStatus{}, err
	}
	hostname := strings.TrimSpace(req.Hostname)
	if hostname != "" && !tailscaleHostnamePattern.MatchString(hostname) {
		return agentcontract.TailscaleStatus{}, errors.New("tailscale hostname is invalid")
	}
	if _, err := exec.LookPath("tailscale"); err != nil {
		if err := installTailscale(ctx); err != nil {
			return agentcontract.TailscaleStatus{}, err
		}
	}
	if err := ensureTailscaledRunning(ctx); err != nil {
		return agentcontract.TailscaleStatus{}, err
	}
	status, err := readTailscaleStatus(ctx)
	if err != nil {
		return agentcontract.TailscaleStatus{}, err
	}
	if status.LoggedIn {
		// 已加入 tailnet 的节点不重复执行 up：那会重写节点偏好并可能打断
		// 现有连接，而地址与主机名无需变更。
		return status, nil
	}
	if strings.TrimSpace(req.AuthKey) == "" && status.BackendState == tailscale.BackendStateNeedsLogin {
		return status, errors.New("tailscale auth key is required to join the tailnet")
	}
	if err := tailscaleUp(ctx, req.AuthKey, tags, hostname); err != nil {
		return agentcontract.TailscaleStatus{}, err
	}
	return waitForTailscaleLogin(ctx)
}

// TailscaleDisable 让节点退出 tailnet 连接，但保留已安装的软件包与节点身份，
// 使再次启用无需重新申请节点密钥（禁用与删除是两种不同的语义）。
func (LocalCollector) TailscaleDisable(ctx context.Context) (agentcontract.TailscaleStatus, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return agentcontract.TailscaleStatus{Installed: false}, nil
	}
	if _, err := runCommand(ctx, tailscaleCommandTimeout, "tailscale", "down"); err != nil {
		return agentcontract.TailscaleStatus{}, err
	}
	return readTailscaleStatus(ctx)
}

// readTailscaleStatus 汇总节点本机事实。任何一步失败都写进 LastError，
// 而不是让调用方只能看到“未知状态”。
func readTailscaleStatus(ctx context.Context) (agentcontract.TailscaleStatus, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return agentcontract.TailscaleStatus{Installed: false}, nil
	}
	status := agentcontract.TailscaleStatus{Installed: true, Running: tailscaledActive(ctx)}
	output, err := runCommand(ctx, tailscaleCommandTimeout, "tailscale", "status", "--json")
	if err != nil {
		status.LastError = tailscaleDiagnostic(err)
		return status, nil
	}
	state, err := tailscale.ParseStatusJSON([]byte(output))
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	status.Running = true
	status.LoggedIn = state.LoggedIn()
	status.Hostname = state.Hostname
	status.IPv4 = state.IPv4
	status.IPv6 = state.IPv6
	status.Version = state.Version
	status.BackendState = state.BackendState
	switch state.BackendState {
	case tailscale.BackendStateNeedsMachineAuth:
		status.LastError = "tailscale node is waiting for admin approval in the tailnet"
	case tailscale.BackendStateNeedsLogin:
		status.LastError = "tailscale node needs to log in to the tailnet"
	}
	return status, nil
}

func tailscaledActive(ctx context.Context) bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	output, err := runCommand(ctx, 30*time.Second, "systemctl", "is-active", tailscaleServiceName)
	return err == nil && strings.TrimSpace(output) == "active"
}

func ensureTailscaledRunning(ctx context.Context) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemd is required to manage tailscaled on this node")
	}
	if _, err := runCommand(ctx, tailscaleCommandTimeout, "systemctl", "enable", "--now", tailscaleServiceName); err != nil {
		return fmt.Errorf("start tailscaled: %w", err)
	}
	return nil
}

// tailscaleUp 执行首次加入或重新加入。密钥只经环境变量传递，并在错误文本中
// 被脱敏，保证任务日志不包含秘密。
func tailscaleUp(ctx context.Context, authKey string, tags []string, hostname string) error {
	args := []string{"up"}
	if len(tags) > 0 {
		args = append(args, "--advertise-tags="+strings.Join(tags, ","))
	}
	if hostname != "" {
		args = append(args, "--hostname="+hostname)
	}
	env := []string{}
	if key := strings.TrimSpace(authKey); key != "" {
		env = append(env, "TS_AUTHKEY="+key)
	}
	output, err := runCommandWithEnv(ctx, tailscaleCommandTimeout, env, "tailscale", args...)
	if err != nil {
		return fmt.Errorf("tailscale up failed: %s", redactSecret(strings.TrimSpace(output), authKey))
	}
	return nil
}

// waitForTailscaleLogin 轮询到后端进入 Running 或明确需要管理员批准为止，
// 使 Panel 在一次任务内就能拿到节点地址，而不用额外轮询周期。
func waitForTailscaleLogin(ctx context.Context) (agentcontract.TailscaleStatus, error) {
	deadline := time.Now().Add(tailscaleLoginWait)
	var last agentcontract.TailscaleStatus
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return last, ctx.Err()
		}
		status, err := readTailscaleStatus(ctx)
		if err != nil {
			return agentcontract.TailscaleStatus{}, err
		}
		last = status
		if status.LoggedIn {
			return status, nil
		}
		if status.BackendState == tailscale.BackendStateNeedsMachineAuth {
			return status, errors.New(status.LastError)
		}
		time.Sleep(2 * time.Second)
	}
	if last.LastError != "" {
		return last, errors.New(last.LastError)
	}
	return last, errors.New("tailscale did not reach the running state in time")
}

// installTailscale 安装 tailscale 软件包。优先使用发行版仓库（用户可能已
// 自行添加软件源），失败时按官方文档添加 vendor 软件源后重试。
//
// 安装过程使用脱离取消的上下文：apt/dpkg 事务被中途打断会留下不一致的包状态，
// 因此与软件包升级遵循同一约定。
func installTailscale(ctx context.Context) error {
	installCtx := context.WithoutCancel(ctx)
	if _, err := runCommand(installCtx, tailscaleInstallTimeout, "apt-get", "install", "-y", "--no-install-recommends", "tailscale"); err == nil {
		return nil
	}
	if err := addTailscaleVendorRepo(installCtx); err != nil {
		return err
	}
	if _, err := runCommand(installCtx, tailscaleInstallTimeout, "apt-get", "update"); err != nil {
		return fmt.Errorf("refresh apt metadata for tailscale: %w", err)
	}
	if _, err := runCommand(installCtx, tailscaleInstallTimeout, "apt-get", "install", "-y", "--no-install-recommends", "tailscale"); err != nil {
		return fmt.Errorf("install tailscale: %w", err)
	}
	return nil
}

// addTailscaleVendorRepo 按 https://tailscale.com/download/linux 的手工步骤写入
// 签名密钥与软件源列表。这里不执行厂商的 install.sh：远端命令只允许结构化参数，
// 不允许把网络脚本直接交给 shell 执行。
func addTailscaleVendorRepo(ctx context.Context) error {
	distro, codename := readOSReleaseIdentity()
	switch distro {
	case "debian", "ubuntu":
	default:
		return fmt.Errorf("tailscale installation is not supported on %q", distro)
	}
	if !tailscaleCodenamePattern.MatchString(codename) {
		return errors.New("cannot determine the distribution codename for the tailscale repository")
	}
	base := "https://pkgs.tailscale.com/stable/" + distro + "/" + codename
	if err := downloadFile(ctx, base+".noarmor.gpg", "/usr/share/keyrings/tailscale-archive-keyring.gpg"); err != nil {
		return err
	}
	if err := downloadFile(ctx, base+".tailscale-keyring.list", "/etc/apt/sources.list.d/tailscale.list"); err != nil {
		return err
	}
	return nil
}

// downloadFile 使用 curl 或 wget 下载固定 URL 到固定路径。
func downloadFile(ctx context.Context, url, target string) error {
	dir := filepath.Dir(target)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if _, err := exec.LookPath("curl"); err == nil {
		if _, err := runCommand(ctx, tailscaleCommandTimeout, "curl", "-fsSL", "-o", target, url); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath("wget"); err == nil {
		if _, err := runCommand(ctx, tailscaleCommandTimeout, "wget", "-qO", target, url); err == nil {
			return nil
		}
	}
	return fmt.Errorf("download %s failed: neither curl nor wget succeeded", url)
}

// readOSReleaseIdentity 读取发行版 ID 与 VERSION_CODENAME。
func readOSReleaseIdentity() (string, string) {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	var distro, codename string
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "ID":
			distro = strings.ToLower(value)
		case "VERSION_CODENAME":
			codename = strings.ToLower(value)
		}
	}
	return distro, codename
}

// runCommandWithEnv 在附加环境变量的前提下执行命令并返回合并输出。
func runCommandWithEnv(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tailscaleDiagnostic 把命令错误裁剪成适合写入观测态的稳定文本。
func tailscaleDiagnostic(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return "tailscale status is unavailable: " + err.Error()
}

// redactSecret 从命令输出中移除认证密钥，保证 GOV-DOD-009 的秘密不外泄要求。
func redactSecret(text, secret string) string {
	if secret = strings.TrimSpace(secret); secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}
