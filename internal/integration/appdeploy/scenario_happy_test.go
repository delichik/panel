package appdeploy

import (
	"testing"
)

// 剧本：保存启用应用 → 两台服务器各生成 apply Job → agent 按请求收敛为
// running → Job succeeded → 实例 observed=running → 聚合状态 running。
func TestScenarioHappyPathDeployConverges(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")
	h.AddServer("srv-b")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")

	for _, srv := range []string{"srv-a", "srv-b"} {
		h.WaitJobState(app.ID, srv, "succeeded")
		h.WaitInstanceObserved(app.ID, srv, "running")
		h.WaitNoActiveJob(app.ID, srv)
	}
	if got := h.RuntimeStatus(app.ID); got != "running" {
		t.Fatalf("runtime status = %q, want running", got)
	}
	// 每台服务器恰好一次 apply，无重复执行。
	if got := h.Agent.RecordCount(app.ID, "srv-a", "apply"); got != 1 {
		t.Fatalf("apply calls on srv-a = %d, want 1", got)
	}
	if got := h.Agent.RecordCount(app.ID, "srv-b", "apply"); got != 1 {
		t.Fatalf("apply calls on srv-b = %d, want 1", got)
	}
}

// 剧本：停用应用 → stop Job → agent 收敛为 stopped → 实例 observed=stopped。
func TestScenarioStopConverges(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")

	if _, err := h.AppSvc.Stop(h.ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "stopped")
	if got := h.RuntimeStatus(app.ID); got != "stopped" {
		t.Fatalf("runtime status = %q, want stopped", got)
	}
	if got := h.Agent.RecordCount(app.ID, "srv-a", "stop"); got != 1 {
		t.Fatalf("stop calls = %d, want 1", got)
	}
}

// 剧本：删除应用 → purge Job（RemoveData）→ agent 收敛为 missing →
// 实例被清理 → 应用物理删除（删除 finalizer 全链路）。
func TestScenarioDeleteFinalizerConverges(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")

	if err := h.AppSvc.Delete(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	// purge Job 执行后，finalizer 会连同终态 Job 一起清理，因此等应用物理删除。
	h.WaitAgentCalls(app.ID, "srv-a", "purge", 1)
	h.WaitAppDeleted(app.ID)
	if got := h.Agent.RecordCount(app.ID, "srv-a", "purge"); got != 1 {
		t.Fatalf("purge calls = %d, want 1", got)
	}
}

// 剧本：停用后再启用 → 期望从 stopped 回到 running，apply 重新执行。
func TestScenarioReenableRestoresRunning(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")

	if _, err := h.AppSvc.Stop(h.ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "stopped")

	// 启用：Deploy 会置 enabled=true 并触发 apply 规划。
	if _, err := h.AppSvc.Deploy(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
	if got := h.RuntimeStatus(app.ID); got != "running" {
		t.Fatalf("runtime status after re-enable = %q, want running", got)
	}
}
