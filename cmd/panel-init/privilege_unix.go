//go:build !windows

package main

import (
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

// applyPanelCredential 让 Panel 子进程以非 root 身份运行；tailscaled 保持在
// panel-init 的 root 身份下运行。
func applyPanelCredential(cmd *exec.Cmd) {
	if cmd == nil || panelUID < 0 {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(panelUID), Gid: uint32(panelGID)},
	}
}
