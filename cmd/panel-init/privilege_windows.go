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

// applyPanelCredential 在 Windows 上不做任何处理。
func applyPanelCredential(cmd *exec.Cmd) {}
