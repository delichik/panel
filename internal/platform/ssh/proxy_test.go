package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	panelerr "panel/internal/platform/errors"
)

// 这些测试用真实的 TCP/SSH/SOCKS5 握手覆盖代理拨号链，避免只测结构而不测
// 线路行为（RFC 1928/1929 与 SSH direct-tcpip）。

type echoTCPServer struct {
	listener net.Listener
}

func newEchoTCPServer(t *testing.T) *echoTCPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &echoTCPServer{listener: listener}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *echoTCPServer) address() string { return s.listener.Addr().String() }

type socks5TestProxy struct {
	listener  net.Listener
	require   bool
	username  string
	password  string
	rejectAll bool
}

func newSocks5TestProxy(t *testing.T, requireAuth bool, username, password string) *socks5TestProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &socks5TestProxy{listener: listener, require: requireAuth, username: username, password: password}
	go proxy.serve()
	t.Cleanup(func() { _ = listener.Close() })
	return proxy
}

func (p *socks5TestProxy) address() string { return p.listener.Addr().String() }

func (p *socks5TestProxy) serve() {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return
		}
		go p.handle(conn)
	}
}

func (p *socks5TestProxy) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	// greeting
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != socks5Version {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	chosen := byte(socks5AuthNone)
	if p.require {
		chosen = socks5AuthUser
	}
	if _, err := conn.Write([]byte{socks5Version, chosen}); err != nil {
		return
	}
	if chosen == socks5AuthUser {
		authHead := make([]byte, 2)
		if _, err := io.ReadFull(conn, authHead); err != nil || authHead[0] != 0x01 {
			return
		}
		user := make([]byte, int(authHead[1]))
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		passLen := make([]byte, 1)
		if _, err := io.ReadFull(conn, passLen); err != nil {
			return
		}
		pass := make([]byte, int(passLen[0]))
		if _, err := io.ReadFull(conn, pass); err != nil {
			return
		}
		if string(user) != p.username || string(pass) != p.password {
			_, _ = conn.Write([]byte{0x01, 0x01})
			return
		}
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	}
	// connect request
	requestHead := make([]byte, 4)
	if _, err := io.ReadFull(conn, requestHead); err != nil || requestHead[0] != socks5Version || requestHead[1] != socks5CmdConnect {
		return
	}
	var host string
	switch requestHead[3] {
	case socks5AtypIPv4:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	case socks5AtypIPv6:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	case socks5AtypDomain:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		buf := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = string(buf)
	default:
		return
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return
	}
	port := int(binary.BigEndian.Uint16(portBuf))
	if p.rejectAll {
		_, _ = conn.Write([]byte{socks5Version, 0x05, 0x00, socks5AtypIPv4, 0, 0, 0, 0, 0, 0})
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_, _ = conn.Write([]byte{socks5Version, 0x05, 0x00, socks5AtypIPv4, 0, 0, 0, 0, 0, 0})
		return
	}
	defer target.Close()
	if _, err := conn.Write([]byte{socks5Version, 0x00, 0x00, socks5AtypIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	go func() { _, _ = io.Copy(target, conn) }()
	_, _ = io.Copy(conn, target)
}

type staticCredentialResolver struct {
	credential ResolvedCredential
}

func (r staticCredentialResolver) Resolve(context.Context, string) (ResolvedCredential, error) {
	return r.credential, nil
}

type staticProxyResolver struct {
	hops []ProxyHop
	err  error
}

func (r staticProxyResolver) ResolveProxyChain(context.Context, string, ProxySpec) ([]ProxyHop, error) {
	return r.hops, r.err
}

// sshTestServer 是最小 SSH 服务器：密码认证 + direct-tcpip 端口转发。
type sshTestServer struct {
	listener net.Listener
	port     int
}

func newSSHTestServer(t *testing.T) *sshTestServer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &sshTestServer{listener: listener, port: listener.Addr().(*net.TCPAddr).Port}
	go server.serve(config)
	t.Cleanup(func() { _ = listener.Close() })
	return server
}

func (s *sshTestServer) serve(config *ssh.ServerConfig) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
			if err != nil {
				return
			}
			defer serverConn.Close()
			go ssh.DiscardRequests(reqs)
			for newChannel := range chans {
				if newChannel.ChannelType() != "direct-tcpip" {
					_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported")
					continue
				}
				var payload struct {
					Host       string
					Port       uint32
					OriginHost string
					OriginPort uint32
				}
				if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
					_ = newChannel.Reject(ssh.ConnectionFailed, "bad payload")
					continue
				}
				target, err := net.Dial("tcp", net.JoinHostPort(payload.Host, strconv.Itoa(int(payload.Port))))
				if err != nil {
					_ = newChannel.Reject(ssh.ConnectionFailed, "dial failed")
					continue
				}
				channel, channelReqs, err := newChannel.Accept()
				if err != nil {
					_ = target.Close()
					continue
				}
				go ssh.DiscardRequests(channelReqs)
				go func() {
					_, _ = io.Copy(channel, target)
					_ = channel.Close()
				}()
				go func() {
					_, _ = io.Copy(target, channel)
					_ = target.Close()
				}()
			}
		}()
	}
}

func newProxyTestExecutor(t *testing.T, hops []ProxyHop, resolverErr error) *SSHExecutor {
	t.Helper()
	executor := NewSSHExecutorWithOptions(staticCredentialResolver{credential: ResolvedCredential{
		ID: "cred", Type: CredentialTypePassword, Username: "du", Password: "pw",
	}}, 5*time.Second, WithKnownHosts(""))
	executor.SetProxyResolver(staticProxyResolver{hops: hops, err: resolverErr})
	return executor
}

