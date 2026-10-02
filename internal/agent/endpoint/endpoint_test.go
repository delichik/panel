package endpoint_test

import (
	"testing"

	agentcontract "panel/internal/agent/contract"
	agentendpoint "panel/internal/agent/endpoint"
	"panel/internal/platform/tailscale"
)

func traits(url, ipv4, ipv6 string) map[string]string {
	out := map[string]string{}
	if url != "" {
		out[agentcontract.TraitURL] = url
	}
	if ipv4 != "" {
		out[agentcontract.TraitTailscaleIPv4] = ipv4
	}
	if ipv6 != "" {
		out[agentcontract.TraitTailscaleIPv6] = ipv6
	}
	return out
}

// panelReady 把进程级 tailnet 可达性固定为给定值，并在用例结束后恢复默认。
func panelReady(t *testing.T, ready bool) {
	t.Helper()
	tailscale.SetPanelReady(ready)
	t.Cleanup(func() { tailscale.SetPanelReady(false) })
}

func TestAgentURLKeepsCanonicalAddressWithoutPreference(t *testing.T) {
	panelReady(t, true)
	const canonical = "https://203.0.113.10:9786"
	cases := map[string]agentendpoint.Preferences{
		"tailscale disabled":       {Enabled: false, Prefer: true},
		"preference off":           {Enabled: true, Prefer: false},
		"neither flag":             {},
	}
	for name, prefs := range cases {
		t.Run(name, func(t *testing.T) {
			if got := agentendpoint.AgentURL(traits(canonical, "100.64.0.5", ""), prefs); got != canonical {
				t.Fatalf("AgentURL = %q, want %q", got, canonical)
			}
		})
	}
}

func TestAgentURLRequiresPanelTailnetReadiness(t *testing.T) {
	panelReady(t, false)
	const canonical = "https://203.0.113.10:9786"
	got := agentendpoint.AgentURL(traits(canonical, "100.64.0.5", ""), agentendpoint.Preferences{Enabled: true, Prefer: true})
	if got != canonical {
		t.Fatalf("AgentURL = %q, want the canonical address while the panel is not on the tailnet", got)
	}
}

func TestAgentURLPrefersTailnetAddressAndKeepsPort(t *testing.T) {
	panelReady(t, true)
	cases := []struct {
		name      string
		canonical string
		ipv4      string
		ipv6      string
		want      string
	}{
		{name: "ipv4", canonical: "https://203.0.113.10:9786", ipv4: "100.64.0.5", want: "https://100.64.0.5:9786"},
		{name: "nat public port", canonical: "https://203.0.113.10:8443", ipv4: "100.64.0.5", want: "https://100.64.0.5:8443"},
		{name: "ipv6 fallback", canonical: "https://203.0.113.10:9786", ipv6: "fd7a:115c:a1e0::5", want: "https://[fd7a:115c:a1e0::5]:9786"},
		{name: "non tailnet address is ignored", canonical: "https://203.0.113.10:9786", ipv4: "192.168.1.5", want: "https://203.0.113.10:9786"},
		{name: "empty address falls back", canonical: "https://203.0.113.10:9786", want: "https://203.0.113.10:9786"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := agentendpoint.AgentURL(traits(tt.canonical, tt.ipv4, tt.ipv6), agentendpoint.Preferences{Enabled: true, Prefer: true})
			if got != tt.want {
				t.Fatalf("AgentURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentURLWithoutCanonicalURLStaysUnconfigured(t *testing.T) {
	panelReady(t, true)
	if got := agentendpoint.AgentURL(traits("", "100.64.0.5", ""), agentendpoint.Preferences{Enabled: true, Prefer: true}); got != "" {
		t.Fatalf("AgentURL = %q, want an empty endpoint when no agent URL is stored", got)
	}
}

func TestInterconnectHostRequiresBothEnds(t *testing.T) {
	peer := func(enabled, prefer bool, ipv4 string) agentendpoint.Node {
		return agentendpoint.Node{Enabled: enabled, Prefer: prefer, Traits: traits("", ipv4, "")}
	}
	const canonical = "203.0.113.10"
	cases := []struct {
		name  string
		local agentendpoint.Node
		peer  agentendpoint.Node
		want  string
	}{
		{
			name:  "both enabled and the peer prefers",
			local: peer(true, false, "100.64.0.9"),
			peer:  peer(true, true, "100.64.0.10"),
			want:  "100.64.0.10",
		},
		{
			name:  "both enabled and the local end prefers",
			local: peer(true, true, "100.64.0.9"),
			peer:  peer(true, false, "100.64.0.10"),
			want:  "100.64.0.10",
		},
		{
			name:  "neither end prefers",
			local: peer(true, false, "100.64.0.9"),
			peer:  peer(true, false, "100.64.0.10"),
			want:  canonical,
		},
		{
			name:  "peer has tailscale disabled",
			local: peer(true, true, "100.64.0.9"),
			peer:  peer(false, true, "100.64.0.10"),
			want:  canonical,
		},
		{
			name:  "local end has no tailnet address",
			local: peer(true, true, ""),
			peer:  peer(true, true, "100.64.0.10"),
			want:  canonical,
		},
		{
			name:  "peer has no tailnet address",
			local: peer(true, true, "100.64.0.9"),
			peer:  peer(true, true, ""),
			want:  canonical,
		},
		{
			name:  "peer address outside the tailnet range",
			local: peer(true, true, "100.64.0.9"),
			peer:  peer(true, true, "10.0.0.5"),
			want:  canonical,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentendpoint.InterconnectHost(canonical, tt.local, tt.peer); got != tt.want {
				t.Fatalf("InterconnectHost = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTailnetAddressPrefersIPv4(t *testing.T) {
	if got := agentendpoint.TailnetAddress(traits("", "100.64.0.5", "fd7a:115c:a1e0::5")); got != "100.64.0.5" {
		t.Fatalf("TailnetAddress = %q", got)
	}
	if got := agentendpoint.TailnetAddress(nil); got != "" {
		t.Fatalf("TailnetAddress(nil) = %q", got)
	}
}
