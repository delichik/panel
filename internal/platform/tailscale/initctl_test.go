package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitControllerUnsupportedWithoutEnvironment(t *testing.T) {
	t.Setenv(InitURLEnv, "")
	t.Setenv(InitTokenEnv, "")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(InitTokenHeader)
		gotMethod = r.Method
		if r.URL.Path != InitApplyPath {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(Status{Available: true, Running: true, IPv4: "100.64.0.9"})
	}))
	defer server.Close()

	t.Setenv(InitURLEnv, server.URL)
	t.Setenv(InitTokenEnv, token)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv(InitURLEnv, server.URL)
	t.Setenv(InitTokenEnv, "token")
	if _, err := NewInitController().Status(context.Background()); err == nil {
		t.Fatal("expected an error for a rejected control request")
	}
}

func TestInitEnvironmentNamesStayStable(t *testing.T) {
	// 环境变量名与鉴权头是 panel-init 与 Panel 进程之间的隐式契约，改名会让
	// 容器内 tailscale 静默变成“不可用”，因此固定断言。
	if InitURLEnv != "PANEL_INIT_TAILSCALE_URL" {
		t.Fatalf("InitURLEnv = %q", InitURLEnv)
	}
	if InitTokenEnv != "PANEL_INIT_RESTART_TOKEN" {
		t.Fatalf("InitTokenEnv = %q", InitTokenEnv)
	}
	if InitTokenHeader != "X-Panel-Init-Token" {
		t.Fatalf("InitTokenHeader = %q", InitTokenHeader)
	}
	if InitApplyPath != "/apply" || InitStatusPath != "/status" {
		t.Fatalf("control paths = %q %q", InitApplyPath, InitStatusPath)
	}
}
