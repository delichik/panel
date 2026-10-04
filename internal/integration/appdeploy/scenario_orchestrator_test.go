package appdeploy

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

// agentReconciler 把控制面 ReconcileRequestRPC 转发给 mock agent 的
// RuntimeReconcile（等价于应用模块的 serviceRuntimeReconciler）。
type agentReconciler struct {
	agent *ScriptedAgent
}

func (r agentReconciler) Reconcile(ctx context.Context, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, error) {
	var spec appruntime.Spec
	_ = json.Unmarshal(req.RenderedRuntimeSpec, &spec)
	resp, err := r.agent.RuntimeReconcile(ctx, "", agentcontract.RuntimeReconcileRequest{
		OperationID: req.OperationID, RunID: req.RunID, JobID: req.JobID, ExecutionID: req.ExecutionID, ApplicationID: req.ApplicationID, InstanceID: req.InstanceID,
		ServerID: req.ServerID, Action: req.Action, DesiredGeneration: req.DesiredGeneration, DesiredSpecHash: req.DesiredSpecHash,
		DesiredRevisionID: req.DesiredRevisionID, Spec: spec, RemoveData: req.RemoveData, PreviousContainerName: req.PreviousContainerName,
	})
	if err != nil {
		return controlplane.ReconcileResponse{ErrorCode: "runtime_reconcile_failed", ErrorClass: "runtime", ErrorMessage: err.Error(), Retryable: true}, err
	}
	return scriptedControlResponse(resp), nil
}

func scriptedControlResponse(resp agentcontract.RuntimeReconcileResponse) controlplane.ReconcileResponse {
	steps := make([]controlplane.Step, 0, len(resp.Steps))
	for _, s := range resp.Steps {
		steps = append(steps, controlplane.Step{Name: s.Name, Status: s.Status, Detail: s.Detail})
	}
	return controlplane.ReconcileResponse{
		ObservedState: resp.ObservedState, ContainerName: resp.ContainerName, ContainerID: resp.ContainerID,
		ObservedGeneration: resp.ObservedGeneration, ObservedSpecHash: resp.ObservedSpecHash, ObservedImageDigest: resp.ObservedImageDigest,
		ObservedAt: resp.ObservedAt, Steps: steps,
		ErrorCode: resp.ErrorCode, ErrorClass: resp.ErrorClass, ErrorMessage: resp.ErrorMessage, ErrorDetail: resp.ErrorDetail,
		Retryable: resp.Retryable, RetryAfter: resp.RetryAfter,
	}
}

func (r agentReconciler) ResolveExecution(ctx context.Context, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, bool, error) {
	result, err := r.agent.GetExecutionResult(ctx, "", req.ExecutionID)
	if err != nil {
		return controlplane.ReconcileResponse{}, false, err
	}
	if result.State != "finished" || result.Result == nil {
		return controlplane.ReconcileResponse{}, false, nil
	}
	return scriptedControlResponse(*result.Result), true, nil
}

// orchHarness 是 orchestrator 层剧本台：短租约 controller + 真实 store。
type orchHarness struct {
	t     *testing.T
	store *storage.Store
	agent *ScriptedAgent
	db    *controlplane.Store
}

