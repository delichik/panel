package sshx

import (
	"context"
	"time"
)

const (
	CredentialTypePassword   = "password"
	CredentialTypePrivateKey = "private_key"
	PrivilegeModeRoot        = "root"
	PrivilegeModeSudo        = "passwordless_sudo"
	PrivilegeModeNone        = "none"
)

// 服务器连接代理模式。direct 是缺省值；ssh_tunnel 用另一台已登记服务器做
// SSH 跳板；socks5 使用用户手填的 SOCKS5 代理。
const (
	ProxyModeDirect    = "direct"
	ProxyModeSSHTunnel = "ssh_tunnel"
	ProxyModeSocks5    = "socks5"

	ProxyHopKindSSH    = "ssh"
	ProxyHopKindSocks5 = "socks5"

	// MaxProxyChainHops 是连接一个目标服务器最多允许的代理跳数。解析层（
	// servers 模块）和拨号层都执行该上限，防止脏数据形成递归或超长链路。
	MaxProxyChainHops = 8
)

type ResolvedCredential struct {
	ID         string
	Type       string
	Username   string
	Password   string
	PrivateKey []byte
	Passphrase string
}

type Target struct {
	ServerID      string
	Host          string
	Port          int
	Username      string
	CredentialID  string
	PrivilegeMode string
	// Proxy 是连接该目标服务器（SSH 与 Agent）时使用的代理配置。空值等价于
	// direct。已保存服务器的 SOCKS5 密码不在 Target 中携带，由 ProxyResolver
	// 从持久化密文解析；只有探测草稿会显式带上明文密码。
	Proxy ProxySpec
}

// ProxySpec 是一台服务器自身的代理配置输入。
type ProxySpec struct {
	Mode           string
	TunnelServerID string
	Socks5Address  string
	Socks5Username string
	// Socks5Password 只在探测草稿中携带；已保存服务器的密码由
	// ProxyResolver 解密后填入 ProxyHop。
	Socks5Password string
}

// ProxyHop 是解析后的单跳代理。链顺序为“最外层到目标”：先经过 hops[0]，
// 再 hops[1]，……最后才拨号目标地址。
type ProxyHop struct {
	Kind string
	// SSH 跳板字段。
	Host         string
	Port         int
	Username     string
	CredentialID string
	// SOCKS5 字段。
	Socks5Address  string
	Socks5Username string
	Socks5Password string
}

// ProxyResolver 把服务器自身的代理配置展开为有界、无环的代理跳列表。
// serverID 为空表示探测草稿（此时 Socks5Password 由调用方显式提供）。
type ProxyResolver interface {
	ResolveProxyChain(ctx context.Context, serverID string, spec ProxySpec) ([]ProxyHop, error)
}

type CommandSpec struct {
	Command  string
	Env      map[string]string
	Timeout  time.Duration
	OnStdout func(line string)
	OnStderr func(line string)
}

type CommandResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
}

type UploadSpec struct {
	LocalPath  string
	RemotePath string
	// Timeout overrides the shared remote command timeout for this transfer.
	// Large payloads such as the agent bundle need a longer bound than the
	// default remote command timeout; zero keeps the shared timeout.
	Timeout time.Duration
}

type DownloadSpec struct {
	RemotePath string
	LocalPath  string
}

type RemoteExecutor interface {
	Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
	ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
	Upload(ctx context.Context, target Target, transfer UploadSpec) error
	Download(ctx context.Context, target Target, transfer DownloadSpec) error
}

type CredentialResolver interface {
	Resolve(ctx context.Context, credentialID string) (ResolvedCredential, error)
}
