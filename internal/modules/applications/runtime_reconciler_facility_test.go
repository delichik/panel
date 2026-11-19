package applications

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	controlplane "panel/internal/orchestrator"
)

// 入口代理设施 reload/recreate 判定（FAC-RP-008/009、方案 §4.2）：
// runtime_reconciler 必须把该实例已存的 runtime spec（由
// application_instances.runtime_spec_json 持久化）作为 current 传给设施
// PlanRuntimeUpdate，而不是空 spec。否则设施永远只能判 reload，镜像/端口
// 变化无法触发 recreate。本文件用与设施侧契约一致的假实现断言传参与分流。
type fakeFacilityRuntimeProvider struct {
	calls   int
	current []appruntime.Spec
	desired []appruntime.Spec
}

func (f *fakeFacilityRuntimeProvider) RuntimeSpecForServer(context.Context, Application, server.Server) (appruntime.Spec, bool, error) {
	return appruntime.Spec{}, false, nil
}

// PlanRuntimeUpdate 模拟设施契约：无 current（首装）或身份字段（镜像/端口）
// 变化 → recreate；仅配置内容变化 → reload。
func (f *fakeFacilityRuntimeProvider) PlanRuntimeUpdate(_ context.Context, _ Application, _ server.Server, current, desired appruntime.Spec) appruntime.UpdatePlan {
	f.calls++
	f.current = append(f.current, current)
	f.desired = append(f.desired, desired)
	if !runtimeSpecIdentityEqual(current, desired) {
		return appruntime.UpdatePlan{Mode: appruntime.UpdateModeRecreate, Reason: "identity_changed"}
	}
	return appruntime.UpdatePlan{
		Mode:     appruntime.UpdateModeReload,
		Reason:   "config_changed",
		Strategy: &appruntime.ReloadStrategy{ValidateCommand: []string{"nginx", "-t"}, ReloadCommand: []string{"nginx", "-s", "reload"}},
	}
}

func runtimeSpecIdentityEqual(current, desired appruntime.Spec) bool {
	if strings.TrimSpace(current.Image) == "" {
		return false
	}
	if current.Image != desired.Image || len(current.Ports) != len(desired.Ports) {
		return false
	}
	for i := range current.Ports {
		if current.Ports[i] != desired.Ports[i] {
			return false
		}
	}
	return true
}

type facilityReloadRecorder struct {
	*fakeRuntimeClient
	reloads []agentcontract.RuntimeReloadRequest
	resp    agentcontract.RuntimeReloadResponse
	err     error
}

func (f *facilityReloadRecorder) RuntimeReload(_ context.Context, _ string, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	f.reloads = append(f.reloads, req)
	if f.err != nil {
		return agentcontract.RuntimeReloadResponse{}, f.err
	}
	return f.resp, nil
}

func facilityProxyPorts() []appruntime.Port {
	return []appruntime.Port{
		{Label: "http", ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
		{Label: "https", ContainerPort: 443, HostPort: 443, Protocol: "tcp"},
	}
}

func seedStoredRuntimeSpec(t *testing.T, svc *Service, spec appruntime.Spec) {
	t.Helper()
	seedFacilityApplication(t, svc)
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		spec.InstanceID, spec.ApplicationID, "srv-a", spec.ContainerName, "", appruntime.DesiredRunning, appruntime.StatusRunning,
		string(raw), 1, "", now, now); err != nil {
		t.Fatalf("seed application_instances row: %v", err)
	}
}

// application_instances.application_id 外键指向 applications(id)，这里先落一行
// 入口代理设施应用，与设施模块 ensureReverseProxyApplication 的形态一致。
func seedFacilityApplication(t *testing.T, svc *Service) {
	t.Helper()
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM applications WHERE id=?`, FacilityProxyApplicationID).Scan(&count); err != nil {
		t.Fatalf("count facility application row: %v", err)
	}
	if count > 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`INSERT INTO applications(id,version,kind,name,enabled,deletion_requested,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,image_reference,job_id,namespace,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		FacilityProxyApplicationID, 1, ApplicationKindFacility, "facility-reverse-proxy", 1, 0,
		"kind: facility/reverse-proxy\nname: entrance-gateway\nimage: nginx:1.28-alpine\n",
		DeploymentModeSelected, `["srv-a"]`, 7, "facility-config-hash", "nginx:1.28-alpine", FacilityProxyApplicationID, "facility",
		now, now); err != nil {
		t.Fatalf("seed facility application row: %v", err)
	}
}

func facilityStoredSpec() appruntime.Spec {
	return appruntime.Spec{
		ApplicationID: FacilityProxyApplicationID,
		InstanceID:    "facility-instance",
		ContainerName: "panel-facility-reverse-proxy",
		Image:         "nginx:1.28-alpine",
		Ports:         facilityProxyPorts(),
		SpecHash:      "old-hash",
	}
}

