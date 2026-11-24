package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	agentfirewall "panel/internal/agent/firewall"
)

// exportsMu 串行化导出配置读写，避免并发覆盖 /etc/exports。
var exportsMu sync.Mutex

const (
	// ExportsMarker 标记 Panel 托管的 /etc/exports 块。
	ExportsMarker = "# panel-storage-share:managed"
	// ExportsPath 是 NFS 导出配置文件。
	ExportsPath = "/etc/exports"
	// RootsStatePath 记录当前已配置的存储根目录，用于限制删除/归档/建目录范围。
	RootsStatePath = "/etc/panel-storage-roots.json"
	// NFSHostsStatePath 记录当前已通过 UFW 放行 2049 端口的受管节点 IP 列表。
	NFSHostsStatePath = "/etc/panel-storage-nfs-hosts.json"

	archiveTimeout  = 10 * time.Minute
	archiveMaxBytes = 512 << 20
)
var (
	rootPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

	deniedRootPrefixes = []string{
		"/etc", "/var", "/usr", "/bin", "/sbin", "/lib", "/boot",
		"/dev", "/proc", "/sys", "/run", "/home", "/root", "/tmp",
	}
)

// ConfigureExport 配置存储共享导出：
//   - enabled=true：创建根目录、校验 nfs-kernel-server 已安装（安装由 Panel 通过
//     SSH 完成）并确保服务运行、把根目录导出给 allowed hosts 并重新加载导出；
//   - enabled=false：移除托管导出块、注销根目录并重新加载导出，不删除数据。
func ConfigureExport(ctx context.Context, root string, allowedHosts []string, enabled bool) error {
	exportsMu.Lock()
	defer exportsMu.Unlock()
	if !validRoot(root) {
		return fmt.Errorf("invalid storage root %q", root)
	}
	if !enabled {
		return removeExport(ctx, root)
	}
	if err := run(ctx, "mkdir", "-p", root); err != nil {
		return fmt.Errorf("create storage root: %w", err)
	}
	if out, err := exec.CommandContext(ctx, "dpkg", "-s", "nfs-kernel-server").CombinedOutput(); err != nil {
		_ = out
		return fmt.Errorf("nfs-kernel-server is not installed; install it on the storage server first")
	}
	if err := ensureNFSServerRunning(ctx); err != nil {
		return err
	}
	hosts := cleanHosts(allowedHosts)
	if len(hosts) == 0 {
		return fmt.Errorf("no allowed hosts configured for storage export")
	}
	current, err := readExports(ctx)
	if err != nil {
		return fmt.Errorf("read exports: %w", err)
	}
	next := ensureTrailingNewline(stripExports(current, root)) + exportsBlock(root, hosts)
	if err := writeExports(ctx, next); err != nil {
		return err
	}
	if err := registerRoot(ctx, root); err != nil {
		return err
	}
	return ensureNFSPortsAllowed(ctx, hosts)
}

// EnsureDirectory 创建存储分区目录（幂等），仅允许在已注册根目录之下。
func EnsureDirectory(ctx context.Context, pathValue string) error {
	if !validPath(pathValue) {
		return fmt.Errorf("invalid storage directory %q", pathValue)
	}
	if _, err := resolveUnderRegisteredRoot(pathValue, false); err != nil {
		return err
	}
	if err := run(ctx, "mkdir", "-p", pathValue); err != nil {
		return fmt.Errorf("create storage directory %s: %w", pathValue, err)
	}
	return nil
}

