package facilityapps

import (
	"context"
	"testing"
)

// SRV-DEL-008: 节点删除事务已经修剪了入口代理的网关列表，设施必须按修剪后的
// 配置重新收敛，否则伪应用会继续把代理部署到已删除的节点上。
func TestReconcileAfterServerRemovedUsesPrunedGatewayList(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{DeploymentServers: []string{"srv-a", "srv-b"}}); err != nil {
		t.Fatal(err)
	}
	// 模拟服务器删除事务对设施配置的修剪。
	if _, err := svc.db.ExecContext(ctx, `UPDATE facility_app_configs SET deployment_server_ids_json=? WHERE id=?`, `["srv-b"]`, ReverseProxyID); err != nil {
		t.Fatal(err)
	}
	before := trigger.calls
	if err := svc.ReconcileAfterServerRemoved(ctx, "srv-a"); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != before+1 {
		t.Fatalf("reconcile calls = %d, want one more than %d", trigger.calls, before)
	}
	var targets string
	var enabled int
	if err := svc.db.QueryRowContext(ctx, `SELECT deployment_server_ids_json,enabled FROM applications WHERE id=?`, proxyApplicationID).Scan(&targets, &enabled); err != nil {
		t.Fatal(err)
	}
	if targets != `["srv-b"]` {
		t.Fatalf("facility proxy targets = %s, want the pruned gateway list", targets)
	}
	if enabled != 1 {
		t.Fatal("expected the facility proxy to stay enabled while a gateway remains")
	}
}

// SRV-DEL-008: 网关全部删除后入口代理必须停用，不能继续指向空节点列表。
func TestReconcileAfterServerRemovedDisablesProxyWithoutGateway(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `UPDATE facility_app_configs SET deployment_server_ids_json=? WHERE id=?`, `[]`, ReverseProxyID); err != nil {
		t.Fatal(err)
	}
	before := trigger.calls
	if err := svc.ReconcileAfterServerRemoved(ctx, "srv-a"); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != before+1 {
		t.Fatalf("reconcile calls = %d, want one more than %d", trigger.calls, before)
	}
	var targets string
	var enabled int
	if err := svc.db.QueryRowContext(ctx, `SELECT deployment_server_ids_json,enabled FROM applications WHERE id=?`, proxyApplicationID).Scan(&targets, &enabled); err != nil {
		t.Fatal(err)
	}
	if targets != `[]` {
		t.Fatalf("facility proxy targets = %s, want an empty gateway list", targets)
	}
	if enabled != 0 {
		t.Fatal("expected the facility proxy to be disabled without any gateway")
	}
}
