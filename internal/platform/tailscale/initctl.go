package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// InitURLEnv 是 panel-init 传给 Panel 子进程的 tailscale 控制面基地址。
	// 未设置表示当前进程不由 panel-init 监管（开发模式直接运行 panel）。
	InitURLEnv = "PANEL_INIT_TAILSCALE_URL"
	// InitTokenEnv 是 panel-init 生成的本次进程控制令牌。
	InitTokenEnv = "PANEL_INIT_RESTART_TOKEN"
	// InitTokenHeader 是控制面鉴权头，必须与既有的 panel-init 重启接口一致。
	InitTokenHeader = "X-Panel-Init-Token"

	// InitApplyPath 触发 panel-init 重新读取期望态配置并收敛 tailscaled。
	InitApplyPath = "/apply"
	// InitStatusPath 返回容器内 tailscale 的当前实际态。
	InitStatusPath = "/status"

	// InitRequestTimeout 限制单次控制面请求的等待时间。
	InitRequestTimeout = 5 * time.Second
)

// ErrInitUnsupported 表示当前进程不受 panel-init 监管，容器内 tailscale 无法管理。
var ErrInitUnsupported = errors.New("panel-init tailscale control is unavailable")

// panelReady 记录 Panel 自身是否已加入 tailnet。它由设置模块按容器内实际态
// 更新，供地址解析判断能否安全改用 tailnet 地址：Panel 不在 tailnet 中时
// 使用 tailnet 地址只会让节点失联。
var panelReady atomic.Bool

// SetPanelReady 更新 Panel 自身的 tailnet 可达性。
func SetPanelReady(ready bool) { panelReady.Store(ready) }

// PanelReady 报告 Panel 自身是否已加入 tailnet。默认 false：在首次确认之前
// 不得假设 tailnet 可达。
func PanelReady() bool { return panelReady.Load() }

// InitController 是 Panel 侧访问 panel-init 控制面的客户端。
type InitController interface {
	// Supported 报告容器内 tailscale 是否可被管理。
	Supported() bool
	// Apply 让 panel-init 重新读取期望态配置并收敛 tailscaled 进程。
	Apply(ctx context.Context) (Status, error)
	// Status 读取当前实际态。
	Status(ctx context.Context) (Status, error)
}

type initController struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewInitController 根据运行环境构造控制面客户端。环境变量缺失时返回一个
// Supported 为 false 的客户端，调用方必须据此对外表达“不可用”，不得静默假装成功。
func NewInitController() InitController {
	return &initController{
		baseURL: strings.TrimRight(strings.TrimSpace(os.Getenv(InitURLEnv)), "/"),
		token:   strings.TrimSpace(os.Getenv(InitTokenEnv)),
		client: &http.Client{
			Timeout: InitRequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *initController) Supported() bool {
	return c != nil && c.baseURL != "" && c.token != "" && c.client != nil
}

func (c *initController) Apply(ctx context.Context) (Status, error) {
	return c.request(ctx, http.MethodPost, InitApplyPath)
}

func (c *initController) Status(ctx context.Context) (Status, error) {
	return c.request(ctx, http.MethodGet, InitStatusPath)
}

func (c *initController) request(ctx context.Context, method, path string) (Status, error) {
	if !c.Supported() {
		return Status{}, ErrInitUnsupported
	}
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return Status{}, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(InitTokenHeader, c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Status{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Status{}, errors.New("panel-init tailscale request rejected with status " + resp.Status)
	}
	var status Status
	if len(raw) == 0 {
		return status, nil
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}