func facilityApplyRequest(t *testing.T, desired appruntime.Spec) controlplane.ReconcileRequestRPC {
	t.Helper()
	raw, err := json.Marshal(desired)
	if err != nil {
		t.Fatal(err)
	}
	return controlplane.ReconcileRequestRPC{
		OperationID: "operation", RunID: "run", JobID: "job", ExecutionID: "execution",
		ApplicationID: FacilityProxyApplicationID, InstanceID: desired.InstanceID, ServerID: "srv-a",
		Action: controlplane.ActionApply, DesiredGeneration: 2, DesiredSpecHash: desired.SpecHash,
		RenderedRuntimeSpec: raw,
	}
}

// 纯配置内容变化（managed files/config 变了，镜像与端口不变）必须以非空 current
// 传给设施并走 reload。
func TestFacilityConfigReloadPassesStoredCurrentSpecAndReloads(t *testing.T) {
	svc, base, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	stored := facilityStoredSpec()
	seedStoredRuntimeSpec(t, svc, stored)
	desired := stored
	desired.SpecHash = "new-hash"
	desired.Files = []appruntime.ManagedFile{{Path: "/etc/panel-nginx/conf.d/app.example.test.conf", Content: []byte("server { listen 443 ssl; }")}}

	client := &facilityReloadRecorder{fakeRuntimeClient: base, resp: agentcontract.RuntimeReloadResponse{Reloaded: true, Phase: "applied"}}
	svc.runtimeClient = client
	provider := &fakeFacilityRuntimeProvider{}
	svc.SetFacilityRuntimeProvider(provider)

	reconciler := &serviceRuntimeReconciler{service: svc}
	response, ok, err := reconciler.tryFacilityConfigReload(ctx, "https://srv-a.agent", facilityApplyRequest(t, desired), desired)
	if err != nil {
		t.Fatalf("facility config reload: %v", err)
	}
	if !ok {
		t.Fatal("config-only change must be handled as reload, got fallthrough to reconcile")
	}
	if response.ObservedState != "running" {
		t.Fatalf("reload response = %#v", response)
	}
	if provider.calls != 1 {
		t.Fatalf("PlanRuntimeUpdate calls = %d, want 1", provider.calls)
	}
	// current 必须来自 application_instances.runtime_spec_json，不能是空 spec。
	if got := provider.current[0]; got.Image != stored.Image || len(got.Ports) != len(stored.Ports) {
		t.Fatalf("current spec passed to facility = %#v, want stored %#v", got, stored)
	}
	if provider.desired[0].SpecHash != "new-hash" {
		t.Fatalf("desired spec passed to facility = %#v", provider.desired[0])
	}
	if len(client.reloads) != 1 {
		t.Fatalf("RuntimeReload calls = %d, want 1", len(client.reloads))
	}
	reload := client.reloads[0]
	if len(reload.ValidateCommand) != 2 || reload.ValidateCommand[0] != "nginx" {
		t.Fatalf("reload validate command = %#v", reload.ValidateCommand)
	}
	if len(reload.ReloadCommand) != 3 || reload.ReloadCommand[0] != "nginx" {
		t.Fatalf("reload command = %#v", reload.ReloadCommand)
	}
	if reload.Spec.SpecHash != "new-hash" {
		t.Fatalf("reload spec hash = %q", reload.Spec.SpecHash)
	}
}

// 镜像变化 → 设施判 recreate；reconciler 不得走 reload，应回落到 RuntimeReconcile。
func TestFacilityConfigReloadFallsThroughOnImageChange(t *testing.T) {
	svc, base, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	stored := facilityStoredSpec()
	seedStoredRuntimeSpec(t, svc, stored)
	desired := stored
	desired.Image = "nginx:1.29-alpine"
	desired.SpecHash = "new-hash"

	client := &facilityReloadRecorder{fakeRuntimeClient: base, resp: agentcontract.RuntimeReloadResponse{Reloaded: true}}
	svc.runtimeClient = client
	provider := &fakeFacilityRuntimeProvider{}
	svc.SetFacilityRuntimeProvider(provider)

	reconciler := &serviceRuntimeReconciler{service: svc}
	_, ok, err := reconciler.tryFacilityConfigReload(ctx, "https://srv-a.agent", facilityApplyRequest(t, desired), desired)
	if err != nil {
		t.Fatalf("facility plan: %v", err)
	}
	if ok {
		t.Fatal("image identity change must fall through to RuntimeReconcile (recreate), not reload")
	}
	if len(client.reloads) != 0 {
		t.Fatalf("RuntimeReload calls = %d after image change, want 0", len(client.reloads))
	}
	if got := provider.current[0]; got.Image != stored.Image {
		t.Fatalf("current spec image = %q, want %q", got.Image, stored.Image)
	}
}

