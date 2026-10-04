package tailscale

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NodeState 是 `tailscale status --json` 中 Panel 关心的部分。Panel 容器侧
// （panel-init）与节点侧 Agent 共用该解析，避免两侧对同一份输出产生不同理解。
type NodeState struct {
	BackendState string
	Version      string
	Hostname     string
	IPv4         string
	IPv6         string
}

// LoggedIn 报告节点已加入 tailnet 且后端处于运行状态。
func (s NodeState) LoggedIn() bool {
	return s.BackendState == BackendStateRunning
}

// 已知的 tailscaled 后端状态。Panel 用它们区分“需要密钥”“等待管理员批准”
// 与其他异常，避免把管理动作的失败统一压成一句无法诊断的文本。
const (
	BackendStateNoState         = "NoState"
	BackendStateNeedsLogin      = "NeedsLogin"
	BackendStateNeedsMachineAuth = "NeedsMachineAuth"
	BackendStateStopped         = "Stopped"
	BackendStateStarting        = "Starting"
	BackendStateRunning         = "Running"
)

// ParseStatusJSON 解析 `tailscale status --json` 输出。
//
// 自身地址只接受 tailnet 网段内的取值：节点可能同时报告 LAN 地址或 MagicDNS
// 名称，它们不能作为节点互联的连接地址。
func ParseStatusJSON(raw []byte) (NodeState, error) {
	var payload struct {
		BackendState string
		Version      string
		Self         *struct {
			HostName     string
			TailscaleIPs []string
		}
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return NodeState{}, fmt.Errorf("decode tailscale status: %w", err)
	}
	state := NodeState{
		BackendState: strings.TrimSpace(payload.BackendState),
		Version:      strings.TrimSpace(payload.Version),
	}
	if payload.Self != nil {
		state.Hostname = strings.TrimSpace(payload.Self.HostName)
		for _, value := range payload.Self.TailscaleIPs {
			if !IsTailnetAddress(value) {
				continue
			}
			if strings.Contains(value, ":") {
				if state.IPv6 == "" {
					state.IPv6 = value
				}
				continue
			}
			if state.IPv4 == "" {
				state.IPv4 = value
			}
		}
	}
	return state, nil
}
