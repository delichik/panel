package applications

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFacilityApplicationNeverExposesFacilityRoutesAsAppRules 回归测试：
// 反向代理设施应用（facility-reverse-proxy）的域名路由统一存放在
// reverse_proxy_routes（app_id='facility-reverse-proxy'），但这些路由属于设施
// 模块管理，target_port 恒为 0（设施 Path 用 ruleType/静态/代理 URL，没有应用
// 目标端口概念）。应用模块读取设施应用时不得把它们当作应用反向代理规则加载，
// 否则 prepareDeploy → refreshApplicationSnapshot → normalizeReverseProxyRules
// 会以 “reverse proxy target port must be between 1 and 65535” 拒绝设施应用
// 的部署规划，导致保存应用/保存设施/手动协调全部报错。
func TestFacilityApplicationNeverExposesFacilityRoutesAsAppRules(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id,version,kind,name,enabled,deletion_requested,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,image_reference,job_id,namespace,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"facility-reverse-proxy", 1, ApplicationKindFacility, "facility-reverse-proxy", 1, 0,
		"kind: facility/reverse-proxy\nname: entrance-gateway\nimage: nginx:1.27-alpine\n",
		DeploymentModeSelected, `["srv-a"]`, 1, "facility-hash", "nginx:1.27-alpine", "facility-reverse-proxy", "facility", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO reverse_proxy_routes(domain,app_id,origin_server_ids,any_access_json,target_type,target_port,paths_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"gateway.example.test", "facility-reverse-proxy", `["srv-a"]`, `{}`, "", 0,
		`[{"path":"/","ruleType":"static","assetName":"index"}]`, now, now); err != nil {
		t.Fatal(err)
	}

	// 设施应用的 Get 不得把设施路由暴露为应用反向代理规则。
	app, err := svc.Get(ctx, "facility-reverse-proxy")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.ReverseProxy) != 0 {
		t.Fatalf("facility app must not expose facility routes as application rules, got %#v", app.ReverseProxy)
	}

	// ListForReconcile 同样不得携带设施路由（drift 检测只读状态字段）。
	apps, err := svc.ListForReconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range apps {
		if item.ID == "facility-reverse-proxy" && len(item.ReverseProxy) != 0 {
			t.Fatalf("ListForReconcile must not carry facility routes for the facility app, got %#v", item.ReverseProxy)
		}
	}

	// 规划设施应用部署（保存设施、手动协调、应用变更触发的路径）必须成功，
	// 不得因设施路由 target_port=0 触发端口校验错误。
	if _, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID: "facility-reverse-proxy",
		ServerIDs:     []string{"srv-a"},
		Force:         true,
		Manual:        true,
		TriggerType:   "facility_app",
	}); err != nil {
		t.Fatalf("facility planning must not fail on facility routes: %v", err)
	}
}

// TestUserApplicationStillLoadsReverseProxyRoutes 确保修复不改变普通应用行为：
// 用户应用的 reverse_proxy_routes 仍然作为应用反向代理规则加载。
func TestUserApplicationStillLoadsReverseProxyRoutes(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id,version,kind,name,enabled,deletion_requested,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,image_reference,job_id,namespace,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"app-user", 1, ApplicationKindUser, "user-app", 1, 0,
		"name: user-app\nimage: nginx:alpine\n", DeploymentModeAll, `[]`, 1, "user-hash", "nginx:alpine", "app-user", "default", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO reverse_proxy_routes(domain,app_id,origin_server_ids,any_access_json,target_type,target_port,paths_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		"api.example.test", "app-user", `["srv-a"]`, `{}`, "", 8317, `[{"path":"/"}]`, now, now); err != nil {
		t.Fatal(err)
	}
	app, err := svc.Get(ctx, "app-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(app.ReverseProxy) != 1 {
		t.Fatalf("user app must keep its reverse proxy rules, got %#v", app.ReverseProxy)
	}
	rule := app.ReverseProxy[0]
	if !strings.EqualFold(rule.Domain, "api.example.test") || rule.TargetPort != 8317 {
		t.Fatalf("user reverse proxy rule = %#v, want domain api.example.test with targetPort 8317", rule)
	}
}