// ArchiveDirectory 把目录打包为 tar.gz 返回（仅允许已注册根目录之下）。
func ArchiveDirectory(ctx context.Context, pathValue string) ([]byte, error) {
	if !validPath(pathValue) {
		return nil, fmt.Errorf("invalid storage directory %q", pathValue)
	}
	resolved, err := resolveUnderRegisteredRoot(pathValue, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, archiveTimeout)
	defer cancel()
	parent := filepath.Dir(resolved)
	base := filepath.Base(resolved)
	cmd := exec.CommandContext(ctx, "tar", "-czf", "-", "-C", parent, base)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("archive %s: %w", pathValue, err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("archive %s: %w", pathValue, err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdoutPipe, archiveMaxBytes+1))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("archive %s: %w", pathValue, readErr)
	}
	if int64(len(out)) > archiveMaxBytes {
		return nil, fmt.Errorf("archive %s exceeds maximum size limit", pathValue)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("archive %s: %w: %s", pathValue, waitErr, strings.TrimSpace(stderr.String()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("archive %s is empty", pathValue)
	}
	return out, nil
}

// DeleteDirectory 删除目录及其内容（仅允许已注册根目录之下）。
func DeleteDirectory(ctx context.Context, pathValue string) error {
	if !validPath(pathValue) {
		return fmt.Errorf("invalid storage directory %q", pathValue)
	}
	resolved, err := resolveUnderRegisteredRoot(pathValue, false)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(resolved); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if err := run(ctx, "rm", "-rf", "--", resolved); err != nil {
		return fmt.Errorf("delete %s: %w", pathValue, err)
	}
	return nil
}

func removeExport(ctx context.Context, root string) error {
	current, err := readExports(ctx)
	if err != nil {
		return fmt.Errorf("read exports: %w", err)
	}
	next := strings.TrimRight(stripExports(current, root), "\n")
	if err := writeExports(ctx, next); err != nil {
		return err
	}
	if err := unregisterRoot(ctx, root); err != nil {
		return err
	}
	return removeNFSPortRules(ctx)
}

func readExports(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "cat", ExportsPath).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// writeExports 原子写 /etc/exports：临时文件与目标同目录、写入后 fsync、
// 保留备份，exportfs 失败时回滚。
func writeExports(ctx context.Context, content string) error {
	dir := filepath.Dir(ExportsPath)
	tmp, err := os.CreateTemp(dir, ".panel-exports-*")
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	backup := ExportsPath + ".panel-bak"
	hadBackup := false
	if _, err := os.Stat(ExportsPath); err == nil {
		if err := os.Rename(ExportsPath, backup); err != nil {
			return err
		}
		hadBackup = true
	}
	if err := os.Rename(tmpName, ExportsPath); err != nil {
		if hadBackup {
			_ = os.Rename(backup, ExportsPath)
		}
		return err
	}
	if err := run(ctx, "exportfs", "-ra"); err != nil {
		if hadBackup {
			_ = os.Rename(backup, ExportsPath)
		}
		return fmt.Errorf("reload nfs exports: %w", err)
	}
	_ = os.Remove(backup)
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

func ensureTrailingNewline(value string) string {
	if value == "" {
		return value
	}
	if strings.HasSuffix(value, "\n") {
		return value
	}
	return value + "\n"
}

// ensureNFSServerRunning 确保 nfs-kernel-server 服务已启用并运行：优先
// systemd，无 systemd 的环境回退 sysvinit 的 service 命令。配置导出依赖
// rpc.nfsd/rpc.mountd 进程，仅安装包不启动服务时 exportfs/showmount 与
// 客户端挂载都会失败。Panel 的 5 分钟导出同步周期会调用 ConfigureExport，
// 因此本函数同时承担自愈：服务被手动停止后会在下一次周期同步自动拉起。
func ensureNFSServerRunning(ctx context.Context) error {
	if err := run(ctx, "systemctl", "enable", "--now", "nfs-kernel-server"); err == nil {
		return nil
	}
	if err := run(ctx, "service", "nfs-kernel-server", "start"); err == nil {
		return nil
	}
	return fmt.Errorf("start nfs-kernel-server service: systemctl and service both failed")
}

// ensureNFSPortsAllowed 在 UFW 已安装时仅对允许的受管节点 IP 放行 NFS 端口
// （2049/tcp 与 2049/udp），并清理旧版全局 2049 规则与已移除节点的规则。
// 未安装 UFW 时跳过；云厂商安全组等 UFW 之外的防火墙需用户自行按节点 IP 放行 2049。
func ensureNFSPortsAllowed(ctx context.Context, hosts []string) error {
	if _, err := exec.LookPath("ufw"); err != nil {
		return nil
	}
	targetIPs := resolveFirewallIPs(hosts)
	return agentfirewall.WithMutationLock(func() error {
		// 尽力清理旧版全局放行规则，避免 2049 对公网开放。
		removeLegacyGlobalNFSPortRules(ctx)

		prevIPs := readNFSHosts()
		targetSet := make(map[string]struct{}, len(targetIPs))
		for _, ip := range targetIPs {
			targetSet[ip] = struct{}{}
		}
		for _, oldIP := range prevIPs {
			if _, keep := targetSet[oldIP]; keep {
				continue
			}
			deleteNFSPortRulesForIP(ctx, oldIP)
		}
		for _, ip := range targetIPs {
			for _, proto := range []string{"tcp", "udp"} {
				if out, err := exec.CommandContext(ctx, "ufw", "allow", "from", ip, "to", "any", "port", "2049", "proto", proto).CombinedOutput(); err != nil {
					return fmt.Errorf("allow nfs port 2049/%s for %s: %w: %s", proto, ip, err, strings.TrimSpace(string(out)))
				}
			}
		}
		return writeNFSHosts(targetIPs)
	})
}

// removeNFSPortRules 尽力移除所有托管的 NFS 端口放行规则（含旧版全局规则）。
func removeNFSPortRules(ctx context.Context) error {
	if _, err := exec.LookPath("ufw"); err != nil {
		return nil
	}
	return agentfirewall.WithMutationLock(func() error {
		removeLegacyGlobalNFSPortRules(ctx)
		for _, ip := range readNFSHosts() {
			deleteNFSPortRulesForIP(ctx, ip)
		}
		_ = writeNFSHosts(nil)
		return nil
	})
}

func removeLegacyGlobalNFSPortRules(ctx context.Context) {
	for _, spec := range []string{"2049/tcp", "2049/udp"} {
		_, _ = exec.CommandContext(ctx, "ufw", "delete", "allow", spec).CombinedOutput()
	}
}

func deleteNFSPortRulesForIP(ctx context.Context, ip string) {
	if net.ParseIP(ip) == nil {
		return
	}
	for _, proto := range []string{"tcp", "udp"} {
		_, _ = exec.CommandContext(ctx, "ufw", "delete", "allow", "from", ip, "to", "any", "port", "2049", "proto", proto).CombinedOutput()
	}
}

func resolveFirewallIPs(hosts []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		text := ip.String()
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			add(ip)
			continue
		}
		if resolved, err := net.LookupIP(host); err == nil {
			for _, ip := range resolved {
				add(ip)
			}
		}
	}
	sort.Strings(out)
	return out
}

