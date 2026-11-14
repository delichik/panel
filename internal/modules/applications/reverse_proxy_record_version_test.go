package applications

import (
	"context"
	"database/sql"
	"testing"

	"panel/internal/platform/database/orm"
)

// applicationRouteRecordVersion reads the independent version of one
// application reverse-proxy configuration record (FAC-RP-001/FAC-RP-004:
// every domain row owns its own version, separate from the application user
// version).
func applicationRouteRecordVersion(t *testing.T, svc *Service, appID, domain string) int {
	t.Helper()
	var version int
	if err := svc.db.QueryRow(`SELECT version FROM reverse_proxy_routes WHERE app_id=? AND domain=?`, appID, domain).Scan(&version); err != nil {
		t.Fatalf("read reverse proxy record version for %s/%s: %v", appID, domain, err)
	}
	return version
}

func countApplicationRouteRecords(t *testing.T, svc *Service, appID string) int {
	t.Helper()
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM reverse_proxy_routes WHERE app_id=?`, appID).Scan(&count); err != nil {
		t.Fatalf("count reverse proxy records for %s: %v", appID, err)
	}
	return count
}

// TestApplicationSaveKeepsReverseProxyRecordVersionMonotonic 回归应用保存路径：
// 应用保存后其反代配置记录（reverse_proxy_routes 每域名一行）的 version 必须
// 单调递增，绝不能因为整应用替换而把已存在记录归零（FAC-RP-001：配置平面变更
// 不得把记录 version 打回旧值；FAC-RP-004：每条记录独立 CAS）。
func TestApplicationSaveKeepsReverseProxyRecordVersionMonotonic(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetReverseProxyPolicyProvider(fakeReverseProxyPolicyProvider{})

	routeA := ReverseProxyRule{Domain: "a.example.test", TargetPort: 8080, Paths: []ReverseProxyPath{{Path: "/"}}}
	base := SaveInput{
		Name:              "web",
		Enabled:           false,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
		ReverseProxy:      []ReverseProxyRule{routeA},
	}
	app, err := svc.Create(ctx, base)
	if err != nil {
		t.Fatalf("create application: %v", err)
	}
	if got := applicationRouteRecordVersion(t, svc, app.ID, "a.example.test"); got != 1 {
		t.Fatalf("new reverse proxy record version = %d, want 1", got)
	}

	// 添加第二条记录：a 已存在，必须从 1 递增到 2；b 是全新记录，从 1 开始。
	routeB := ReverseProxyRule{Domain: "b.example.test", TargetPort: 9090, Paths: []ReverseProxyPath{{Path: "/"}}}
	second := base
	second.ReverseProxy = []ReverseProxyRule{routeA, routeB}
	if _, err := svc.Update(ctx, app.ID, second); err != nil {
		t.Fatalf("update application adding a route: %v", err)
	}
	if got := applicationRouteRecordVersion(t, svc, app.ID, "a.example.test"); got != 2 {
		t.Fatalf("existing reverse proxy record version = %d after app save, want 2 (must not reset)", got)
	}
	if got := applicationRouteRecordVersion(t, svc, app.ID, "b.example.test"); got != 1 {
		t.Fatalf("new reverse proxy record version = %d, want 1", got)
	}

	// 修改保留记录的端口：version 继续递增，被移除的记录删除。
	routeAChanged := routeA
	routeAChanged.TargetPort = 8081
	third := base
	third.ReverseProxy = []ReverseProxyRule{routeAChanged}
	if _, err := svc.Update(ctx, app.ID, third); err != nil {
		t.Fatalf("update application modifying a route: %v", err)
	}
	if got := applicationRouteRecordVersion(t, svc, app.ID, "a.example.test"); got != 3 {
		t.Fatalf("reverse proxy record version = %d after modifying route, want 3", got)
	}
	if count := countApplicationRouteRecords(t, svc, app.ID); count != 1 {
		t.Fatalf("reverse proxy record count = %d after removing a route, want 1", count)
	}

	// 完全相同的保存不应触碰已有记录，version 保持不变。
	if _, err := svc.Update(ctx, app.ID, third); err != nil {
		t.Fatalf("no-op application save: %v", err)
	}
	if got := applicationRouteRecordVersion(t, svc, app.ID, "a.example.test"); got != 3 {
		t.Fatalf("reverse proxy record version = %d after no-op save, want 3", got)
	}
}

// TestReplaceApplicationReverseProxyRoutesKeepsVersionsMonotonic 直接回归整应用
// 替换函数：它在删除并重建记录行时，必须按 domain 继承旧 version 并 +1，而不是
// 把 version 重置为 0（记录归零会破坏记录级 CAS，与 FAC-RP-001/FAC-RP-004 冲突）。
func TestReplaceApplicationReverseProxyRoutesKeepsVersionsMonotonic(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	const appID = "app-record-versions"

	replace := func(rules []ReverseProxyRule) {
		t.Helper()
		if err := orm.WithTx(ctx, svc.db, func(tx *sql.Tx) error {
			return replaceApplicationReverseProxyRoutes(ctx, tx, appID, rules)
		}); err != nil {
			t.Fatalf("replace reverse proxy routes: %v", err)
		}
	}
	routeA := func(port int) ReverseProxyRule {
		return ReverseProxyRule{Domain: "a.example.test", TargetPort: port, Paths: []ReverseProxyPath{{Path: "/"}}}
	}
	routeB := ReverseProxyRule{Domain: "b.example.test", TargetPort: 9090, Paths: []ReverseProxyPath{{Path: "/"}}}

	replace([]ReverseProxyRule{routeA(8080)})
	if got := applicationRouteRecordVersion(t, svc, appID, "a.example.test"); got != 1 {
		t.Fatalf("created record version = %d, want 1", got)
	}

	replace([]ReverseProxyRule{routeA(8081), routeB})
	if got := applicationRouteRecordVersion(t, svc, appID, "a.example.test"); got != 2 {
		t.Fatalf("retained record version = %d, want 2", got)
	}
	if got := applicationRouteRecordVersion(t, svc, appID, "b.example.test"); got != 1 {
		t.Fatalf("added record version = %d, want 1", got)
	}

	replace([]ReverseProxyRule{routeA(8082)})
	if got := applicationRouteRecordVersion(t, svc, appID, "a.example.test"); got != 3 {
		t.Fatalf("retained record version = %d after replacement, want 3", got)
	}
	if count := countApplicationRouteRecords(t, svc, appID); count != 1 {
		t.Fatalf("record count = %d after removing b, want 1", count)
	}

	// 删除全部记录后再重新添加同一域名属于新记录，允许从 1 重新开始；删除本身
	// 不得留下旧行或陈旧 version。
	replace(nil)
	if count := countApplicationRouteRecords(t, svc, appID); count != 0 {
		t.Fatalf("record count = %d after clearing routes, want 0", count)
	}
	replace([]ReverseProxyRule{routeA(8083)})
	if got := applicationRouteRecordVersion(t, svc, appID, "a.example.test"); got != 1 {
		t.Fatalf("recreated record version = %d, want 1 for a brand new record", got)
	}
}
