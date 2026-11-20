package panel

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestAPIRouteManifest(t *testing.T) {
	root := panelRepositoryRoot(t)
	files := []string{filepath.Join(root, "internal", "bootstrap", "panel", "app.go")}
	err := filepath.WalkDir(filepath.Join(root, "internal", "modules"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "routes.go" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	patterns := routePatterns(t, files)
	// Tailscale 功能整体移除，删除 3 条相关路由（197 → 194）：
	//   PUT /api/v1/settings/tailscale
	//   POST /api/v1/settings/tailscale/apply
	//   POST /api/v1/servers/{id}/tailscale/apply
	// 已删除两条手动防火墙入口（改用 Agent 部署时自动接管，见 AGT-FW-*）：
	//   POST /api/v1/servers/{id}/ufw/install
	//   POST /api/v1/servers/{id}/ufw/enable
	// 并删除了 NAT 服务器能力及其 4 条端口子资源路由（SRV-NAT-* 已废弃）：
	//   GET/POST /api/v1/servers/{id}/nat-ports
	//   PUT/DELETE /api/v1/servers/{id}/nat-ports/{mappingID}
	// 消费者（web/src/api/settings.ts、web/src/api/servers.ts、Mock 与验收条目
	// UFW-API-*）必须与本清单同步变化（COV-API-001）。
	// Events + logs product surface: /api/v1/events* + /api/v1/logs, with
	// short-lived /api/v1/log-contexts* aliases. See UI-EVT / UI-LOG / ENG-EVT.
	// 入口网关双平面：代理平面与配置记录。
	//   GET/PUT /api/v1/facility-apps/reverse-proxy/proxy-plane
	//   GET /api/v1/facility-apps/reverse-proxy/config-records
	//   GET/DELETE /api/v1/facility-apps/reverse-proxy/config-records/{recordId}
	//   POST /api/v1/facility-apps/reverse-proxy/config-records/{recordId}/apply
	// 网络探测设施（FAC-PRB-*）新增 4 条路由：
	//   GET/PUT /api/v1/facility-apps/network-probe
	//   GET /api/v1/facility-apps/network-probe/results
	//   GET /api/v1/servers/{id}/network-probes
	// 指标设施（FAC-MET-*）新增 2 条路由：
	//   GET/PUT /api/v1/facility-apps/metrics
	// 外观设置（SET-APP-*）新增 6 条路由（186 → 192）：
	//   GET/PUT /api/v1/settings/appearance
	//   POST/DELETE /api/v1/settings/appearance/background
	//   GET /api/v1/settings/appearance/background/content
	//   GET /api/v1/settings/public-appearance（skipAuth，仍计入 /api manifest）
	// 设置写入按分区拆分为子资源（SET-RUN-022，192 → 197）：删除整体
	//   PUT /api/v1/settings/runtime
	// 新增 5 条分区 PUT（自动保存只提交当前分区）：
	//   PUT /api/v1/settings/runtime/general
	//   PUT /api/v1/settings/runtime/security
	//   PUT /api/v1/settings/runtime/certificates
	//   PUT /api/v1/settings/runtime/agent
	//   PUT /api/v1/settings/runtime/branding
	// GET /api/v1/settings/runtime 不变。
	// 入口代理自动更新/每域名开关/自定义指令（2026-11-19，194 → 198）：
	//   PUT /api/v1/settings/runtime/auto-update（SET-RUN-023）
	//   PUT /api/v1/facility-apps/reverse-proxy/settings（FAC-RP-015）
	//   POST /api/v1/facility-apps/reverse-proxy/image/check（FAC-RP-014）
	//   POST /api/v1/facility-apps/reverse-proxy/image/update（FAC-RP-014）
	// 消费者（web/src/api/settings.ts、web/src/api/facilityApps.ts、Mock 与验收条目
	// SET-RUN-023/FAC-RP-014/015）必须与本清单同步（COV-API-001）。
	// 入口代理 P1（2026-11-19，198 → 201）：
	//   POST /api/v1/facility-apps/reverse-proxy/image/rollback（FAC-RP-014）
	//   POST /api/v1/facility-apps/reverse-proxy/image/resume（FAC-RP-014）
	//   POST /api/v1/settings/runtime/auto-update/run（SET-RUN-023）
	// 镜像仓库源设置（2026-11-19，201 → 202）：
	//   PUT /api/v1/settings/runtime/image-registry（SET-RUN-024）
	// 内存事件通知长轮询（ENG-EVT-011，2026-11-19，202 → 203）：
	//   GET /api/v1/notifications（auth；after/wait 长轮询，只读内存 hub，不读写数据库）
	const wantCount = 203
	const wantHash = "82079c044beaca07e85fe29f4a40597149dff6d29c338ed45ccac9a40b366e91"
	manifest := strings.Join(patterns, "\n") + "\n"
	gotHash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	if len(patterns) != wantCount || gotHash != wantHash {
		t.Fatalf("API route manifest changed: count=%d hash=%s\n%s", len(patterns), gotHash, manifest)
	}
}

// TestPublicArtifactRouteManifest guards the unauthenticated surface the Panel
// exposes outside /api. The API manifest cannot see those routes, so they get
// their own: adding, renaming or removing one must be reviewed here and
// reflected in the acceptance contracts that describe the agent bundle
// download.
func TestPublicArtifactRouteManifest(t *testing.T) {
	root := panelRepositoryRoot(t)
	files := []string{filepath.Join(root, "internal", "modules", "servers", "agent_download.go")}

	patterns := publicRoutePatterns(t, files)
	const wantCount = 2
	const wantHash = "e6010026b226bfdc767151c5e7f78637dd6e99592b2e72caf76fd34f66758cc5"
	manifest := strings.Join(patterns, "\n") + "\n"
	gotHash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	if len(patterns) != wantCount || gotHash != wantHash {
		t.Fatalf("public route manifest changed: count=%d hash=%s\n%s", len(patterns), gotHash, manifest)
	}

	// The artifact name must stay a literal in the pattern rather than a path
	// parameter: that is what keeps request data out of the filesystem, and the
	// /agent prefix is what keeps the route out of /api so CDN rules that bypass
	// caching for API traffic do not apply to the bundle.
	const wantArtifactPattern = "GET /agent/{version}/{platform}/panel-agent.gz"
	found := false
	for _, pattern := range patterns {
		if strings.Contains(pattern, "/api/") {
			t.Fatalf("public route %q must stay outside /api", pattern)
		}
		if pattern == wantArtifactPattern {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the fixed-literal artifact pattern %q, got %v", wantArtifactPattern, patterns)
	}
}

// publicRoutePatterns collects the literal route patterns that are not part of
// the /api surface.
func publicRoutePatterns(t *testing.T, files []string) []string {
	t.Helper()
	found := map[string]struct{}{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			pattern, ok := literalRoutePattern(node)
			if !ok || strings.Contains(pattern, "/api/") {
				return true
			}
			found[pattern] = struct{}{}
			return true
		})
	}
	patterns := make([]string, 0, len(found))
	for pattern := range found {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

// literalRoutePattern extracts the pattern argument of one mux.Handle or
// mux.HandleFunc call whose first argument is a string literal.
func literalRoutePattern(node ast.Node) (string, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	pattern, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return pattern, true
}

func routePatterns(t *testing.T, files []string) []string {
	t.Helper()
	found := map[string]struct{}{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil || !strings.HasPrefix(pattern, "GET /api/") &&
				!strings.HasPrefix(pattern, "POST /api/") &&
				!strings.HasPrefix(pattern, "PUT /api/") &&
				!strings.HasPrefix(pattern, "DELETE /api/") &&
				!strings.HasPrefix(pattern, "PATCH /api/") {
				return true
			}
			found[pattern] = struct{}{}
			return true
		})
	}
	patterns := make([]string, 0, len(found))
	for pattern := range found {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

func panelRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