// 端口布局变化（例如新增 UDP 443）同样必须走 recreate，而不是 reload。
func TestFacilityConfigReloadFallsThroughOnPortLayoutChange(t *testing.T) {
	svc, base, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	stored := facilityStoredSpec()
	seedStoredRuntimeSpec(t, svc, stored)
	desired := stored
	desired.Ports = append(append([]appruntime.Port(nil), stored.Ports...), appruntime.Port{Label: "https-quic", ContainerPort: 443, HostPort: 443, Protocol: "udp"})
	desired.SpecHash = "new-hash"

	client := &facilityReloadRecorder{fakeRuntimeClient: base, resp: agentcontract.RuntimeReloadResponse{Reloaded: true}}
	svc.runtimeClient = client
	provider := &fakeFacilityRuntimeProvider{}
	svc.SetFacilityRuntimeProvider(provider)

	reconciler := &serviceRuntimeReconciler{service: svc}
	_, ok, err := reconciler.tryFacilityConfigReload(ctx, "https://srv-a.agent", facilityApplyRequest(t, desired), desired)
	if err != nil {
		t.Fatalf("facility plan: %v", err)
	}
	if ok {
		t.Fatal("port layout change must fall through to RuntimeReconcile (recreate), not reload")
	}
	if len(client.reloads) != 0 {
		t.Fatalf("RuntimeReload calls = %d after port change, want 0", len(client.reloads))
	}
	if got := len(provider.current[0].Ports); got != len(stored.Ports) {
		t.Fatalf("current spec ports = %#v, want %#v", provider.current[0].Ports, stored.Ports)
	}
}

// 首次安装没有已存 runtime spec：current 为零值，设施按“无 current”判 recreate，
// reconciler 回落到 RuntimeReconcile；但绝不能因此阻塞协调。
func TestFacilityConfigReloadWithoutStoredSpecFallsThrough(t *testing.T) {
	svc, base, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	desired := facilityStoredSpec()
	desired.SpecHash = "first-install"
	client := &facilityReloadRecorder{fakeRuntimeClient: base, resp: agentcontract.RuntimeReloadResponse{Reloaded: true}}
	svc.runtimeClient = client
	provider := &fakeFacilityRuntimeProvider{}
	svc.SetFacilityRuntimeProvider(provider)

	reconciler := &serviceRuntimeReconciler{service: svc}
	_, ok, err := reconciler.tryFacilityConfigReload(ctx, "https://srv-a.agent", facilityApplyRequest(t, desired), desired)
	if err != nil {
		t.Fatalf("facility plan: %v", err)
	}
	if ok {
		t.Fatal("missing stored current spec must fall through to RuntimeReconcile (recreate)")
	}
	if provider.calls != 1 || provider.current[0].Image != "" {
		t.Fatalf("facility current for first install = %#v", provider.current)
	}
	if len(client.reloads) != 0 {
		t.Fatalf("RuntimeReload calls = %d for first install, want 0", len(client.reloads))
	}
}

func TestStoredRuntimeSpecReadsInstanceRow(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	stored := facilityStoredSpec()
	seedStoredRuntimeSpec(t, svc, stored)
	reconciler := &serviceRuntimeReconciler{service: svc}

	byInstance := reconciler.storedRuntimeSpec(ctx, controlplane.ReconcileRequestRPC{
		ApplicationID: FacilityProxyApplicationID, InstanceID: "facility-instance", ServerID: "srv-a",
	})
	if byInstance.Image != stored.Image || byInstance.ContainerName != stored.ContainerName {
		t.Fatalf("storedRuntimeSpec by instance = %#v", byInstance)
	}
	// instance ID 缺失时按 application+server 定位同一行。
	byServer := reconciler.storedRuntimeSpec(ctx, controlplane.ReconcileRequestRPC{
		ApplicationID: FacilityProxyApplicationID, ServerID: "srv-a",
	})
	if byServer.Image != stored.Image {
		t.Fatalf("storedRuntimeSpec by application/server = %#v", byServer)
	}
	// 无匹配行返回零值（设施按首装处理），不得报错。
	if empty := reconciler.storedRuntimeSpec(ctx, controlplane.ReconcileRequestRPC{
		ApplicationID: "another-app", InstanceID: "missing", ServerID: "srv-a",
	}); empty.Image != "" {
		t.Fatalf("missing instance row must yield empty current spec, got %#v", empty)
	}
}