func roundTrip(t *testing.T, conn net.Conn, payload string) {
	t.Helper()
	// SSH 转发通道（tcpChan）不支持 SetDeadline，用外部超时守护即可。
	done := make(chan error, 1)
	go func() {
		if _, err := conn.Write([]byte(payload)); err != nil {
			done <- err
			return
		}
		buffer := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, buffer); err != nil {
			done <- err
			return
		}
		if string(buffer) != payload {
			done <- errors.New("echo mismatch: got " + string(buffer) + ", want " + payload)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for echo response")
	}
}

func proxyErrorCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var domain *panelerr.Error
	if !errors.As(err, &domain) {
		t.Fatalf("expected typed panel error, got %T: %v", err, err)
	}
	return domain.Code
}

func TestDialSocks5NoAuthRoundTrip(t *testing.T) {
	echo := newEchoTCPServer(t)
	proxy := newSocks5TestProxy(t, false, "", "")
	conn, err := dialSocks5(context.Background(), directDial, proxy.address(), echo.address(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	roundTrip(t, conn, "no-auth")
}

func TestDialSocks5UsernamePasswordRoundTrip(t *testing.T) {
	echo := newEchoTCPServer(t)
	proxy := newSocks5TestProxy(t, true, "proxy-user", "proxy-pass")
	conn, err := dialSocks5(context.Background(), directDial, proxy.address(), echo.address(), "proxy-user", "proxy-pass")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	roundTrip(t, conn, "with-auth")

	if _, err := dialSocks5(context.Background(), directDial, proxy.address(), echo.address(), "proxy-user", "wrong"); err == nil {
		t.Fatal("expected SOCKS5 authentication failure")
	}
}

func TestSSHExecutorDialThroughProxySocks5ThenSSH(t *testing.T) {
	echo := newEchoTCPServer(t)
	sshServer := newSSHTestServer(t)
	proxy := newSocks5TestProxy(t, false, "", "")
	executor := newProxyTestExecutor(t, []ProxyHop{
		{Kind: ProxyHopKindSocks5, Socks5Address: proxy.address()},
		{Kind: ProxyHopKindSSH, Host: "127.0.0.1", Port: sshServer.port, Username: "du", CredentialID: "cred"},
	}, nil)

	conn, err := executor.DialThroughProxy(context.Background(), "tcp", echo.address(), "srv", ProxySpec{Mode: ProxyModeSSHTunnel, TunnelServerID: "tunnel"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	roundTrip(t, conn, "socks5-then-ssh")
}

func TestSSHExecutorDialThroughProxyMapsTransportFailure(t *testing.T) {
	proxy := newSocks5TestProxy(t, false, "", "")
	executor := newProxyTestExecutor(t, []ProxyHop{{Kind: ProxyHopKindSocks5, Socks5Address: proxy.address()}}, nil)
	_, err := executor.DialThroughProxy(context.Background(), "tcp", "127.0.0.1:1", "srv", ProxySpec{Mode: ProxyModeSocks5})
	if code := proxyErrorCode(t, err); code != "server_proxy_dial_failed" {
		t.Fatalf("expected server_proxy_dial_failed, got %s", code)
	}
}

func TestSSHExecutorRejectsTooManyProxyHops(t *testing.T) {
	hops := make([]ProxyHop, MaxProxyChainHops+1)
	for i := range hops {
		hops[i] = ProxyHop{Kind: ProxyHopKindSocks5, Socks5Address: "127.0.0.1:1"}
	}
	executor := newProxyTestExecutor(t, hops, nil)
	_, err := executor.DialThroughProxy(context.Background(), "tcp", "127.0.0.1:1", "srv", ProxySpec{Mode: ProxyModeSocks5})
	if code := proxyErrorCode(t, err); code != "server_proxy_chain_too_deep" {
		t.Fatalf("expected server_proxy_chain_too_deep, got %s", code)
	}
}

func TestSSHExecutorDirectModeIgnoresResolver(t *testing.T) {
	echo := newEchoTCPServer(t)
	executor := NewSSHExecutorWithOptions(staticCredentialResolver{}, 5*time.Second, WithKnownHosts(""))
	conn, err := executor.DialThroughProxy(context.Background(), "tcp", echo.address(), "srv", ProxySpec{Mode: ProxyModeDirect})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	roundTrip(t, conn, "direct")
}

func TestSocks5NegotiationRejectsUnsupportedVersion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte{0x04, 0x00})
	}()
	_, err = dialSocks5(context.Background(), directDial, listener.Addr().String(), "127.0.0.1:1", "", "")
	if err == nil {
		t.Fatal("expected unsupported SOCKS5 version error")
	}
}

func TestSocks5ConnectRequestEncodesDomainAndIPv6(t *testing.T) {
	domain, err := socks5ConnectRequest("proxy.example.test", 1080)
	if err != nil {
		t.Fatal(err)
	}
	if domain[3] != socks5AtypDomain || string(domain[5:5+len("proxy.example.test")]) != "proxy.example.test" {
		t.Fatalf("unexpected domain request: %v", domain)
	}
	ipv6, err := socks5ConnectRequest("2001:db8::5", 443)
	if err != nil {
		t.Fatal(err)
	}
	if ipv6[3] != socks5AtypIPv6 {
		t.Fatalf("unexpected ipv6 request: %v", ipv6)
	}
	if _, err := socks5ConnectRequest("bad host with spaces", 80); err == nil {
		t.Fatal("expected domain length/content validation")
	}
}

var _ = sync.Mutex{}
