package panelsupervision

import (
	"flag"
	"path/filepath"
	"slices"
	"testing"
)

// TS-INIT-005 参数合同回归：panel-init 用 Args 下发的每个参数都必须能被 Panel
// 侧 Register 注册的 flag 解析。历史事故是 panel 漏注册参数，容器以
// "flag provided but not defined" 启动失败。
func TestArgsParseWithRegisteredFlags(t *testing.T) {
	cfg := Config{
		MaintenanceMode: "restore",
		ControlSocket:   "/app/data/run/panel-control.sock",
		ControlToken:    "opaque-token",
	}

	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	var got Config
	Register(fs, &got)

	if err := fs.Parse(Args(cfg)); err != nil {
		t.Fatalf("panel rejected supervision args %q: %v", Args(cfg), err)
	}
	if got != cfg {
		t.Fatalf("parsed supervision config = %+v, want %+v", got, cfg)
	}
}

// TestArgsShape 固定监督参数的顺序与 flag 名：镜像构建冒烟与 Dockerfile 依赖
// 同一组字面量。
func TestArgsShape(t *testing.T) {
	args := Args(Config{
		MaintenanceMode: "normal",
		ControlSocket:   "/tmp/panel-control.sock",
		ControlToken:    "token",
	})
	want := []string{
		"--maintenance-mode", "normal",
		"--init-control-socket", "/tmp/panel-control.sock",
		"--init-control-token", "token",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("Args() = %q, want %q", args, want)
	}
}

func TestControlSocketPathShape(t *testing.T) {
	got := ControlSocketPath("/app/data")
	if filepath.Base(got) != "panel-control.sock" {
		t.Fatalf("ControlSocketPath() = %q, want a panel-control.sock leaf", got)
	}
	if filepath.Base(filepath.Dir(got)) != "run" {
		t.Fatalf("ControlSocketPath() = %q, want the run directory under the data root", got)
	}
}
