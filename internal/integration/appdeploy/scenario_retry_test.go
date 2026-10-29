package appdeploy

import (
	"testing"
	"time"
)

// 剧本：apply 第一次可重试失败 → Job failed_retryable + 未来 next_run_at →
// 退避到期自动重试 → 第二次成功收敛。
func TestScenarioRetryableFailureBackoffThenSuccess(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", false, "name: web\nimage: nginx\n")
	// 第 1 次 apply 失败可重试，脚本指定短退避。
	h.Agent.Script(app.ID, "srv-a", "apply", 1, ScriptedResponse{
		ErrorCode:    "docker_unavailable",
		ErrorClass:   "docker_unavailable",
		ErrorMessage: "engine down",
		Retryable:    true,
		RetryAfter:   200 * time.Millisecond,
	})
	if _, err := h.AppSvc.Deploy(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}

	job := h.WaitJobState(app.ID, "srv-a", "failed_retryable")
	if job.NextRunAt == nil || !job.NextRunAt.After(time.Now()) {
		t.Fatalf("failed_retryable job should persist future next_run_at, got %v", job.NextRunAt)
	}
	if job.ErrorCode != "docker_unavailable" || job.ErrorClass != "docker_unavailable" {
		t.Fatalf("job error = code=%q class=%q", job.ErrorCode, job.ErrorClass)
	}

	// 退避到期后自动重试成功。
	h.WaitJobState(app.ID, "srv-a", "succeeded")
	h.WaitInstanceObserved(app.ID, "srv-a", "running")
	if got := h.Agent.RecordCount(app.ID, "srv-a", "apply"); got != 2 {
		t.Fatalf("apply calls = %d, want 2 (fail then success)", got)
	}
}

// 剧本：不可重试失败（如 invalid_spec）→ Job 终态 failed，不再自动重试。
func TestScenarioNonRetryableFailureFailsJob(t *testing.T) {
	h := NewHarness(t)
	h.AddServer("srv-a")

	app := h.CreateApp("web", false, "name: web\nimage: nginx\n")
	h.Agent.Script(app.ID, "srv-a", "apply", 0, ScriptedResponse{
		ErrorCode:    "invalid_spec",
		ErrorClass:   "invalid_spec",
		ErrorMessage: "rendered spec is invalid",
		Retryable:    false,
	})
	if _, err := h.AppSvc.Deploy(h.ctx, app.ID); err != nil {
		t.Fatal(err)
	}

	job := h.WaitJobState(app.ID, "srv-a", "failed")
	if job.ErrorCode != "invalid_spec" {
		t.Fatalf("job error code = %q", job.ErrorCode)
	}
	// 不可重试：不再调度重试。
	time.Sleep(200 * time.Millisecond)
	if got := h.Agent.RecordCount(app.ID, "srv-a", "apply"); got != 1 {
		t.Fatalf("apply calls = %d, want 1 (no retry for terminal failure)", got)
	}
}

// 剧本：RPC 超时/结果未知 → retryable 重放完整 RuntimeReconcile → 第二次成功。
