// Package tailscale 定义 Panel 容器内 tailscaled 的共享契约。
//
// Panel 进程负责生成期望态配置（config.json），panel-init 负责消费该配置并
// 管理 tailscaled 进程，同时把实际态写入状态文件并以 LocalAPI/CLI 结果回报。
// 两侧必须共用本包，避免出现只在其中一侧生效的字段或路径。
package tailscale

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// DirName 是 dataRoot 下保存 tailscaled 状态、LocalAPI socket 与 Panel
	// 期望态配置的目录。该目录由 panel-init 创建并交给 panel 用户，使 Panel
	// 进程可以写入 config.json，而 root 的 panel-init 仍可在其中保存节点身份。
	DirName = "tailscale"
	// ConfigFileName 是 Panel 写入、panel-init 读取的期望态配置。
	ConfigFileName = "config.json"
	// StatusFileName 是 panel-init 写入的实际态快照（root 私有）。
	StatusFileName = "status.json"
	// StateFileName 是 tailscaled 自身持久状态（节点身份与私钥）。
	StateFileName = "tailscaled.state"
	// SocketFileName 是 tailscaled LocalAPI socket。
	SocketFileName = "tailscaled.sock"

	// PanelHostname 是 Panel 容器加入 tailnet 时使用的固定主机名。
	PanelHostname = "seamark-panel"

	// DefaultTagsLimit 限制单个 tailnet 节点可声明的 tag 数量。
	DefaultTagsLimit = 16
)

// AuthKeyPrefix 是 Tailscale 认证密钥的固定前缀。
const AuthKeyPrefix = "tskey-"

// MaxAuthKeyLength 限制持久化的认证密钥长度，避免把异常输入当作合法配置。
const MaxAuthKeyLength = 512

var (
	// ErrAuthKeyRequired 表示需要认证密钥才能首次加入 tailnet。
	ErrAuthKeyRequired = errors.New("tailscale auth key is required")
	// ErrAuthKeyInvalid 表示认证密钥格式不合法。
	ErrAuthKeyInvalid = errors.New("tailscale auth key must start with " + AuthKeyPrefix)
	// ErrTagInvalid 表示 tag 格式不合法。
	ErrTagInvalid = errors.New("tailscale tag must look like tag:name")
	// ErrHostnameInvalid 表示主机名不合法。
	ErrHostnameInvalid = errors.New("tailscale hostname is invalid")
)

