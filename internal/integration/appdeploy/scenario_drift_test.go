package appdeploy

import (
	"testing"
	"time"

	"panel/internal/modules/applications"
)

// 剧本：agent 上报容器缺失（desired=running 且 observed=missing）→
// 漂移修复触发 apply → 重新收敛为 running。
func TestScenarioDriftMissingContainerTriggersApply(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
	before := h.Agent.RecordCount(app.ID, "srv-a", "apply")

	// 模拟 agent 上报：托管容器消失。
	h.WriteObservation(runtimeInstanceIDFor(app.ID, "srv-a"), "missing", app.Generation, app.SpecHash)
	// 周期巡检等价动作：观测到漂移 → 触发 apply 修复。
	h.Plan(applications.DeploymentPlanRequest{
		ApplicationID:        app.ID,
		ServerIDs:            []string{"srv-a"},
		ObservedRuntimeDrift: true,
		TriggerType:          "agent_report",
	})

	h.WaitAgentCalls(app.ID, "srv-a", "apply", before+1)
	h.WaitNoActiveJob(app.ID, "srv-a")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
}

// 剧本：agent 上报 generation/spec_hash 漂移（容器还在但版本旧）→
// 触发 apply → 收敛到新期望。
func TestScenarioDriftGenerationTriggersApply(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
	before := h.Agent.RecordCount(app.ID, "srv-a", "apply")

	// 上报旧 generation（期望 1，上报 0）。
	h.WriteObservation(runtimeInstanceIDFor(app.ID, "srv-a"), "running", 0, app.SpecHash)
	h.Plan(applications.DeploymentPlanRequest{
		ApplicationID:        app.ID,
		ServerIDs:            []string{"srv-a"},
		ObservedRuntimeDrift: true,
		TriggerType:          "periodic_scan",
	})

	h.WaitAgentCalls(app.ID, "srv-a", "apply", before+1)
	h.WaitNoActiveJob(app.ID, "srv-a")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
}

// 剧本：健康且无漂移时，周期巡检不产生新 apply（不制造无效重部署）。
func TestScenarioHealthyNoExtraApply(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", true, "name: web\nimage: nginx\n")
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
	before := h.Agent.RecordCount(app.ID, "srv-a", "apply")

	// 无触发、无漂移：短暂窗口内不产生额外 apply，也不出现活跃 Job。
	time.Sleep(300 * time.Millisecond)
	if got := h.Agent.RecordCount(app.ID, "srv-a", "apply"); got != before {
		t.Fatalf("healthy instance triggered extra apply: before=%d after=%d", before, got)
	}
	h.WaitNoActiveJob(app.ID, "srv-a")
}
