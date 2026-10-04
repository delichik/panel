//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// panelUID/panelGID 是 Panel 子进程降权后的身份，-1 表示不降权（例如在非容器
// 环境直接运行 panel-init）。panel-init 自身以 root 运行，以便与 tailscaled
// 共享需要 NET_ADMIN 的内核 TUN 网络能力。
var (
	panelUID = -1
	panelGID = -1
)

// resolvePanelUser 解析 Panel 运行身份。解析失败不是致命错误：开发机上可能
// 不存在 panel 用户，此时保持不降权并运行。
func resolvePanelUser(name string) error {
	value := strings.TrimSpace(name)
	if value == "" {
		return nil
	}
	account, err := user.Lookup(value)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	panelUID = uid
	panelGID = gid
	return nil
}

// shouldDropPrivileges 报告是否把 Panel 子进程降权到 panel 用户。
//
// 只有 panel-init 本身以 root 运行时才降权；非 root 的情况由 privilegeProblem
// 在启动阶段直接拒绝，不会走到这里。
func shouldDropPrivileges(euid int) bool {
	return panelUID >= 0 && euid == 0
}

// privilegeProblem 校验 panel-init 的运行身份，返回可操作的拒绝原因。
//
// 这里不做任何降级：非 root 既无法把 Panel 子进程降权（内核以 EPERM 拒绝
// setuid，表现为 "fork/exec /app/panel: operation not permitted"），也无法以内核
// TUN 模式启动容器内 tailscaled。与其让容器带着一个悄悄失效的能力运行，不如在
// 启动阶段就明确拒绝并说明怎么修。
func privilegeProblem(euid int, panelUser string, resolvedUID int, resolveErr error) error {
	if euid != 0 {
		return fmt.Errorf("panel-init must run as root: it starts tailscaled in kernel TUN mode and drops the panel child to the %q user; remove any non-root user override (compose \"user:\", docker run --user) or an older image that set USER", defaultPanelUser())
	}
	if strings.TrimSpace(panelUser) == "" {
		// 显式要求不降权是允许的：Panel 子进程与 panel-init 同身份运行。
		return nil
	}
	if resolveErr != nil {
		return fmt.Errorf("panel user %q cannot be resolved (%v); fix the image or user database, or pass -panel-user=\"\" to run the panel child as root on purpose", panelUser, resolveErr)
	}
	if resolvedUID < 0 {
		return fmt.Errorf("panel user %q cannot be resolved; fix the image or user database, or pass -panel-user=\"\" to run the panel child as root on purpose", panelUser)
	}
	return nil
}

// ensurePrivileges 是 privilegeProblem 的进程内入口。
func ensurePrivileges(panelUser string, resolveErr error) error {
	return privilegeProblem(os.Geteuid(), panelUser, panelUID, resolveErr)
}

// applyPanelCredential 让 Panel 子进程以非 root 身份运行；tailscaled 保持在
// panel-init 的 root 身份下运行。
func applyPanelCredential(cmd *exec.Cmd) {
	if cmd == nil || !shouldDropPrivileges(os.Geteuid()) {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(panelUID), Gid: uint32(panelGID)},
	}
}
