package tailscale_test

import (
	"testing"

	"panel/internal/modules/backups"
	"panel/internal/platform/tailscale"
)

// TestInitTokenHeaderMatchesRestartContract 固定 Panel 侧重启客户端与 tailscale
// 控制面客户端使用同一鉴权头。panel-init 用同一个令牌服务两类控制请求；任意一侧
// 改变字面量都会让另一侧静默变成 401，因此这里显式约束相等。
func TestInitTokenHeaderMatchesRestartContract(t *testing.T) {
	if tailscale.InitTokenHeader != backups.InitRestartTokenHeader {
		t.Fatalf("tailscale auth header %q != restart auth header %q", tailscale.InitTokenHeader, backups.InitRestartTokenHeader)
	}
}