var (
	tagPattern      = regexp.MustCompile(`^tag:[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
)

// tailnetV4 是 Tailscale 为节点分配 IPv4 地址的 CGNAT 网段。
var tailnetV4 = mustCIDR("100.64.0.0/10")

// tailnetV6 是 Tailscale 为节点分配 IPv6 地址的 ULA 网段。
var tailnetV6 = mustCIDR("fd7a:115c:a1e0::/48")

// Config 是 Panel 下发给 panel-init 的 tailscale 期望态。
type Config struct {
	// Enabled 为真时 panel-init 必须保证 tailscaled 已启动并完成登录。
	Enabled bool `json:"enabled"`
	// AuthKey 只用于首次登录；已登录节点重新下发配置不要求再次提供。
	AuthKey string `json:"authKey,omitempty"`
	// Tags 是加入 tailnet 时声明的 ACL tag。
	Tags []string `json:"tags,omitempty"`
	// Hostname 覆盖默认的 Panel 主机名。
	Hostname string `json:"hostname,omitempty"`
}

// Status 是 panel-init 上报的容器内 tailscale 实际态。
type Status struct {
	// Available 表示当前部署可以管理容器内 tailscale（镜像含 tailscale 且
	// 具备内核 TUN 前提）。
	Available bool `json:"available"`
	// Running 表示 tailscaled 进程存活。
	Running bool `json:"running"`
	// LoggedIn 表示节点已加入 tailnet（BackendState 为 Running）。
	LoggedIn bool `json:"loggedIn"`
	// Hostname 是节点自身的 tailnet 主机名。
	Hostname string `json:"hostname,omitempty"`
	// IPv4/IPv6 是节点自身的 tailnet 地址。
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
	// Version 是节点上报的 tailscale 版本。
	Version string `json:"version,omitempty"`
	// BackendState 是 tailscaled 原始后端状态，供诊断使用。
	BackendState string `json:"backendState,omitempty"`
	// LastError 是最近一次失败的稳定英文描述，不含密钥。
	LastError string `json:"lastError,omitempty"`
	// UpdatedAt 是该快照的生成时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// Dir 返回 tailscale 工作目录。
func Dir(dataRoot string) string {
	return filepath.Join(strings.TrimSpace(dataRoot), DirName)
}

// ConfigPath 返回期望态配置文件路径。
func ConfigPath(dataRoot string) string { return filepath.Join(Dir(dataRoot), ConfigFileName) }

// StatusPath 返回实际态快照文件路径。
func StatusPath(dataRoot string) string { return filepath.Join(Dir(dataRoot), StatusFileName) }

// StatePath 返回 tailscaled 状态文件路径。
func StatePath(dataRoot string) string { return filepath.Join(Dir(dataRoot), StateFileName) }

// SocketPath 返回 tailscaled LocalAPI socket 路径。
func SocketPath(dataRoot string) string { return filepath.Join(Dir(dataRoot), SocketFileName) }

// NormalizeConfig 裁剪并校验期望态配置。缺少认证密钥时 Enabled 被强制为
// false，使“未配置”与“配置为空”表现一致。
func NormalizeConfig(cfg Config) (Config, error) {
	next := Config{
		Enabled:  cfg.Enabled,
		AuthKey:  strings.TrimSpace(cfg.AuthKey),
		Hostname: strings.TrimSpace(cfg.Hostname),
	}
	tags, err := NormalizeTags(cfg.Tags)
	if err != nil {
		return Config{}, err
	}
	next.Tags = tags
	if next.Hostname == "" {
		next.Hostname = PanelHostname
	}
	if err := ValidateHostname(next.Hostname); err != nil {
		return Config{}, err
	}
	if next.AuthKey != "" {
		if err := ValidateAuthKey(next.AuthKey); err != nil {
			return Config{}, err
		}
	}
	if next.AuthKey == "" {
		// 没有认证密钥就无法首次加入 tailnet；保持配置存在但明确为未启用。
		next.Enabled = false
	}
	return next, nil
}

// ValidateAuthKey 校验认证密钥格式。
func ValidateAuthKey(key string) error {
	value := strings.TrimSpace(key)
	if value == "" {
		return ErrAuthKeyRequired
	}
	if len(value) > MaxAuthKeyLength {
		return ErrAuthKeyInvalid
	}
	if !strings.HasPrefix(value, AuthKeyPrefix) || len(value) == len(AuthKeyPrefix) {
		return ErrAuthKeyInvalid
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return ErrAuthKeyInvalid
		}
	}
	return nil
}

// ValidateTag 校验单个 tag 格式。
func ValidateTag(tag string) error {
	if !tagPattern.MatchString(strings.TrimSpace(tag)) {
		return ErrTagInvalid
	}
	return nil
}

// ValidateHostname 校验节点主机名格式。
func ValidateHostname(hostname string) error {
	if !hostnamePattern.MatchString(strings.TrimSpace(hostname)) {
		return ErrHostnameInvalid
	}
	return nil
}

// NormalizeTags 裁剪、去重、排序并校验 tag 集合。
func NormalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		value := strings.ToLower(strings.TrimSpace(tag))
		if value == "" {
			continue
		}
		if err := ValidateTag(value); err != nil {
			return nil, err
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > DefaultTagsLimit {
		return nil, fmt.Errorf("at most %d tailscale tags are supported", DefaultTagsLimit)
	}
	sort.Strings(out)
	return out, nil
}

// IsTailnetAddress 判断地址是否落在 Tailscale 分配的节点地址网段内。
// 只有该网段内的地址才允许替换节点之间的连接地址。
func IsTailnetAddress(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return false
	}
	return tailnetV4.Contains(ip) || tailnetV6.Contains(ip)
}

// FirstTailnetAddress 返回优先使用的 tailnet 地址：IPv4 优先，其次 IPv6。
// 任一候选不合法时按无效处理，绝不返回非 tailnet 地址。
func FirstTailnetAddress(ipv4, ipv6 string) string {
	if IsTailnetAddress(ipv4) {
		return strings.TrimSpace(ipv4)
	}
	if IsTailnetAddress(ipv6) {
		return strings.TrimSpace(ipv6)
	}
	return ""
}

// ReadConfig 读取期望态配置。文件不存在等同于未配置，不返回错误。
func ReadConfig(dataRoot string) (Config, error) {
	raw, err := os.ReadFile(ConfigPath(dataRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode tailscale config: %w", err)
	}
	return NormalizeConfig(cfg)
}

// WriteConfig 以原子替换写入期望态配置，权限固定为 0640。
func WriteConfig(dataRoot string, cfg Config) error {
	next, err := NormalizeConfig(cfg)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ConfigPath(dataRoot), append(raw, '\n'), 0o640)
}

// ReadStatus 读取实际态快照。文件不存在等同于尚未运行。
func ReadStatus(dataRoot string) (Status, error) {
	raw, err := os.ReadFile(StatusPath(dataRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{}, nil
		}
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return Status{}, fmt.Errorf("decode tailscale status: %w", err)
	}
	return status, nil
}

// WriteStatus 以原子替换写入实际态快照，权限固定为 0600。
func WriteStatus(dataRoot string, status Status) error {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	raw, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(StatusPath(dataRoot), append(raw, '\n'), 0o600)
}

// EnsureDir 创建 tailscale 工作目录并返回其路径。owner/group 为 -1 时不做
// 属主调整；panel-init 以 root 运行时用它把目录交给 panel 用户，使 Panel 进程
// 可以写入 config.json。
func EnsureDir(dataRoot string, owner, group int) (string, error) {
	dir := Dir(dataRoot)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if owner >= 0 || group >= 0 {
		if err := os.Chown(dir, owner, group); err != nil {
			return "", fmt.Errorf("chown tailscale dir: %w", err)
		}
	}
	return dir, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
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
	return os.Rename(tmpName, path)
}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}