func newOrchHarness(t *testing.T) *orchHarness {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,spec_yaml,job_id,created_at,updated_at)
		VALUES('app-1','web','name: web\nimage: nginx\n','',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,'password','root',?,?)`,
		"cred-srv-a", "test", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at)
		VALUES('srv-a','srv-a','127.0.0.1',22,'root','cred-srv-a','unix:///var/run/docker.sock','{}','{}',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	return &orchHarness{t: t, store: store, agent: NewScriptedAgent(t), db: controlplane.NewStore(store.AppDB())}
}

// planApply 通过 planner 规划一个 apply Job。
func (o *orchHarness) planApply() controlplane.Job {
	o.t.Helper()
	specJSON, _ := json.Marshal(appruntime.Spec{ApplicationID: "app-1", InstanceID: "app-1-srv-a", ContainerName: "panel-web", Image: "nginx"})
	_, results, err := controlplane.NewPlanner(o.db).EnsureRevisionAndPlanBatch(o.t.Context(), controlplane.RevisionInput{
		ApplicationID:       "app-1",
		Generation:          1,
		SpecHash:            "hash-1",
		RenderedRuntimeSpec: specJSON,
		SpecYAML:            "name: web\nimage: nginx\n",
	}, []controlplane.PlanInput{{
		ApplicationID:     "app-1",
		ServerID:          "srv-a",
		InstanceID:        "app-1-srv-a",
		Action:            controlplane.ActionApply,
		DesiredState:      controlplane.DesiredRunning,
		DesiredGeneration: 1,
		DesiredSpecHash:   "hash-1",
		DesiredSpecJSON:   specJSON,
		ContainerName:     "panel-web",
		IntentID:          "intent-1",
		TriggerType:       "test",
	}})
	if err != nil {
		o.t.Fatal(err)
	}
	return results[0].Job
}

func (o *orchHarness) startController(leaseTTL time.Duration, owner string) *controlplane.Controller {
	o.t.Helper()
	ctrl := controlplane.NewController(o.db, agentReconciler{o.agent}, controlplane.ControllerConfig{
		Owner:        owner,
		LeaseTTL:     leaseTTL,
		ScanInterval: 20 * time.Millisecond,
		WorkerCount:  2,
	})
	if err := ctrl.Start(context.Background()); err != nil {
		o.t.Fatal(err)
	}
	return ctrl
}

// The remote action completed durably, but worker A lost its response before
// committing the Job result. Lease expiry must verify that same execution;
// neither expiry nor a missing local result authorizes a second side effect.
func TestScenarioLeaseExpiryRecoveryVerifiesDurableResult(t *testing.T) {
	o := newOrchHarness(t)
	job := o.planApply()
	claimed, ok, err := o.db.Claim(context.Background(), job.ID, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	var spec appruntime.Spec
	if err = json.Unmarshal(claimed.DesiredSpecJSON, &spec); err != nil {
		t.Fatal(err)
	}
	_, err = o.agent.RuntimeReconcile(context.Background(), "", agentcontract.RuntimeReconcileRequest{OperationID: claimed.IntentID, RunID: claimed.IntentID, JobID: claimed.ID, ExecutionID: claimed.ExecutionID, ApplicationID: claimed.ApplicationID, InstanceID: claimed.InstanceID, ServerID: claimed.ServerID, Action: claimed.Action, DesiredGeneration: claimed.DesiredGeneration, DesiredSpecHash: claimed.DesiredSpecHash, DesiredRevisionID: claimed.DesiredRevisionID, Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if _, err = o.store.AppDB().Exec(`UPDATE jobs SET last_stage='reconcile_started',lease_expires_at=? WHERE id=?`, past, job.ID); err != nil {
		t.Fatal(err)
	}
	if err = o.db.RecoverExpiredLeases(context.Background()); err != nil {
		t.Fatal(err)
	}
	pending, err := o.db.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != controlplane.JobRunning || pending.ErrorClass != "uncertainty" || pending.ExecutionID != claimed.ExecutionID || pending.LeaseToken != claimed.LeaseToken || pending.NextRunAt != nil {
		t.Fatalf("uncertain execution lost its identity/fence: %+v", pending)
	}
	if _, claimedAgain, err := o.db.Claim(context.Background(), job.ID, "worker-b", time.Second); err != nil || claimedAgain {
		t.Fatalf("uncertain job was reclaimed: %v %v", claimedAgain, err)
	}
	ctrl := o.startController(2*time.Second, "worker-b")
	defer ctrl.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		current, err := o.db.GetJob(context.Background(), job.ID)
		if err == nil && current.State == controlplane.JobSucceeded {
			if current.ExecutionID != claimed.ExecutionID || current.Attempts != 1 || o.agent.RecordCount("app-1", "srv-a", "apply") != 1 {
				t.Fatalf("recovery repeated execution: %+v calls=%d", current, o.agent.RecordCount("app-1", "srv-a", "apply"))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("durable remote result did not resolve uncertainty, state=%s", mustJobState(t, o, job.ID))
}

func TestScenarioLeaseExpiryWithoutResultRetainsFence(t *testing.T) {
	o := newOrchHarness(t)
	job := o.planApply()
	claimed, ok, err := o.db.Claim(context.Background(), job.ID, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if _, err = o.store.AppDB().Exec(`UPDATE jobs SET lease_expires_at=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), job.ID); err != nil {
		t.Fatal(err)
	}
	ctrl := o.startController(2*time.Second, "worker-b")
	defer ctrl.Stop()
	time.Sleep(100 * time.Millisecond)
	current, err := o.db.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != controlplane.JobRunning || current.ErrorClass != "uncertainty" || current.ExecutionID != claimed.ExecutionID || o.agent.RecordCount("app-1", "srv-a", "apply") != 0 {
		t.Fatalf("unverified side effect was retried: %+v", current)
	}
}

func mustJobState(t *testing.T, o *orchHarness, jobID string) string {
	t.Helper()
	j, err := o.db.GetJob(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	return j.State
}

func TestScenarioConcurrentClaimOnlyOneWorker(t *testing.T) {
	o := newOrchHarness(t)
	job := o.planApply()

	ctrlA := o.startController(2*time.Second, "worker-a")
	ctrlB := o.startController(2*time.Second, "worker-b")
	defer ctrlA.Stop()
	defer ctrlB.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := o.db.GetJob(context.Background(), job.ID)
		if err == nil && j.State == controlplane.JobSucceeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	j, err := o.db.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != controlplane.JobSucceeded {
		t.Fatalf("job state = %s", j.State)
	}
	if got := o.agent.RecordCount("app-1", "srv-a", "apply"); got != 1 {
		t.Fatalf("apply calls = %d, want exactly 1 (only one worker claimed)", got)
	}
}

// 剧本：旧 lease token 写回必须被拒绝（fencing）。
func TestScenarioStaleTokenWritebackRejected(t *testing.T) {
	o := newOrchHarness(t)
	job := o.planApply()

	claimed, ok, err := o.db.Claim(context.Background(), job.ID, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim failed ok=%v err=%v", ok, err)
	}
	// 模拟另一个 worker 接管：直接改写 lease_token（旧 worker 仍持有旧 token）。
	if _, err := o.store.AppDB().Exec(`UPDATE jobs SET lease_token=? WHERE id=?`, "new-token", job.ID); err != nil {
		t.Fatal(err)
	}
	ok, err = o.db.Succeed(context.Background(), claimed, controlplane.ReconcileResponse{ObservedState: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale lease token must not succeed the job")
	}
	ok, err = o.db.Fail(context.Background(), claimed, controlplane.ReconcileResponse{ErrorCode: "x", ErrorClass: "y", ErrorMessage: "z", Retryable: true})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("stale lease token must not fail the job")
	}
	// 新 owner 的 token 仍可正常写回。
	current, err := o.db.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != controlplane.JobRunning {
		t.Fatalf("job state = %s, want running (untouched by stale writeback)", current.State)
	}
}
