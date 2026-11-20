package sshx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// 最小 SOCKS5 客户端（RFC 1928 + RFC 1929 用户名/密码认证），只支持 CONNECT。
// 该实现有意不引入额外依赖：Panel 需要的只是"经过 SOCKS5 建立一条到目标
// host:port 的 TCP 连接"这一能力。

const (
	socks5Version    = 0x05
	socks5AuthNone   = 0x00
	socks5AuthUser   = 0x02
	socks5CmdConnect = 0x01
	socks5AtypIPv4   = 0x01
	socks5AtypDomain = 0x03
	socks5AtypIPv6   = 0x04
)

// socks5HandshakeTimeout bounds the SOCKS5 negotiation when the caller's
// context carries no deadline. It is independent of the remote command timeout:
// negotiation happens once per connection and must never hang a task.
const socks5HandshakeTimeout = 30 * time.Second

// dialSocks5 connects to targetAddress through the SOCKS5 proxy at
// proxyAddress. base dials the proxy address itself (it may already be routed
// through an SSH hop or another proxy).
func dialSocks5(ctx context.Context, base dialFunc, proxyAddress, targetAddress, username, password string) (net.Conn, error) {
	conn, err := base(ctx, "tcp", proxyAddress)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		_ = conn.Close()
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(socks5HandshakeTimeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fail(err)
	}
	if err := socks5Negotiate(conn, targetAddress, username, password); err != nil {
		return fail(err)
	}
	// 清除协商阶段设置的 deadline，连接生命周期由调用方控制。
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	return conn, nil
}

func socks5Negotiate(conn net.Conn, targetAddress, username, password string) error {
	if err := socks5Authenticate(conn, username, password); err != nil {
		return err
	}
	host, port, err := splitDialAddress(targetAddress)
	if err != nil {
		return err
	}
	request, err := socks5ConnectRequest(host, port)
	if err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("write SOCKS5 connect request: %w", err)
	}
	// 应答固定先读 4 字节：VER REP RSV ATYP。
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("read SOCKS5 connect reply: %w", err)
	}
	if head[0] != socks5Version {
		return fmt.Errorf("SOCKS5 proxy returned unsupported version %d", head[0])
	}
	if head[1] != 0x00 {
		return fmt.Errorf("SOCKS5 proxy refused connection: %s", socks5ReplyMessage(head[1]))
	}
	// 丢弃 BND.ADDR + BND.PORT。
	var remainder int
	switch head[3] {
	case socks5AtypIPv4:
		remainder = 4 + 2
	case socks5AtypIPv6:
		remainder = 16 + 2
	case socks5AtypDomain:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return fmt.Errorf("read SOCKS5 bound address: %w", err)
		}
		remainder = int(length[0]) + 2
	default:
		return fmt.Errorf("SOCKS5 proxy returned unsupported address type %d", head[3])
	}
	if remainder > 0 {
		if _, err := io.CopyN(io.Discard, conn, int64(remainder)); err != nil {
			return fmt.Errorf("read SOCKS5 bound address: %w", err)
		}
	}
	return nil
}

func socks5Authenticate(conn net.Conn, username, password string) error {
	methods := []byte{socks5AuthNone}
	if username != "" || password != "" {
		// 同时提供无认证与用户名/密码，由代理解决定；配置了凭据时仍允许
		// 代理在无认证模式下工作（部分内网代理如此）。
		methods = append(methods, socks5AuthUser)
	}
	greeting := append([]byte{socks5Version, byte(len(methods))}, methods...)
	if _, err := conn.Write(greeting); err != nil {
		return fmt.Errorf("write SOCKS5 greeting: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("read SOCKS5 greeting reply: %w", err)
	}
	if reply[0] != socks5Version {
		return fmt.Errorf("SOCKS5 proxy returned unsupported version %d", reply[0])
	}
	switch reply[1] {
	case socks5AuthNone:
		return nil
	case socks5AuthUser:
		return socks5UserPasswordAuth(conn, username, password)
	default:
		return errors.New("SOCKS5 proxy did not accept the offered authentication methods")
	}
}

func socks5UserPasswordAuth(conn net.Conn, username, password string) error {
	if len(username) > 255 || len(password) > 255 {
		return errors.New("SOCKS5 username and password must be at most 255 bytes")
	}
	request := make([]byte, 0, 3+len(username)+len(password))
	request = append(request, 0x01, byte(len(username)))
	request = append(request, username...)
	request = append(request, byte(len(password)))
	request = append(request, password...)
	if _, err := conn.Write(request); err != nil {
		return fmt.Errorf("write SOCKS5 authentication: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return fmt.Errorf("read SOCKS5 authentication reply: %w", err)
	}
	if reply[0] != 0x01 {
		return fmt.Errorf("SOCKS5 proxy returned unsupported authentication version %d", reply[0])
	}
	if reply[1] != 0x00 {
		return errors.New("SOCKS5 proxy rejected the configured username/password")
	}
	return nil
}

func socks5ConnectRequest(host string, port int) ([]byte, error) {
	request := []byte{socks5Version, socks5CmdConnect, 0x00}
	switch {
	case net.ParseIP(host) != nil && strings.Contains(host, ":"):
		ip := net.ParseIP(host)
		if ip.To4() != nil {
			request = append(request, socks5AtypIPv4)
			request = append(request, ip.To4()...)
		} else {
			request = append(request, socks5AtypIPv6)
			request = append(request, ip.To16()...)
		}
	case net.ParseIP(host) != nil:
		request = append(request, socks5AtypIPv4)
		request = append(request, net.ParseIP(host).To4()...)
	default:
		if len(host) == 0 || len(host) > 255 || strings.ContainsAny(host, " \t\r\n") {
			return nil, errors.New("SOCKS5 target host must be a non-empty hostname of at most 255 bytes")
		}
		request = append(request, socks5AtypDomain, byte(len(host)))
		request = append(request, host...)
	}
	request = binary.BigEndian.AppendUint16(request, uint16(port))
	return request, nil
}

func splitDialAddress(address string) (string, int, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", 0, fmt.Errorf("invalid target address: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, errors.New("invalid target port")
	}
	if host == "" {
		return "", 0, errors.New("target host is empty")
	}
	return host, port, nil
}

func socks5ReplyMessage(code byte) string {
	switch code {
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return "unknown error code " + strconv.Itoa(int(code))
	}
}
