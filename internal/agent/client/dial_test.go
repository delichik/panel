package client

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

// SRV-PROXY-011：Panel→Agent 的 gRPC 拨号必须经过注入的拨号函数（生产环境由
// servers 服务按目标 agent 地址套用 SSH 隧道 / SOCKS5 代理链）。
func TestGRPCClientRPCUsesInjectedDialFunc(t *testing.T) {
	client, endpoint, stop := newPrepareRestartTestClient(t, &prepareRestartServerStub{})
	defer stop()

	var mu sync.Mutex
	var dialed []string
	client.SetDialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, network+" "+address)
		mu.Unlock()
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	})
	if err := client.PrepareRestart(context.Background(), endpoint); err != nil {
		t.Fatalf("PrepareRestart returned error: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) == 0 {
		t.Fatal("expected the injected dial func to be used for agent RPCs")
	}
	want := strings.TrimPrefix(endpoint, "https://")
	if dialed[0] != "tcp "+want {
		t.Fatalf("dialed %q, want tcp %q", dialed[0], want)
	}
}
