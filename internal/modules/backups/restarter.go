package backups

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

var errRestartRejected = errors.New("panel_init restart request rejected")

const (
	InitRestartURLEnv      = "PANEL_INIT_RESTART_URL"
	InitRestartTokenEnv    = "PANEL_INIT_RESTART_TOKEN"
	InitRestartTokenHeader = "X-Panel-Init-Token"
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
	delay  time.Duration
	url    string
	token  string
	client *http.Client
}

type restartRequest struct {
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"createdAt"`
}

func NewPanelInitRestarter(dataRoot string) Restarter {
	_ = dataRoot
	return initRestarter{
		delay: 800 * time.Millisecond,
		url:   os.Getenv(InitRestartURLEnv),
		token: os.Getenv(InitRestartTokenEnv),
		client: &http.Client{
			Timeout: 2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (r initRestarter) Supported() bool {
	return r.url != "" && r.token != "" && r.client != nil
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
	req, err := http.NewRequest(http.MethodPost, r.url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(InitRestartTokenHeader, r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errRestartRejected
	}
	return nil
}
