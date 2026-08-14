package storage

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// ExportsMarker 标记 Panel 托管的 /etc/exports 块。
	ExportsMarker = "# panel-storage-share:managed"
	// ExportsPath 是 NFS 导出配置文件。
	ExportsPath = "/etc/exports"

	archiveTimeout = 10 * time.Minute
)

var (
	rootPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
)

// ConfigureExport 在 Agent 所在主机上配置存储共享导出：
//   - enabled=true：创建根目录、确保安装 nfs-kernel-server、把根目录导出给
//     allowed hosts（白名单），并重新加载导出；
//   - enabled=false：移除托管导出块并重新加载导出，不删除数据。
func ConfigureExport(ctx context.Context, root string, allowedHosts []string, enabled bool) error {
	if !validPath(root) {
		return fmt.Errorf("invalid storage root %q", root)
	}
	if !enabled {
		return removeExport(ctx, root)
	}
	if err := run(ctx, "mkdir", "-p", root); err != nil {
		return fmt.Errorf("create storage root: %w", err)
	}
	if err := ensureNFSServer(ctx); err != nil {
		return err
	}
	hosts := cleanHosts(allowedHosts)
	if len(hosts) == 0 {
		return fmt.Errorf("no allowed hosts configured for storage export")
	}
	current, _ := readExports(ctx)
	next := stripExports(current, root) + exportsBlock(root, hosts)
	return writeExports(ctx, next)
}

// ArchiveDirectory 把目录打包为 tar.gz 返回。
func ArchiveDirectory(ctx context.Context, pathValue string) ([]byte, error) {
	if !validPath(pathValue) {
		return nil, fmt.Errorf("invalid storage directory %q", pathValue)
	}
	ctx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	parent := filepath.Dir(pathValue)
	base := filepath.Base(pathValue)
	cmd := exec.CommandContext(ctx, "tar", "-czf", "-", "-C", parent, base)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("archive %s: %w: %s", pathValue, err, strings.TrimSpace(string(out)))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("archive %s is empty", pathValue)
	}
	return out, nil
}

// DeleteDirectory 删除目录及其内容（仅限 Panel 分配的存储分区路径）。
func DeleteDirectory(ctx context.Context, pathValue string) error {
	if !validPath(pathValue) {
		return fmt.Errorf("invalid storage directory %q", pathValue)
	}
	if err := run(ctx, "rm", "-rf", "--", pathValue); err != nil {
		return fmt.Errorf("delete %s: %w", pathValue, err)
	}
	return nil
}

func ensureNFSServer(ctx context.Context) error {
	if out, err := exec.CommandContext(ctx, "dpkg", "-s", "nfs-kernel-server").CombinedOutput(); err == nil {
		_ = out
		return nil
	}
	_ = run(ctx, "apt-get", "update", "-qq")
	if out, err := exec.CommandContext(ctx, "apt-get", "install", "-y", "--no-install-recommends", "nfs-kernel-server").CombinedOutput(); err != nil {
		return fmt.Errorf("install nfs-kernel-server: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeExport(ctx context.Context, root string) error {
	current, _ := readExports(ctx)
	return writeExports(ctx, strings.TrimRight(stripExports(current, root), "\n"))
}

func readExports(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "cat", ExportsPath).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeExports(ctx context.Context, content string) error {
	tmp, err := os.CreateTemp("", "panel-exports-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, ExportsPath); err != nil {
		return err
	}
	if err := run(ctx, "exportfs", "-ra"); err != nil {
		return fmt.Errorf("reload nfs exports: %w", err)
	}
	return nil
}

func exportsBlock(root string, hosts []string) string {
	var builder strings.Builder
	builder.WriteString(ExportsMarker)
	builder.WriteString("\n")
	builder.WriteString(root)
	for _, host := range hosts {
		builder.WriteString(" " + host + "(rw,sync,no_subtree_check,no_root_squash,insecure)")
	}
	builder.WriteString("\n")
	return builder.String()
}

func stripExports(content, root string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == ExportsMarker {
			skip = true
			continue
		}
		if skip {
			if trimmed == "" {
				continue
			}
			skip = false
			continue
		}
		if strings.HasPrefix(trimmed, root+" ") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func cleanHosts(hosts []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

func validPath(value string) bool {
	if !rootPattern.MatchString(value) {
		return false
	}
	parts := strings.Split(value, "/")
	for index, segment := range parts {
		if segment == ".." || segment == "." {
			return false
		}
		if segment == "" && index != 0 {
			return false
		}
	}
	return true
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
