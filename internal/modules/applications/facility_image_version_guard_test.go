package applications

import (
	"testing"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	controlplane "panel/internal/orchestrator"
)

// 入口代理镜像版本线守卫：期望 minor 从受管标签解析，实际 minor 从 Agent
// `nginx -v` 输出解析；空输出只告警不阻断（旧 Agent 升级窗口）。
func TestProxyImageVersionGuard(t *testing.T) {
	cases := []struct {
		name     string
		reference string
		version  string
		wantCode string
	}{
		{name: "match", reference: "nginx:1.28-alpine", version: "nginx version: nginx/1.28.3", wantCode: ""},
		{name: "cross minor", reference: "nginx:1.28-alpine", version: "nginx version: nginx/1.29.0", wantCode: "image_version_line_invalid"},
		{name: "empty version unverified", reference: "nginx:1.28-alpine", version: "", wantCode: ""},
		{name: "unparsable reference", reference: "custom-image", version: "nginx version: nginx/1.28.0", wantCode: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := agentcontract.RuntimeReconcileRequest{
				Spec:                appruntime.Spec{Image: tc.reference},
				ImageVersionCommand: []string{"nginx", "-v"},
			}
			response := agentcontract.RuntimeReconcileResponse{ImageVersion: tc.version}
			out := applyFacilityProxyImageVersionGuard(controlplane.ReconcileResponse{}, input, response)
			if out.ErrorCode != tc.wantCode {
				t.Fatalf("ErrorCode = %q, want %q", out.ErrorCode, tc.wantCode)
			}
		})
	}
}

func TestProxyImageMinorParsers(t *testing.T) {
	if got := proxyImageMinorFromReference("nginx:1.28-alpine"); got != "1.28" {
		t.Fatalf("reference minor = %q", got)
	}
	if got := proxyImageMinorFromReference("nginx:1.28.1-alpine"); got != "1.28" {
		t.Fatalf("reference patch minor = %q", got)
	}
	if got := proxyImageMinorFromVersionOutput("nginx version: nginx/1.28.1"); got != "1.28" {
		t.Fatalf("version minor = %q", got)
	}
	if got := proxyImageMinorFromVersionOutput("unexpected"); got != "" {
		t.Fatalf("unexpected version parsed = %q", got)
	}
}