func readNFSHosts() []string {
	content, err := os.ReadFile(NFSHostsStatePath)
	if err != nil {
		return nil
	}
	var hosts []string
	if err := json.Unmarshal(content, &hosts); err != nil {
		return nil
	}
	return hosts
}

func writeNFSHosts(hosts []string) error {
	if hosts == nil {
		hosts = []string{}
	}
	sort.Strings(hosts)
	content, _ := json.Marshal(hosts)
	tmp, err := os.CreateTemp(filepath.Dir(NFSHostsStatePath), ".panel-nfs-hosts-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, NFSHostsStatePath)
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

func validRoot(root string) bool {
	if !validPath(root) || root == "/" {
		return false
	}
	for _, prefix := range deniedRootPrefixes {
		if root == prefix || strings.HasPrefix(root, prefix+"/") {
			return false
		}
	}
	return true
}

func readRoots() []string {
	content, err := os.ReadFile(RootsStatePath)
	if err != nil {
		return nil
	}
	var roots []string
	if err := json.Unmarshal(content, &roots); err != nil {
		return nil
	}
	return roots
}

func writeRoots(roots []string) error {
	sort.Strings(roots)
	content, _ := json.Marshal(roots)
	tmp, err := os.CreateTemp(filepath.Dir(RootsStatePath), ".panel-roots-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, RootsStatePath)
}

func registerRoot(_ context.Context, root string) error {
	roots := readRoots()
	for _, existing := range roots {
		if existing == root {
			return nil
		}
	}
	return writeRoots(append(roots, root))
}

func unregisterRoot(_ context.Context, root string) error {
	roots := readRoots()
	out := []string{}
	for _, existing := range roots {
		if existing != root {
			out = append(out, existing)
		}
	}
	return writeRoots(out)
}

func underRegisteredRoot(pathValue string) bool {
	_, err := resolveUnderRegisteredRoot(pathValue, false)
	return err == nil
}

// resolveUnderRegisteredRoot 校验 pathValue 位于已注册的存储根目录之下，
// 且从根目录到目标路径（或其最近已存在祖先）不经过任何符号链接，防止
// 容器/NFS 客户端通过软链读写或删除根目录外的宿主机路径。
func resolveUnderRegisteredRoot(pathValue string, mustExist bool) (string, error) {
	return resolveUnderRoots(readRoots(), pathValue, mustExist)
}

func resolveUnderRoots(roots []string, pathValue string, mustExist bool) (string, error) {
	cleanPath := filepath.Clean(pathValue)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if cleanRoot == "" || cleanPath == cleanRoot {
			continue
		}
		prefix := cleanRoot + string(os.PathSeparator)
		if !strings.HasPrefix(cleanPath, prefix) {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(cleanRoot)
		if err != nil {
			return "", fmt.Errorf("resolve storage root %q: %w", cleanRoot, err)
		}
		rel, err := filepath.Rel(cleanRoot, cleanPath)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("directory %q escapes storage root %q", pathValue, cleanRoot)
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		cur := realRoot
		for i, part := range parts {
			cur = filepath.Join(cur, part)
			info, statErr := os.Lstat(cur)
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					if mustExist {
						return "", fmt.Errorf("storage directory %q does not exist", pathValue)
					}
					for _, rest := range parts[i+1:] {
						cur = filepath.Join(cur, rest)
					}
					break
				}
				return "", fmt.Errorf("stat storage path %q: %w", cur, statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("storage path %q traverses symbolic link", pathValue)
			}
		}
		realPrefix := realRoot + string(os.PathSeparator)
		if !strings.HasPrefix(cur, realPrefix) {
			return "", fmt.Errorf("directory %q escapes storage root %q", pathValue, cleanRoot)
		}
		return cur, nil
	}
	return "", fmt.Errorf("directory %q is not under a registered storage root", pathValue)
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
