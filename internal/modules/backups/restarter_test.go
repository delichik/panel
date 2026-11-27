package backups

import (
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/platform/panelsupervision"
)

// newRestartControlServer 在临时目录中启动只监听 Unix socket 的替身控制面，
// 返回 socket 路径。
func newRestartControlServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "panel-control.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socketPath
}

func TestInitRestarterUnsupportedWithoutControlSocket(t *testing.T) {
	t.Setenv(panelsupervision.ControlSocketEnv, "")
	t.Setenv(panelsupervision.ControlTokenEnv, "secret")
	r := NewPanelInitRestarter(t.TempDir())
	if r.Supported() {
		t.Fatal("expected restart to be unsupported without panel_init control socket")
	}
}

func TestInitRestarterUnsupportedWithoutControlToken(t *testing.T) {
	t.Setenv(panelsupervision.ControlSocketEnv, filepath.Join(t.TempDir(), "panel-control.sock"))
	t.Setenv(panelsupervision.ControlTokenEnv, "")
	r := NewPanelInitRestarter(t.TempDir())
	if r.Supported() {
		t.Fatal("expected restart to be unsupported without panel_init control token")
	}
}

func TestInitRestarterPostsRequestedMode(t *testing.T) {
	received := make(chan restartRequest, 1)
	socketPath := newRestartControlServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(panelsupervision.ControlTokenHeader); got != "secret-token" {
			t.Errorf("token header = %q, want secret-token", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req restartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		received <- req
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Setenv(panelsupervision.ControlSocketEnv, socketPath)
	t.Setenv(panelsupervision.ControlTokenEnv, "secret-token")
	r := NewPanelInitRestarter(t.TempDir())

	r.RestartSoon(MaintenanceModeRestore)

	select {
	case req := <-received:
		if req.Mode != MaintenanceModeRestore {
			t.Fatalf("mode = %q, want %q", req.Mode, MaintenanceModeRestore)
		}
	case <-time.After(time.Second):
		t.Fatal("expected restart request")
	}
}
