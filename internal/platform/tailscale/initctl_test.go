package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"panel/internal/platform/panelsupervision"
)

// newControlSocketServer 在临时目录中启动一个只监听 Unix socket 的替身控制面。
func newControlSocketServer(t *testing.T, handler http.Handler) string {
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

func TestInitControllerUnsupportedWithoutEnvironment(t *testing.T) {
	t.Setenv(panelsupervision.ControlSocketEnv, "")
	t.Setenv(panelsupervision.ControlTokenEnv, "")
	controller := NewInitController()
	if controller.Supported() {
		t.Fatal("controller without environment must not report support")
	}
	if _, err := controller.Status(context.Background()); !errors.Is(err, ErrInitUnsupported) {
		t.Fatalf("err = %v, want %v", err, ErrInitUnsupported)
	}
	if _, err := controller.Apply(context.Background()); !errors.Is(err, ErrInitUnsupported) {
		t.Fatalf("err = %v, want %v", err, ErrInitUnsupported)
	}
}

func TestInitControllerAuthorizesAndDecodesStatus(t *testing.T) {
	const token = "init-token"
	var gotHeader string
	var gotMethod string
	socketPath := newControlSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(panelsupervision.ControlTokenHeader)
		gotMethod = r.Method
		if r.URL.Path != InitApplyPath {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(Status{Available: true, Running: true, IPv4: "100.64.0.9"})
	}))

	t.Setenv(panelsupervision.ControlSocketEnv, socketPath)
	t.Setenv(panelsupervision.ControlTokenEnv, token)
	controller := NewInitController()
	if !controller.Supported() {
		t.Fatal("controller must report support when the environment is complete")
	}
	status, err := controller.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != token {
		t.Fatalf("token header = %q, want %q", gotHeader, token)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if !status.Running || status.IPv4 != "100.64.0.9" {
		t.Fatalf("status = %#v", status)
	}
}

func TestInitControllerReportsRejectedRequests(t *testing.T) {
	socketPath := newControlSocketServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))

	t.Setenv(panelsupervision.ControlSocketEnv, socketPath)
	t.Setenv(panelsupervision.ControlTokenEnv, "token")
	if _, err := NewInitController().Status(context.Background()); err == nil {
		t.Fatal("expected an error for a rejected control request")
	}
}

func TestInitEnvironmentNamesStayStable(t *testing.T) {
	// 控制面 socket/令牌环境变量与鉴权头是 panel-init 与 Panel 进程之间的隐式
	// 契约，改名会让容器内 tailscale 静默变成“不可用”，因此固定断言。
	if panelsupervision.ControlSocketEnv != "PANEL_INIT_CONTROL_SOCKET" {
		t.Fatalf("ControlSocketEnv = %q", panelsupervision.ControlSocketEnv)
	}
	if panelsupervision.ControlTokenEnv != "PANEL_INIT_CONTROL_TOKEN" {
		t.Fatalf("ControlTokenEnv = %q", panelsupervision.ControlTokenEnv)
	}
	if panelsupervision.ControlTokenHeader != "X-Panel-Init-Token" {
		t.Fatalf("ControlTokenHeader = %q", panelsupervision.ControlTokenHeader)
	}
	if InitApplyPath != "/apply" || InitStatusPath != "/status" {
		t.Fatalf("control paths = %q %q", InitApplyPath, InitStatusPath)
	}
}
