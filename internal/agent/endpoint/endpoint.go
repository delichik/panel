// Package endpoint 解析 Panel 实际使用的节点连接地址。
//
// 一个节点可能同时具备规范地址（LAN/公网）与 tailnet 地址。是否改用 tailnet
// 地址由节点自身的意图开关决定，但只有同时满足以下条件才生效：
//
//  1. 该节点已启用 tailscale；
//  2. 该节点（或互联链接的另一端）要求优先使用 tailnet 地址；
//  3. 相关节点都已上报位于 tailnet 网段内的有效地址；
//  4. Panel 自身已加入 tailnet——否则改用 tailnet 地址只会让节点失联。
//
// 任一条件不满足时一律回落到规范地址，保证开关不会把可达节点变成不可达。
package endpoint

import (
	"net"
	"net/url"
	"strings"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/tailscale"
)

// Preferences 是决定是否改用 tailnet 地址的节点意图。
type Preferences struct {
	// Enabled 表示该节点已启用 tailscale。
	Enabled bool
	// Prefer 表示该节点要求 Panel 优先使用 tailnet 地址连接其 agent。
	Prefer bool
}

// AgentURL 返回 Panel 连接该节点 agent 使用的地址。traits 中缺少规范地址时
// 返回空串，调用方按“未配置 Agent”处理。
func AgentURL(traits map[string]string, prefs Preferences) string {
	canonical := strings.TrimSpace(traits[agentcontract.TraitURL])
	if canonical == "" {
		return ""
	}
	if !prefs.Enabled || !prefs.Prefer {
		return canonical
	}
	// Panel 自身不在 tailnet 中时使用 tailnet 地址必然失败，因此保持规范地址。
	if !tailscale.PanelReady() {
		return canonical
	}
	address := TailnetAddress(traits)
	if address == "" {
		return canonical
	}
	return replaceHost(canonical, address)
}

// TailnetAddress 返回节点已上报且落在 tailnet 网段内的地址（IPv4 优先）。
func TailnetAddress(traits map[string]string) string {
	if traits == nil {
		return ""
	}
	return tailscale.FirstTailnetAddress(traits[agentcontract.TraitTailscaleIPv4], traits[agentcontract.TraitTailscaleIPv6])
}

// Node 是判断互联地址优先所需的最小节点事实。
type Node struct {
	// Enabled 表示该节点已启用 tailscale。
	Enabled bool
	// Prefer 表示该节点要求互联链接优先使用 tailnet 地址。
	Prefer bool
	// Traits 保存该节点已上报的 tailnet 地址。
	Traits map[string]string
}

// TailnetAddress 返回该节点的 tailnet 地址，没有则返回空串。
func (n Node) TailnetAddress() string { return TailnetAddress(n.Traits) }

// InterconnectHost 决定节点互联链接使用的对端主机地址。
//
// 链接两端都启用 tailscale、双方都有有效 tailnet 地址、且至少一端要求优先
// 使用 tailnet 地址时才替换；否则返回对端规范地址。
func InterconnectHost(canonicalHost string, local, peer Node) string {
	host := strings.TrimSpace(canonicalHost)
	if host == "" {
		return host
	}
	if !local.Enabled || !peer.Enabled {
		return host
	}
	if !local.Prefer && !peer.Prefer {
		return host
	}
	if local.TailnetAddress() == "" {
		// 本端没有 tailnet 地址时，对端无法把本端识别为 tailnet 来源
		// （NFS 导出白名单、ACL 都按来源地址判定），因此不能只改对端地址。
		return host
	}
	address := peer.TailnetAddress()
	if address == "" {
		return host
	}
	return address
}

// replaceHost 保留规范地址的 scheme 与端口，只替换主机部分。
func replaceHost(canonical, host string) string {
	parsed, err := url.Parse(canonical)
	if err != nil || parsed.Host == "" {
		return canonical
	}
	port := parsed.Port()
	if port == "" {
		return canonical
	}
	parsed.Host = net.JoinHostPort(host, port)
	return parsed.String()
}
