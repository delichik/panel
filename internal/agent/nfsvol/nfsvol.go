// Package nfsvol 提供 Panel NFS 卷的确定性命名与 Docker NFS 驱动参数。
// Agent 运行时与 Panel 侧使用同一套规则，保证双方对同一挂载计算同一卷名。
package nfsvol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"strings"
)

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// Name 返回 NFS 挂载对应的 Docker 卷名（与源、目标一一对应）。
func Name(source, target string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(source) + "|" + strings.TrimSpace(target)))
	return "panel-nfs-" + hex.EncodeToString(sum[:12])
}

// SplitSource 把 "host:/path"（IPv6 用 [host]:/path）拆成 host 与导出路径。
func SplitSource(source string) (string, string, error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "[") {
		end := strings.Index(source, "]")
		if end < 0 || end+1 >= len(source) || source[end+1] != ':' {
			return "", "", fmt.Errorf("invalid nfs source %q", source)
		}
		return source[1:end], source[end+2:], nil
	}
	idx := strings.Index(source, ":")
	if idx <= 0 || idx == len(source)-1 {
		return "", "", fmt.Errorf("invalid nfs source %q", source)
	}
	host := source[:idx]
	if net.ParseIP(host) == nil && !hostPattern.MatchString(host) {
		return "", "", fmt.Errorf("invalid nfs host %q", host)
	}
	return host, source[idx+1:], nil
}

// Options 返回创建 Docker local 卷时使用的 NFS driver 参数。
// readOnly 为 true 时挂载选项使用 ro。
func Options(source string, readOnly bool) (map[string]string, error) {
	host, export, err := SplitSource(source)
	if err != nil {
		return nil, err
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	return map[string]string{
		"type":   "nfs",
		"o":      "addr=" + host + "," + mode + ",nfsvers=4",
		"device": ":" + export,
	}, nil
}
