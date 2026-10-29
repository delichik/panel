package appdeploy

import (
	"testing"
	"time"

	"panel/internal/modules/applications"
)

// 剧本：非托管同名容器冲突 → 终态失败（不删除非托管资源，不自动重试）。
func TestScenarioNonManagedConflictFailsTerminal(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", false, "name: web\nimage: nginx\n")
	h.Agent.Script(app.ID, "srv-a", "apply", 0, ScriptedResponse{
		ErrorCode:    "non_managed_conflict",
		ErrorClass:   "non_managed_conflict",
		ErrorMessage: "container name is owned by a different resource",
		Retryable:    false,
	})
	if _, err := h.AppSvc.Deploy(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	job := h.WaitJobState(app.ID, "srv-a", "failed")
	if job.ErrorCode != "non_managed_conflict" {
		t.Fatalf("job error code = %q, want non_managed_conflict", job.ErrorCode)
	}
	time.Sleep(200 * time.Millisecond)
	if got := h.Agent.RecordCount(app.ID, "srv-a", "apply"); got != 1 {
		t.Fatalf("apply calls = %d, want 1 (terminal conflict must not retry)", got)
	}
}

// 剧本：apply 执行期间用户发起 stop → 旧 apply 不得覆盖新期望，执行完重新
// 排队为 stop → 最终收敛为 stopped。
func TestScenarioStopDuringApplyDoesNotOverwriteDesired(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", false, "name: web\nimage: nginx\n")
	if _, err := h.AppSvc.Deploy(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")

	// 下一次 apply 延迟 500ms 返回（模拟远端执行中）。
	h.Agent.Script(app.ID, "srv-a", "apply", 0, ScriptedResponse{Success: true, Delay: 500 * time.Millisecond})
	// 触发一次强制 apply（重启语义）。
	h.Plan(applications.DeploymentPlanRequest{
		ApplicationID: app.ID,
		ServerIDs:     []string{"srv-a"},
		Force:         true,
		Manual:        true,
		TriggerType:   "application_restart",
	})
	// apply 仍在执行中时，用户发起 stop。
	time.Sleep(120 * time.Millisecond)
	if _, err := h.AppSvc.Stop(h.ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}

	// 旧 apply 返回后不得把期望覆盖回 running：最终必须收敛为 stopped。
	h.WaitInstanceObserved(app.ID, "srv-a", "stopped")
	h.WaitNoActiveJob(app.ID, "srv-a")
	if got := h.RuntimeStatus(app.ID); got != "stopped" {
		t.Fatalf("runtime status = %q, want stopped (old apply must not overwrite stop)", got)
	}
	// 停止最终由 stop 动作完成。
	h.WaitAgentCalls(app.ID, "srv-a", "stop", 1)
}
