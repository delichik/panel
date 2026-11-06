package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"panel/internal/platform/panelsupervision"
)

var errRestartRejected = errors.New("panel_init restart request rejected")

const (
	// InitRestartPath 是 panel-init 控制面上维护重启入口的路径。
	InitRestartPath = "/restart"

	MaintenanceModeNormal  = "normal"
	MaintenanceModeExport  = "backup_export"
	MaintenanceModeRestore = "restore"
)

type Restarter interface {
	Supported() bool
	RestartSoon(mode string)
}

type noopRestarter struct{}

func (noopRestarter) Supported() bool         { return false }
func (noopRestarter) RestartSoon(mode string) {}

type initRestarter struct {
	delay   time.Duration
	control *panelsupervision.ControlClient
}

type restartRequest struct {
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"createdAt"`
}

// NewPanelInitRestarter 构造通过控制面 socket 联系 panel-init 的重启客户端。
// socket 路径与令牌来自 panel-init 下发的环境变量；任一缺失表示 Panel 未被监管，
// 重启入口按“不支持”处理，独立运行 panel 时不得进入维护逻辑。
func NewPanelInitRestarter(dataRoot string) Restarter {
	_ = dataRoot
	control, ok := panelsupervision.NewControlClient(2 * time.Second)
	if !ok {
		return noopRestarter{}
	}
	return initRestarter{delay: 800 * time.Millisecond, control: control}
}

func (r initRestarter) Supported() bool {
	return r.control != nil
}

func (r initRestarter) RestartSoon(mode string) {
	if !r.Supported() {
		return
	}
	delay := r.delay
	if delay <= 0 {
		delay = 800 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		_ = r.requestRestart(mode)
	}()
}

func (r initRestarter) requestRestart(mode string) error {
	if mode == "" {
		mode = MaintenanceModeNormal
	}
	raw, err := json.MarshalIndent(restartRequest{
		Mode:      mode,
		CreatedAt: time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	resp, err := r.control.Do(context.Background(), http.MethodPost, InitRestartPath, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errRestartRejected
	}
	return nil
}
