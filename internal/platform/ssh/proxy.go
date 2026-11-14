package sshx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"

	panelerr "panel/internal/platform/errors"
)

// dialFunc 与 net.Dialer.DialContext 同形，便于把"直连 / 经代理 / 经 SSH
// 通道"组合成链。
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

func directDial(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// proxyDialer 按 target.Proxy 解析并建立一条到目标地址的连接。
//
// 返回的 net.Conn 在 Close 时会关闭整条链上的 SSH 客户端与底层连接，因此
// 调用方只需关闭最终连接（SSH 会话/上传下载都建立在其上）。
func (e *SSHExecutor) proxyDialer(ctx context.Context, serverID string, spec ProxySpec) (dialFunc, error) {
	hops, err := e.resolveProxyHops(ctx, serverID, spec)
	if err != nil {
		return nil, err
	}
	if len(hops) == 0 {
		return directDial, nil
	}
	base := directDial
	closers := make([]io.Closer, 0, len(hops))
	for index, hop := range hops {
		switch hop.Kind {
		case ProxyHopKindSocks5:
			base = socks5HopDialer(base, hop)
		case ProxyHopKindSSH:
			client, err := e.sshClientThroughHop(ctx, hop, base)
			if err != nil {
				closeAll(closers)
				return nil, hopDialError(index, hop, err)
			}
			closers = append(closers, client)
			base = sshHopDialer(client)
		default:
			closeAll(closers)
			return nil, panelerr.Validation("server_proxy_mode_invalid", "Unsupported proxy hop kind")
		}
	}
	return func(dialCtx context.Context, network, address string) (net.Conn, error) {
		conn, err := base(dialCtx, network, address)
		if err != nil {
			closeAll(closers)
			return nil, err
		}
		return &chainedConn{Conn: conn, closers: closers}, nil
	}, nil
}

// resolveProxyHops 校验目标代理配置并展开为跳列表。直连时返回 nil。
func (e *SSHExecutor) resolveProxyHops(ctx context.Context, serverID string, spec ProxySpec) ([]ProxyHop, error) {
	mode := spec.Mode
	if mode == "" || mode == ProxyModeDirect {
		return nil, nil
	}
	if mode != ProxyModeSSHTunnel && mode != ProxyModeSocks5 {
		return nil, panelerr.Validation("server_proxy_mode_invalid", "Unsupported proxy mode")
	}
	if e.proxyResolver == nil {
		return nil, panelerr.Validation("server_proxy_unavailable", "Server proxy resolution is unavailable")
	}
	hops, err := e.proxyResolver.ResolveProxyChain(ctx, serverID, spec)
	if err != nil {
		return nil, err
	}
	if len(hops) > MaxProxyChainHops {
		return nil, panelerr.Validation("server_proxy_chain_too_deep", "Server proxy chain is too deep")
	}
	for _, hop := range hops {
		if hop.Kind != ProxyHopKindSSH && hop.Kind != ProxyHopKindSocks5 {
			return nil, panelerr.Validation("server_proxy_mode_invalid", "Unsupported proxy hop kind")
		}
	}
	return hops, nil
}

// DialThroughProxy 供 agent gRPC 客户端使用：按服务器代理配置建立到指定地址
// 的连接。spec 为空/直连时与直接拨号等价。
func (e *SSHExecutor) DialThroughProxy(ctx context.Context, network, address, serverID string, spec ProxySpec) (net.Conn, error) {
	dial, err := e.proxyDialer(ctx, serverID, spec)
	if err != nil {
		return nil, err
	}
	conn, err := dial(ctx, network, address)
	if err != nil {
		return nil, panelerr.BadGateway("server_proxy_dial_failed", "Failed to connect through the configured proxy")
	}
	return conn, nil
}

// SetProxyResolver 注入代理链解析器。必须在服务开始处理请求前调用一次；
// 之后只读，避免并发写入。
func (e *SSHExecutor) SetProxyResolver(resolver ProxyResolver) {
	e.proxyResolver = resolver
}

func socks5HopDialer(base dialFunc, hop ProxyHop) dialFunc {
	return func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialSocks5(ctx, base, hop.Socks5Address, address, hop.Socks5Username, hop.Socks5Password)
	}
}

func sshHopDialer(client *ssh.Client) dialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return sshClientDialContext(ctx, client, network, address)
	}
}

// sshClientThroughHop 通过既有通道（直连或上一层代理）拨号到 SSH 跳板并完成
// SSH 握手。跳板链路的错误统一映射为 server_proxy_dial_failed：它表达的是
// "代理链路不可用"，与目标服务器自身的认证/主机密钥失败区分开。
func (e *SSHExecutor) sshClientThroughHop(ctx context.Context, hop ProxyHop, base dialFunc) (*ssh.Client, error) {
	resolved, err := e.resolver.Resolve(ctx, hop.CredentialID)
	if err != nil {
		return nil, err
	}
	auth, err := authMethod(resolved)
	if err != nil {
		return nil, err
	}
	user := hop.Username
	if user == "" {
		user = resolved.Username
	}
	address := net.JoinHostPort(hop.Host, strconv.Itoa(hop.Port))
	conn, err := base(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: e.hostKeyCallback(address), Timeout: e.timeout()}
	// SSH NewClientConn 没有 context 参数；用连接 deadline 让握手受调用方超时
	// 约束，握手完成后清除 deadline。
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(e.timeout())
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		_ = conn.Close()
		if errors.Is(err, ErrHostKeyMismatch) {
			return nil, panelerr.BadGateway("server_proxy_dial_failed", "SSH tunnel server host key verification failed")
		}
		return nil, panelerr.BadGateway("server_proxy_dial_failed", "SSH tunnel authentication or handshake failed")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// sshClientDialContext 在 SSH 连接上打开一条转发通道。ssh.Client.Dial 本身不
// 接受 context；用 goroutine + select 让调用方可以取消，取消时由链的关闭路径
// 《关闭 SSH 客户端》使 goroutine 退出。
func sshClientDialContext(ctx context.Context, client *ssh.Client, network, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := client.Dial(network, address)
		done <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-done:
		return out.conn, out.err
	}
}

func hopDialError(index int, hop ProxyHop, err error) error {
	if err == nil {
		return nil
	}
	var panel *panelerr.Error
	if errors.As(err, &panel) {
		return err
	}
	return panelerr.BadGateway("server_proxy_dial_failed", fmt.Sprintf("Failed to connect through proxy hop %d (%s)", index+1, hop.Kind))
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}

// chainedConn 在关闭最终连接时一并关闭代理链上的 SSH 客户端。
type chainedConn struct {
	net.Conn
	closers []io.Closer
}

func (c *chainedConn) Close() error {
	err := c.Conn.Close()
	closeAll(c.closers)
	return err
}
