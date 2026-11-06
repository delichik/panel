package facilityapps

import (
	"context"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
)

// tailnetTraits 在既有 Agent trait 之上补充 tailnet 观测地址。
func tailnetTraits(host, ipv4 string) map[string]string {
	return map[string]string{
		agentcontract.TraitEnabled:          "true",
		agentcontract.TraitURL:              "https://" + host + ":9786",
		agentcontract.TraitTailscaleIPv4:    ipv4,
		agentcontract.TraitTailscaleStatus:  agentcontract.TailscaleStatusRunning,
		agentcontract.TraitTailscaleVersion: "1.80.0",
	}
}

// storageInterconnectFixture 搭建存储节点（srv-a）与应用节点（srv-b）都有
// tailnet 地址的场景，并按参数设置两端的开关。
func storageInterconnectFixture(t *testing.T, storageEnabled, storagePrefer, nodeEnabled, nodePrefer bool) (*Service, *fakeStorageAgent, *fakeServerProvider) {
	t.Helper()
	svc, servers, _, agent, _ := newStorageShareTestService(t)
	servers.servers[0].Host = "10.0.0.5"
	servers.servers[0].Traits = tailnetTraits("10.0.0.5", "100.64.0.5")
	servers.servers[0].TailscaleEnabled = storageEnabled
	servers.servers[0].TailscalePreferInterconnect = storagePrefer
	servers.servers[1].Host = "10.0.0.6"
	servers.servers[1].Traits = tailnetTraits("10.0.0.6", "100.64.0.6")
	servers.servers[1].TailscaleEnabled = nodeEnabled
	servers.servers[1].TailscalePreferInterconnect = nodePrefer
	return svc, agent, servers
}

func resolveStorageMountSource(t *testing.T, svc *Service, node server.Server) string {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	resolved, err := svc.ResolveStorageShareMounts(ctx,
		applications.Application{ID: "app-1", Name: "app-1"},
		node,
		[]appruntime.Mount{{Type: "storage_share", Source: "storage-share:srv-a", Target: "/data"}},
	)
	if err != nil {
		t.Fatalf("resolve storage mounts: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("mounts = %#v", resolved)
	}
	return resolved[0].Source
}

func TestStorageMountUsesTailnetAddressWhenEndsQualify(t *testing.T) {
	cases := []struct {
		name           string
		storageEnabled bool
		storagePrefer  bool
		nodeEnabled    bool
		nodePrefer     bool
		wantHost       string
	}{
		{name: "storage end prefers", storageEnabled: true, storagePrefer: true, nodeEnabled: true, wantHost: "100.64.0.5"},
		{name: "app node prefers", storageEnabled: true, nodeEnabled: true, nodePrefer: true, wantHost: "100.64.0.5"},
		{name: "neither end prefers", storageEnabled: true, nodeEnabled: true, wantHost: "10.0.0.5"},
		{name: "storage tailscale disabled", storagePrefer: true, nodeEnabled: true, nodePrefer: true, wantHost: "10.0.0.5"},
		{name: "app node tailscale disabled", storageEnabled: true, storagePrefer: true, nodeEnabled: false, nodePrefer: true, wantHost: "10.0.0.5"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, servers := storageInterconnectFixture(t, tt.storageEnabled, tt.storagePrefer, tt.nodeEnabled, tt.nodePrefer)
			source := resolveStorageMountSource(t, svc, servers.servers[1])
			if !strings.HasPrefix(source, tt.wantHost+":") {
				t.Fatalf("NFS source = %q, want the host %q", source, tt.wantHost)
			}
			if !strings.HasSuffix(source, "/srv/panel-storage/srv-a/srv-b/app-1") {
				t.Fatalf("NFS source = %q, want the partition path preserved", source)
			}
		})
	}
}

func TestStorageMountFallsBackWhenPeerAddressIsNotTailnet(t *testing.T) {
	svc, _, servers := storageInterconnectFixture(t, true, true, true, true)
	// 非 tailnet 网段的地址不得被当作互联地址。
	servers.servers[0].Traits[agentcontract.TraitTailscaleIPv4] = "10.0.0.99"
	source := resolveStorageMountSource(t, svc, servers.servers[1])
	if !strings.HasPrefix(source, "10.0.0.5:") {
		t.Fatalf("NFS source = %q, want the canonical host", source)
	}
}

func TestStorageExportWhitelistIncludesTailnetAddresses(t *testing.T) {
	svc, agent, _ := storageInterconnectFixture(t, true, false, true, false)
	if _, err := svc.SaveStorageShare(context.Background(), StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatalf("save storage share: %v", err)
	}
	if agent.configureCalls == 0 {
		t.Fatal("saving the facility must configure the export")
	}
	hosts := agent.configureAllowedHosts[len(agent.configureAllowedHosts)-1]
	// 启用 tailscale 的挂载方来源地址是它的 tailnet 地址，白名单必须包含该条目，
	// 否则导出策略会拒绝挂载。
	for _, want := range []string{"10.0.0.6", "100.64.0.6", "10.0.0.5", "100.64.0.5"} {
		if !containsString(hosts, want) {
			t.Fatalf("allowed hosts = %#v, want %q", hosts, want)
		}
	}
}

// TestReverseProxyUpstreamUsesTailnetAddressOnlyWhenBothQualify 覆盖反向代理
// 网关节点到源站节点的互联地址规则：与存储共享使用同一套判定。
func TestReverseProxyUpstreamUsesTailnetAddressOnlyWhenBothQualify(t *testing.T) {
	svc, _, servers := storageInterconnectFixture(t, true, false, true, false)
	gateway := servers.servers[1]
	gateway.ID = "srv-gateway"
	origin := servers.servers[0]

	relay, err := svc.buildProxyRelay(context.Background(), gateway.ID, "example.test", []string{origin.ID}, applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin})
	if err != nil {
		t.Fatalf("build relay: %v", err)
	}
	if got := relay.Servers[len(relay.Servers)-1].Host; got != "10.0.0.5" {
		t.Fatalf("upstream host = %q, want the canonical address when neither end prefers", got)
	}

	gateway.TailscalePreferInterconnect = true
	servers.servers[1] = gateway
	relay, err = svc.buildProxyRelay(context.Background(), gateway.ID, "example.test", []string{origin.ID}, applications.AnyAccessConfig{Enabled: true, Strategy: applications.AnyAccessStrategyRoundRobin})
	if err != nil {
		t.Fatalf("build relay: %v", err)
	}
	if got := relay.Servers[len(relay.Servers)-1].Host; got != "100.64.0.5" {
		t.Fatalf("upstream host = %q, want the origin tailnet address", got)
	}
}
