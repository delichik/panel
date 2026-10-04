//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestPrivilegeProblemRequiresRoot 固定“不允许降级”的运行身份判据。
//
// 非 root 的 panel-init 既无法把 Panel 子进程降权（内核以 EPERM 拒绝 setuid，
// 表现为 "fork/exec /app/panel: operation not permitted"），也无法以内核 TUN
// 模式启动容器内 tailscaled；因此启动阶段必须直接拒绝，而不是降级运行。
func TestPrivilegeProblemRequiresRoot(t *testing.T) {
	err := privilegeProblem(1000, "panel", 1000, nil)
	if err == nil {
		t.Fatal("a non-root panel-init must be rejected")
	}
	if !strings.Contains(err.Error(), "must run as root") {
		t.Fatalf("error = %q, want an actionable root requirement", err)
	}
	if !strings.Contains(err.Error(), "user:") && !strings.Contains(err.Error(), "--user") {
		t.Fatalf("error = %q, want the non-root override hint", err)
	}
	if err := privilegeProblem(0, "panel", 1000, nil); err != nil {
		t.Fatalf("root with a resolved panel user must be accepted: %v", err)
	}
	// 非 root 时即使目标用户已解析也必须拒绝：拒绝的理由是身份，不是用户解析。
	if err := privilegeProblem(1000, "", -1, nil); err == nil {
		t.Fatal("a non-root panel-init must be rejected even with the opt-out flag")
	}
}

// TestPrivilegeProblemRejectsUnresolvedUser 解析不到目标用户属于配置错误：
// 静默把 Panel 子进程跑成 root 与镜像契约不符，必须显式拒绝，除非用户主动
// 传 -panel-user="" 表示确实要同身份运行。
func TestPrivilegeProblemRejectsUnresolvedUser(t *testing.T) {
	if err := privilegeProblem(0, "panel", -1, errors.New("user: unknown user panel")); err == nil {
		t.Fatal("an unresolved panel user must be rejected")
	} else if !strings.Contains(err.Error(), `-panel-user=""`) {
		t.Fatalf("error = %q, want the explicit opt-out hint", err)
	}
	if err := privilegeProblem(0, "panel", -1, nil); err == nil {
		t.Fatal("an unresolved panel user must be rejected even without a lookup error")
	}
	if err := privilegeProblem(0, "", -1, nil); err != nil {
		t.Fatalf("an explicitly empty panel user is an intentional opt-out: %v", err)
	}
}

// TestApplyPanelCredentialDropsOnlyAsRoot 保证只有 root 才写入 Credential：
// 非 root 写入会直接变成 EPERM 启动失败。
func TestApplyPanelCredentialDropsOnlyAsRoot(t *testing.T) {
	previousUID, previousGID := panelUID, panelGID
	t.Cleanup(func() { panelUID, panelGID = previousUID, previousGID })

	panelUID, panelGID = 1000, 1000
	cmd := exec.Command("/bin/true")
	applyPanelCredential(cmd)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Skip("test process runs as root; the drop path is exercised in production")
	}
	if cmd.SysProcAttr != nil {
		t.Fatalf("SysProcAttr = %#v, want none when privileges cannot be dropped", cmd.SysProcAttr)
	}

	if !shouldDropPrivileges(0) {
		t.Fatal("root panel-init with a resolved panel user must drop privileges")
	}
	if shouldDropPrivileges(1000) {
		t.Fatal("a non-root panel-init must not attempt setuid")
	}
	panelUID, panelGID = -1, -1
	if shouldDropPrivileges(0) {
		t.Fatal("an unresolved panel user must not be used for privilege dropping")
	}
}
