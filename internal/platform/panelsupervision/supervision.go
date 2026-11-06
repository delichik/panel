// Package panelsupervision 定义 panel-init 与 Panel 子进程之间的控制面合同。
//
// panel-init 在 <dataRoot>/run/ 下监听一个 Unix domain socket，同时提供维护重启
// 与容器内 tailscale 控制面；它用 Args 生成子进程命令行的全部监督参数，Panel 侧
// 用 Register 注册同一组 flag，并用 NewControlClient 通过 socket 访问控制面。
// 两侧必须始终成对，任何一侧漏接参数都会让 Panel 以
// "flag provided but not defined" 拒绝启动（TS-INIT-005）。
package panelsupervision

import (
	"context"
	"flag"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ControlSocketEnv 是 panel-init 传给 Panel 子进程的控制面 socket 路径。
	// 未设置表示当前进程不由 panel-init 监管（开发模式直接运行 panel）。
	ControlSocketEnv = "PANEL_INIT_CONTROL_SOCKET"
	// ControlTokenEnv 是 panel-init 生成的本次进程控制令牌。
	ControlTokenEnv = "PANEL_INIT_CONTROL_TOKEN"
	// ControlTokenHeader 是控制面鉴权头，重启与 tailscale 两条路径共用。
	ControlTokenHeader = "X-Panel-Init-Token"

	controlDirName        = "run"
	controlSocketFileName = "panel-control.sock"
)

// ControlSocketPath 返回 panel-init 控制面 socket 的固定路径。socket 位于持久
// 卷上，容器重启后沿用同一路径；旧的 socket 文件由 panel-init 启动时清理。
func ControlSocketPath(dataRoot string) string {
	return filepath.Join(strings.TrimSpace(dataRoot), controlDirName, controlSocketFileName)
}

// Config 保存 panel-init 随子进程命令行下发的全部监督参数。
type Config struct {
	// MaintenanceMode 是启动维护模式：normal、export 或 restore。
	MaintenanceMode string
	// ControlSocket 是 panel-init 控制面 socket 路径。
	ControlSocket string
	// ControlToken 是本次进程的控制面鉴权令牌。
	ControlToken string
}

// Register 在 fs 上注册 Panel 接收的全部监督 flag。
// cfg 的当前值作为默认值，使独立运行（不经 panel-init）时保持既有默认行为。
func Register(fs *flag.FlagSet, cfg *Config) {
	fs.StringVar(&cfg.MaintenanceMode, "maintenance-mode", cfg.MaintenanceMode, "startup maintenance mode")
	fs.StringVar(&cfg.ControlSocket, "init-control-socket", cfg.ControlSocket, "local panel_init control socket path")
	fs.StringVar(&cfg.ControlToken, "init-control-token", cfg.ControlToken, "local panel_init control token")
}

// Args 返回 panel-init 启动 Panel 子进程时使用的完整参数列表。
func Args(cfg Config) []string {
	return []string{
		"--maintenance-mode", cfg.MaintenanceMode,
		"--init-control-socket", cfg.ControlSocket,
		"--init-control-token", cfg.ControlToken,
	}
}

// ControlClient 是 Panel 侧访问 panel-init 控制面的客户端：所有请求通过
// Unix domain socket 发出，只对 socket 路径可达的进程开放。
type ControlClient struct {
	socket string
	token  string
	client *http.Client
}

// NewControlClient 依据 panel-init 下发的环境变量构造控制面客户端。socket 路径
// 或令牌缺失时返回 nil, false：调用方必须据此对外表达“不受 panel-init 监管”，
// 不得静默假装成功。
func NewControlClient(timeout time.Duration) (*ControlClient, bool) {
	socket := strings.TrimSpace(os.Getenv(ControlSocketEnv))
	token := strings.TrimSpace(os.Getenv(ControlTokenEnv))
	if socket == "" || token == "" {
		return nil, false
	}
	return &ControlClient{
		socket: socket,
		token:  token,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var dialer net.Dialer
					return dialer.DialContext(ctx, "unix", socket)
				},
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, true
}

// Do 向控制面发送带鉴权的请求；path 形如 "/restart"。请求的 Host 只用于构造
// URL，实际连接始终走控制面 socket。
func (c *ControlClient) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://panel-init"+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(ControlTokenHeader, c.token)
	return c.client.Do(req)
}
