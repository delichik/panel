package facilityapps

import (
	"context"
	"path/filepath"
	"testing"

	agentcontract "panel/internal/agent/contract"
	server "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

// recordingReconcileTrigger 记录反向代理协调触发次数。
type recordingReconcileTrigger struct{ calls int }

func (r *recordingReconcileTrigger) TriggerApplicationReconcile(context.Context, tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	r.calls++
	return tasks.Task{}, false, nil
}

func newInterconnectSyncTestService(t *testing.T) (*Service, *recordingReconcileTrigger, *tasks.Service) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	servers := facilityTestServers{items: map[string]server.Server{
		"srv-a": {ID: "srv-a", Name: "storage-a", Host: "10.0.0.5", Port: 22, SSHUsername: "root", CredentialID: "cred-a",
			Traits: map[string]string{agentcontract.TraitEnabled: "true", agentcontract.TraitURL: "https://10.0.0.5:9786"}},
		"srv-b": {ID: "srv-b", Name: "app-b", Host: "10.0.0.6", Port: 22, SSHUsername: "root", CredentialID: "cred-b",
			Traits: map[string]string{agentcontract.TraitEnabled: "true", agentcontract.TraitURL: "https://10.0.0.6:9786"}},
	}}
	taskSvc := tasks.NewService(store.AppDB())
	svc := NewService(store.AppDB(), nil, servers, &fakeAppsProvider{}, WithDataRoot(cfg.DataRoot), WithTaskService(taskSvc))
	trigger := &recordingReconcileTrigger{}
	svc.reconciler = trigger
	return svc, trigger, taskSvc
}

// TestSyncInterconnectServersIgnoresEmptyInput 空集合表示没有可判定的变化。
func TestSyncInterconnectServersIgnoresEmptyInput(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	if err := svc.SyncInterconnectServers(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != 0 {
		t.Fatalf("reconcile calls = %d, want none", trigger.calls)
	}
}

// TestSyncInterconnectServersReconcilesParticipatingGateway 参与反向代理设施的
// 节点地址变化必须触发重新渲染。
func TestSyncInterconnectServersReconcilesParticipatingGateway(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}}); err != nil {
		t.Fatal(err)
	}
	before := trigger.calls
	if err := svc.SyncInterconnectServers(ctx, []string{"srv-a"}); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != before+1 {
		t.Fatalf("reconcile calls = %d, want one more than %d", trigger.calls, before)
	}
}

// TestSyncInterconnectServersSkipsUnrelatedServer 与设施无关的节点不应造成无谓
// 的重新渲染。
func TestSyncInterconnectServersSkipsUnrelatedServer(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}}); err != nil {
		t.Fatal(err)
	}
	before := trigger.calls
	if err := svc.SyncInterconnectServers(ctx, []string{"srv-unknown"}); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != before {
		t.Fatalf("reconcile calls = %d, want %d", trigger.calls, before)
	}
}

// TestSyncInterconnectServersResyncsStorageExports 存储共享的导出白名单包含所有
// 启用 tailscale 的节点地址，因此任何相关节点变化都要重新下发导出配置。
func TestSyncInterconnectServersResyncsStorageExports(t *testing.T) {
	svc, _, taskSvc := newInterconnectSyncTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveStorageShare(ctx, StorageShareSaveInput{Servers: []StorageServerSetting{{ServerID: "srv-a", Root: "/srv/panel-storage"}}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncInterconnectServers(ctx, []string{"srv-b"}); err != nil {
		t.Fatal(err)
	}
	result, err := taskSvc.List(ctx, tasks.ListFilter{Type: storageReconcileTaskType, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total == 0 {
		t.Fatal("expected a storage export reconcile task after an interconnect address change")
	}
}

// TestSyncInterconnectServersWithoutFacilitiesIsNoop 未配置任何设施时不得报错。
func TestSyncInterconnectServersWithoutFacilitiesIsNoop(t *testing.T) {
	svc, trigger, _ := newInterconnectSyncTestService(t)
	if err := svc.SyncInterconnectServers(context.Background(), []string{"srv-a"}); err != nil {
		t.Fatal(err)
	}
	if trigger.calls != 0 {
		t.Fatalf("reconcile calls = %d, want none without a configured facility", trigger.calls)
	}
}
