package storage

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ExportStatus 描述一台存储服务器上 NFS 导出的生效状态。
type ExportStatus struct {
	ServerInstalled bool
	RootExists      bool
	ExportLive      bool
	Detail          string
}

// Status 检查根目录、nfs-kernel-server 安装与 showmount 导出生效情况。
func Status(ctx context.Context, root string) ExportStatus {
	status := ExportStatus{}
	status.RootExists = dirExists(root)
	if out, err := exec.CommandContext(ctx, "dpkg", "-s", "nfs-kernel-server").CombinedOutput(); err == nil {
		status.ServerInstalled = true
		_ = out
	} else {
		status.Detail = "nfs-kernel-server is not installed"
		return status
	}
	if !status.RootExists {
		status.Detail = "storage root does not exist"
		return status
	}
	if out, err := exec.CommandContext(ctx, "showmount", "-e", "localhost").CombinedOutput(); err == nil {
		if exportsContainRoot(string(out), root) {
			if !nfsServerRunning(ctx) {
				status.Detail = "export is listed but the nfs server is not running"
				return status
			}
			status.ExportLive = true
			return status
		}
		status.Detail = "export is not loaded (showmount does not list " + root + ")"
		return status
	} else if out2, err2 := exec.CommandContext(ctx, "exportfs", "-s").CombinedOutput(); err2 == nil {
		if exportsContainRoot(string(out2), root) {
			if !nfsServerRunning(ctx) {
				status.Detail = "export is listed but the nfs server is not running"
				return status
			}
			status.ExportLive = true
			return status
		}
		status.Detail = "export is not loaded (exportfs does not list " + root + ")"
		return status
	} else {
		status.Detail = "cannot query exports: " + strings.TrimSpace(string(out))
		return status
	}
}

// nfsServerRunning 判断内核 NFS 服务是否在提供服务。
// 现代 nfs-kernel-server（含 Ubuntu 24.04）上 rpc.nfsd 只是把线程数写入
// /proc/fs/nfsd/threads 后退出，systemd 显示 active (exited)；此时 pgrep
// rpc.nfsd 会假阴性。优先读 threads；没有 proc 节点时再回退 pgrep。
func nfsServerRunning(ctx context.Context) bool {
	if raw, err := os.ReadFile("/proc/fs/nfsd/threads"); err == nil {
		if n, ok := parseNFSThreadCount(string(raw)); ok {
			return n > 0
		}
	}
	if _, err := exec.CommandContext(ctx, "pgrep", "-x", "rpc.nfsd").CombinedOutput(); err == nil {
		return true
	}
	return false
}

func parseNFSThreadCount(raw string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, false
	}
	return n, true
}

// MountStatus 描述应用节点上某个 NFS 挂载点是否已挂载且可读写。
type MountStatus struct {
	Mounted  bool
	Writable bool
	Detail   string
}

// MountStatusAt 检查挂载点是否为 NFS 挂载并做一次写探测。
func MountStatusAt(ctx context.Context, mountpoint string) MountStatus {
	status := MountStatus{}
	if !validPath(mountpoint) {
		status.Detail = "invalid mountpoint"
		return status
	}
	if !isNFSMount(mountpoint) {
		status.Detail = "volume is not mounted as NFS"
		return status
	}
	status.Mounted = true
	probeCh := make(chan error, 1)
	go func() { probeCh <- probeWritable(ctx, mountpoint) }()
	select {
	case err := <-probeCh:
		if err != nil {
			status.Detail = "write probe failed: " + err.Error()
			return status
		}
		status.Writable = true
		return status
	case <-time.After(5 * time.Second):
		status.Detail = "write probe timed out (storage may be unresponsive)"
		return status
	case <-ctx.Done():
		status.Detail = "write probe cancelled"
		return status
	}
}

func exportsContainRoot(output, root string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if field == root {
				return true
			}
		}
	}
	return false
}

func dirExists(pathValue string) bool {
	info, err := os.Stat(pathValue)
	return err == nil && info.IsDir()
}

func isNFSMount(mountpoint string) bool {
	content, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[1] != mountpoint {
			continue
		}
		if fields[2] == "nfs" || fields[2] == "nfs4" {
			return true
		}
	}
	return false
}

func probeWritable(ctx context.Context, dir string) error {
	probe, err := os.CreateTemp(dir, ".panel-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if _, err := probe.WriteString("ok"); err != nil {
		_ = probe.Close()
		_ = os.Remove(name)
		return err
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
