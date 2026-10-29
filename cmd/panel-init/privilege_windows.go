//go:build windows

package main

import "os/exec"

// panelUID/panelGID 在 Windows 上不参与降权，保留变量以便共享代码引用。
var (
	panelUID = -1
	panelGID = -1
)

// resolvePanelUser 在 Windows 上不支持进程降权；返回错误让调用方记录告警。
func resolvePanelUser(name string) error {
	_ = name
	return exec.ErrNotFound
}

// shouldDropPrivileges 在 Windows 上恒为 false：没有 setuid 语义。
func shouldDropPrivileges(euid int) bool {
	_ = euid
	return false
}

// privilegeProblem 在 Windows 上没有对应的运行身份要求。
func privilegeProblem(euid int, panelUser string, resolvedUID int, resolveErr error) error {
	_, _, _, _ = euid, panelUser, resolvedUID, resolveErr
	return nil
}

// ensurePrivileges 在 Windows 上没有对应的运行身份要求。
func ensurePrivileges(panelUser string, resolveErr error) error {
	return privilegeProblem(0, panelUser, panelUID, resolveErr)
}

// applyPanelCredential 在 Windows 上不做任何处理。
func applyPanelCredential(cmd *exec.Cmd) {}
