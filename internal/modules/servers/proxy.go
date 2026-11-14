package server

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"

	agentendpoint "panel/internal/agent/endpoint"
	panelerr "panel/internal/platform/errors"
	sshx "panel/internal/platform/ssh"
)

// 服务器连接代理配置（SRV-PROXY-*）。
//
// 这里同时承担三件事：
//  1. 保存时的合法性校验（模式、隧道存在性、自引用/成环/层数、SOCKS5 地址）；
//  2. 读取时派生 proxyInvalid/proxyError 与 tunnelServerName；
//  3. 作为 sshx.ProxyResolver 把配置展开成有界、无环的代理跳列表，并解密
//     SOCKS5 密码。
const (
	serverProxySecretType    = "server_proxy"
	serverProxyPasswordField = "socks5_password"
)

type serverProxySnapshot struct {
	mode                     string
	tunnelServerID           string
	socks5Address            string
	socks5Username           string
	socks5PasswordCiphertext string
}

// normalizeServerProxyMode 校验并归一化代理模式；空值等价于 direct。
func normalizeServerProxyMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", sshx.ProxyModeDirect:
		return sshx.ProxyModeDirect, nil
	case sshx.ProxyModeSSHTunnel:
		return sshx.ProxyModeSSHTunnel, nil
	case sshx.ProxyModeSocks5:
		return sshx.ProxyModeSocks5, nil
	default:
		return "", panelerr.Validation("server_proxy_mode_invalid", "Proxy mode must be direct, ssh_tunnel or socks5")
	}
}

