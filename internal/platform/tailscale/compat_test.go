package tailscale_test

import (
	"testing"

	"panel/internal/modules/backups"
	"panel/internal/platform/panelsupervision"
	"panel/internal/platform/tailscale"
)

// TestControlPlaneContractPins 固定 panel-init 控制面的线格式：Unix socket 与
// 令牌环境变量、鉴权头，以及维护重启和 tailscale 两条控制路由。
//
// panel-init 的监听器与 Panel 侧两个客户端（重启、tailscale）现在共用
// internal/platform/panelsupervision 中的同一组常量，两侧漂移已由结构消除；
// 本用例再固定字面量，防止无意改名让控制面静默失联。
func TestControlPlaneContractPins(t *testing.T) {
	if panelsupervision.ControlSocketEnv != "PANEL_INIT_CONTROL_SOCKET" {
		t.Fatalf("control socket env = %q", panelsupervision.ControlSocketEnv)
	}
	if panelsupervision.ControlTokenEnv != "PANEL_INIT_CONTROL_TOKEN" {
		t.Fatalf("control token env = %q", panelsupervision.ControlTokenEnv)
	}
	if panelsupervision.ControlTokenHeader != "X-Panel-Init-Token" {
		t.Fatalf("control token header = %q", panelsupervision.ControlTokenHeader)
	}
	if backups.InitRestartPath != "/restart" {
		t.Fatalf("restart path = %q", backups.InitRestartPath)
	}
	if tailscale.InitApplyPath != "/apply" || tailscale.InitStatusPath != "/status" {
		t.Fatalf("tailscale control paths = %q %q", tailscale.InitApplyPath, tailscale.InitStatusPath)
	}
}