// normalizeSocks5Address 校验并返回规范化后的 host:port。
func normalizeSocks5Address(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", panelerr.Validation("server_proxy_socks5_address_required", "SOCKS5 mode requires a proxy address")
	}
	if strings.Contains(value, "://") {
		return "", panelerr.Validation("server_proxy_socks5_address_invalid", "SOCKS5 address must be host:port")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", panelerr.Validation("server_proxy_socks5_address_invalid", "SOCKS5 address must be host:port")
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \t/") {
		return "", panelerr.Validation("server_proxy_socks5_address_invalid", "SOCKS5 address must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", panelerr.Validation("server_proxy_socks5_address_invalid", "SOCKS5 address must be host:port with a valid port")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// validateProxySave 校验创建/更新请求，并返回应持久化的代理快照。direct 会
// 清空全部代理字段与密码密文；socks5 密码留空表示保留 current 的密文。
func (s *Service) validateProxySave(ctx context.Context, serverID string, req SaveRequest, current *Server) (serverProxySnapshot, error) {
	mode, err := normalizeServerProxyMode(req.ProxyMode)
	if err != nil {
		return serverProxySnapshot{}, err
	}
	switch mode {
	case sshx.ProxyModeSSHTunnel:
		tunnelServerID := strings.TrimSpace(req.TunnelServerID)
		if tunnelServerID == "" {
			return serverProxySnapshot{}, panelerr.Validation("server_proxy_tunnel_required", "SSH tunnel mode requires a tunnel server")
		}
		if tunnelServerID == serverID {
			return serverProxySnapshot{}, panelerr.Validation("server_proxy_tunnel_self", "A server cannot use itself as its SSH tunnel")
		}
		if err := s.validateTunnelChain(ctx, serverID, tunnelServerID); err != nil {
			return serverProxySnapshot{}, err
		}
		return serverProxySnapshot{mode: mode, tunnelServerID: tunnelServerID}, nil
	case sshx.ProxyModeSocks5:
		address, err := normalizeSocks5Address(req.Socks5Address)
		if err != nil {
			return serverProxySnapshot{}, err
		}
		username := strings.TrimSpace(req.Socks5Username)
		password := req.Socks5Password
		if password != "" && username == "" {
			return serverProxySnapshot{}, panelerr.Validation("server_proxy_socks5_username_required", "SOCKS5 password requires a username")
		}
		ciphertext := ""
		if password != "" {
			if s.secrets == nil {
				return serverProxySnapshot{}, panelerr.Validation("server_proxy_password_unavailable", "SOCKS5 password storage is unavailable")
			}
			encrypted, err := s.secrets.Encrypt(serverID, serverProxySecretType, serverProxyPasswordField, []byte(password))
			if err != nil {
				return serverProxySnapshot{}, err
			}
			ciphertext = encrypted
		} else if current != nil && normalizeStoredProxyMode(current.ProxyMode) == sshx.ProxyModeSocks5 {
			ciphertext = current.Socks5PasswordCiphertext
		}
		return serverProxySnapshot{mode: mode, socks5Address: address, socks5Username: username, socks5PasswordCiphertext: ciphertext}, nil
	default:
		return serverProxySnapshot{mode: sshx.ProxyModeDirect}, nil
	}
}

// validateTunnelChain 沿 tunnel_server_id 链检查存在性、自引用/成环与层数。
// subjectID 是待保存（或正在探测）的服务器自身 ID，会被预置进 visited，从而
// 拦住 A→B→A 以及"探测草稿把自己选为隧道"的环。
func (s *Service) validateTunnelChain(ctx context.Context, subjectID, tunnelServerID string) error {
	visited := map[string]bool{}
	if subjectID != "" {
		visited[subjectID] = true
	}
	current := strings.TrimSpace(tunnelServerID)
	hops := 0
	for {
		if current == "" {
			return panelerr.Validation("server_proxy_tunnel_required", "SSH tunnel mode requires a tunnel server")
		}
		if visited[current] {
			return panelerr.Validation("server_proxy_cycle", "Server proxy chain contains a cycle")
		}
		visited[current] = true
		hops++
		if hops > sshx.MaxProxyChainHops {
			return panelerr.Validation("server_proxy_chain_too_deep", "Server proxy chain is too deep")
		}
		srv, err := s.repo.Get(ctx, current)
		if err != nil {
			if isNotFoundError(err) {
				return panelerr.Validation("server_proxy_tunnel_missing", "The selected tunnel server does not exist")
			}
			return err
		}
		mode := normalizeStoredProxyMode(srv.ProxyMode)
		switch mode {
		case sshx.ProxyModeSSHTunnel:
			current = strings.TrimSpace(srv.TunnelServerID)
		case sshx.ProxyModeSocks5:
			if _, err := normalizeSocks5Address(srv.Socks5Address); err != nil {
				return err
			}
			return nil
		default:
			return nil
		}
	}
}

// validateProxyProbe 校验探测请求中的代理草稿。探测不持久化，因此 SOCKS5
// 密码由请求显式提供；编辑已有服务器且密码留空时，解析阶段会复用密文。
func (s *Service) validateProxyProbe(ctx context.Context, req SaveRequest) error {
	mode, err := normalizeServerProxyMode(req.ProxyMode)
	if err != nil {
		return err
	}
	subjectID := strings.TrimSpace(req.ProbeServerID)
	switch mode {
	case sshx.ProxyModeSSHTunnel:
		tunnelServerID := strings.TrimSpace(req.TunnelServerID)
		if tunnelServerID == "" {
			return panelerr.Validation("server_proxy_tunnel_required", "SSH tunnel mode requires a tunnel server")
		}
		if subjectID != "" && tunnelServerID == subjectID {
			return panelerr.Validation("server_proxy_tunnel_self", "A server cannot use itself as its SSH tunnel")
		}
		return s.validateTunnelChain(ctx, subjectID, tunnelServerID)
	case sshx.ProxyModeSocks5:
		if _, err := normalizeSocks5Address(req.Socks5Address); err != nil {
			return err
		}
		if req.Socks5Password != "" && strings.TrimSpace(req.Socks5Username) == "" {
			return panelerr.Validation("server_proxy_socks5_username_required", "SOCKS5 password requires a username")
		}
		return nil
	default:
		return nil
	}
}

// ResolveProxyChain 实现 sshx.ProxyResolver。spec 是目标服务器自身的代理
// 配置；隧道服务器及其上游一律使用已持久化配置（含解密后的 SOCKS5 密码）。
func (s *Service) ResolveProxyChain(ctx context.Context, serverID string, spec sshx.ProxySpec) ([]sshx.ProxyHop, error) {
	mode, err := normalizeServerProxyMode(spec.Mode)
	if err != nil {
		return nil, err
	}
	switch mode {
	case sshx.ProxyModeSSHTunnel:
		tunnelServerID := strings.TrimSpace(spec.TunnelServerID)
		if tunnelServerID == "" && serverID != "" {
			stored, err := s.repo.Get(ctx, serverID)
			if err != nil && !isNotFoundError(err) {
				return nil, err
			}
			if err == nil {
				tunnelServerID = strings.TrimSpace(stored.TunnelServerID)
			}
		}
		visited := map[string]bool{}
		if serverID != "" {
			visited[serverID] = true
		}
		return s.resolveTunnelChain(ctx, tunnelServerID, visited)
	case sshx.ProxyModeSocks5:
		address, err := normalizeSocks5Address(spec.Socks5Address)
		if err != nil {
			return nil, err
		}
		password := spec.Socks5Password
		if password == "" && serverID != "" {
			stored, err := s.repo.Get(ctx, serverID)
			// 探测请求的 serverId 可能悬空/非法：按“没有历史密文”处理，
			// 不把探测失败变成 404/500，也不读取其他服务器的密文。
			if err != nil && !isNotFoundError(err) {
				return nil, err
			}
			if err == nil {
				password, err = s.decryptSocks5Password(stored)
				if err != nil {
					return nil, err
				}
			}
		}
		return []sshx.ProxyHop{{
			Kind:           sshx.ProxyHopKindSocks5,
			Socks5Address:  address,
			Socks5Username: strings.TrimSpace(spec.Socks5Username),
			Socks5Password: password,
		}}, nil
	default:
		return nil, nil
	}
}

// resolveTunnelChain 返回“到达该隧道服务器”的完整跳列表，并把该服务器自身的
// SSH hop 追加在最后。visited 跨层共享以阻断环。
func (s *Service) resolveTunnelChain(ctx context.Context, tunnelServerID string, visited map[string]bool) ([]sshx.ProxyHop, error) {
	if strings.TrimSpace(tunnelServerID) == "" {
		return nil, panelerr.Validation("server_proxy_tunnel_required", "SSH tunnel mode requires a tunnel server")
	}
	if visited[tunnelServerID] {
		return nil, panelerr.Validation("server_proxy_cycle", "Server proxy chain contains a cycle")
	}
	visited[tunnelServerID] = true
	// 防御性上限：即使持久化数据被手工改坏，也不允许无限递归。
	if len(visited) > sshx.MaxProxyChainHops+1 {
		return nil, panelerr.Validation("server_proxy_chain_too_deep", "Server proxy chain is too deep")
	}
	srv, err := s.repo.Get(ctx, tunnelServerID)
	if err != nil {
		if isNotFoundError(err) {
			return nil, panelerr.Validation("server_proxy_tunnel_missing", "The selected tunnel server does not exist")
		}
		return nil, err
	}
	var hops []sshx.ProxyHop
	switch normalizeStoredProxyMode(srv.ProxyMode) {
	case sshx.ProxyModeSSHTunnel:
		upstream, err := s.resolveTunnelChain(ctx, strings.TrimSpace(srv.TunnelServerID), visited)
		if err != nil {
			return nil, err
		}
		hops = append(hops, upstream...)
	case sshx.ProxyModeSocks5:
		address, err := normalizeSocks5Address(srv.Socks5Address)
		if err != nil {
			return nil, err
		}
		password, err := s.decryptSocks5Password(srv)
		if err != nil {
			return nil, err
		}
		hops = append(hops, sshx.ProxyHop{
			Kind:           sshx.ProxyHopKindSocks5,
			Socks5Address:  address,
			Socks5Username: srv.Socks5Username,
			Socks5Password: password,
		})
	}
	hops = append(hops, sshx.ProxyHop{
		Kind:         sshx.ProxyHopKindSSH,
		Host:         srv.Host,
		Port:         srv.Port,
		Username:     srv.SSHUsername,
		CredentialID: srv.CredentialID,
	})
	if len(hops) > sshx.MaxProxyChainHops {
		return nil, panelerr.Validation("server_proxy_chain_too_deep", "Server proxy chain is too deep")
	}
	return hops, nil
}

func (s *Service) decryptSocks5Password(srv Server) (string, error) {
	ciphertext := strings.TrimSpace(srv.Socks5PasswordCiphertext)
	if ciphertext == "" {
		return "", nil
	}
	if s.secrets == nil {
		return "", panelerr.Validation("server_proxy_password_unavailable", "SOCKS5 password storage is unavailable")
	}
	raw, err := s.secrets.Decrypt(srv.ID, serverProxySecretType, serverProxyPasswordField, ciphertext)
	if err != nil {
		return "", panelerr.Validation("server_proxy_password_unavailable", "The stored SOCKS5 password is unavailable")
	}
	return string(raw), nil
}

// normalizeStoredProxyMode 归一化持久化列：未知值按 direct 处理，避免旧数据/
// 手工改坏的数据让读取路径一直失败。
func normalizeStoredProxyMode(value string) string {
	mode, err := normalizeServerProxyMode(value)
	if err != nil {
		return sshx.ProxyModeDirect
	}
	return mode
}

// decorateServerProxy 为单台服务器填充派生代理状态。非当前模式的残留字段
// 一律在读取视图里清空，保证 direct 视图不会暴露无关的代理配置。
func (s *Service) decorateServerProxy(srv *Server, all map[string]Server) {
	mode := normalizeStoredProxyMode(srv.ProxyMode)
	srv.ProxyMode = mode
	srv.TunnelServerName = ""
	srv.ProxyInvalid = false
	srv.ProxyError = ""
	srv.Socks5PasswordSet = mode == sshx.ProxyModeSocks5 && strings.TrimSpace(srv.Socks5PasswordCiphertext) != ""
	switch mode {
	case sshx.ProxyModeSSHTunnel:
		srv.Socks5Address = ""
		srv.Socks5Username = ""
		if tunnel, ok := all[strings.TrimSpace(srv.TunnelServerID)]; ok {
			srv.TunnelServerName = tunnel.Name
		}
		if code := evaluateProxyInvalid(srv, all); code != "" {
			srv.ProxyInvalid = true
			srv.ProxyError = code
		}
	case sshx.ProxyModeSocks5:
		srv.TunnelServerID = ""
		if code := socks5AddressInvalidCode(srv.Socks5Address); code != "" {
			srv.ProxyInvalid = true
			srv.ProxyError = code
		}
	default:
		srv.TunnelServerID = ""
		srv.Socks5Address = ""
		srv.Socks5Username = ""
	}
}

// decorateSummaryProxy 为列表摘要填充派生代理状态。
func (s *Service) decorateSummaryProxy(item *ServerSummary, all map[string]Server) {
	mode := normalizeStoredProxyMode(item.ProxyMode)
	item.ProxyMode = mode
	item.TunnelServerName = ""
	item.ProxyInvalid = false
	item.ProxyError = ""
	switch mode {
	case sshx.ProxyModeSSHTunnel:
		item.Socks5Address = ""
		item.Socks5Username = ""
		if tunnel, ok := all[strings.TrimSpace(item.TunnelServerID)]; ok {
			item.TunnelServerName = tunnel.Name
		}
		if code := evaluateProxyInvalid(&Server{
			ID:             item.ID,
			ProxyMode:      mode,
			TunnelServerID: item.TunnelServerID,
		}, all); code != "" {
			item.ProxyInvalid = true
			item.ProxyError = code
		}
	case sshx.ProxyModeSocks5:
		item.TunnelServerID = ""
		if code := socks5AddressInvalidCode(item.Socks5Address); code != "" {
			item.ProxyInvalid = true
			item.ProxyError = code
		}
	default:
		item.TunnelServerID = ""
		item.Socks5Address = ""
		item.Socks5Username = ""
	}
}

// evaluateProxyInvalid 返回 SSH 隧道链的结构性问题稳定码；空串表示有效。
// 判定只依赖引用关系，不拨号。
func evaluateProxyInvalid(srv *Server, all map[string]Server) string {
	visited := map[string]bool{srv.ID: true}
	current := strings.TrimSpace(srv.TunnelServerID)
	hops := 0
	for {
		if current == "" {
			return "tunnel_missing"
		}
		if visited[current] {
			return "cycle"
		}
		visited[current] = true
		hops++
		if hops > sshx.MaxProxyChainHops {
			return "chain_too_deep"
		}
		next, ok := all[current]
		if !ok {
			return "tunnel_missing"
		}
		switch normalizeStoredProxyMode(next.ProxyMode) {
		case sshx.ProxyModeSSHTunnel:
			current = strings.TrimSpace(next.TunnelServerID)
		case sshx.ProxyModeSocks5:
			return socks5AddressInvalidCode(next.Socks5Address)
		default:
			return ""
		}
	}
}

func socks5AddressInvalidCode(value string) string {
	_, err := normalizeSocks5Address(value)
	if err == nil {
		return ""
	}
	if pe, ok := err.(*panelerr.Error); ok && pe.Code == "server_proxy_socks5_address_required" {
		return "socks5_address_missing"
	}
	return "socks5_address_invalid"
}

// proxyDialExecutor 是服务器服务要求执行器具备的代理拨号能力。测试替身或旧
// 执行器不具备该能力时，agent 连接回落为直连（保持既有行为）。
type proxyDialExecutor interface {
	DialThroughProxy(ctx context.Context, network, address, serverID string, spec sshx.ProxySpec) (net.Conn, error)
}

// DialContext 让 agent gRPC 客户端按目标 agent 地址匹配服务器并走其代理链。
// 匹配不到时回落直连，确保未知端点/旧数据行为不变。
func (s *Service) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer, supported := s.exec.(proxyDialExecutor)
	if !supported || s.exec == nil || !s.proxyInUse() {
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}
	srv, ok := s.agentServerForAddress(ctx, address)
	if !ok || normalizeStoredProxyMode(srv.ProxyMode) == sshx.ProxyModeDirect {
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}
	return dialer.DialThroughProxy(ctx, network, address, srv.ID, sshx.ProxySpec{
		Mode:           srv.ProxyMode,
		TunnelServerID: srv.TunnelServerID,
		Socks5Address:  srv.Socks5Address,
		Socks5Username: srv.Socks5Username,
	})
}

// proxyInUse 返回是否存在任何非直连服务器。首次调用做一次性数据库检测；
// 之后保存非直连代理会直接置位。没有代理配置时 agent 拨号保持零额外查询。
func (s *Service) proxyInUse() bool {
	if s.proxyConfigured.Load() {
		return true
	}
	s.proxyConfiguredCheck.Do(func() {
		var count int
		err := s.db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM servers WHERE COALESCE(proxy_mode,'direct') IN (?,?)`,
			sshx.ProxyModeSSHTunnel, sshx.ProxyModeSocks5).Scan(&count)
		s.proxyConfigured.Store(err == nil && count > 0)
	})
	return s.proxyConfigured.Load()
}

// noteProxyConfigured 在保存非直连配置后打开快速开关。
func (s *Service) noteProxyConfigured(mode string) {
	if mode == sshx.ProxyModeSSHTunnel || mode == sshx.ProxyModeSocks5 {
		s.proxyConfigured.Store(true)
	}
}

// agentServerForAddress 按 agent 连接地址反查服务器。agent 地址由 traits 派生，
// 因此这里与 agentendpoint.AgentURL 使用同一套地址选择规则（含 tailnet 优先）。
func (s *Service) agentServerForAddress(ctx context.Context, address string) (Server, bool) {
	normalized := normalizeDialAddress(address)
	servers, err := s.repo.List(ctx)
	if err != nil {
		return Server{}, false
	}
	for _, srv := range servers {
		endpoint := agentendpoint.AgentURL(srv.Traits, agentendpoint.Preferences{
			Enabled: srv.TailscaleEnabled,
			Prefer:  srv.TailscalePreferAgent,
		})
		if endpoint == "" {
			continue
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Host == "" {
			continue
		}
		if normalizeDialAddress(parsed.Host) == normalized {
			return srv, true
		}
	}
	return Server{}, false
}

func normalizeDialAddress(value string) string {
	value = strings.TrimSpace(value)
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return strings.ToLower(value)
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	}
	return strings.ToLower(net.JoinHostPort(host, port))
}
