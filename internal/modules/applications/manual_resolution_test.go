package applications

import (
	"context"
	"testing"

	contract "panel/internal/agent/contract"
	"panel/internal/agent/executionevents"
	control "panel/internal/orchestrator"
	"panel/internal/platform/activitylog"
)

type manualTransportClient struct {
	*activityTransportClient
	resolutionCalls int
}

func (c *manualTransportClient) ResolveExecution(ctx context.Context, _ string, input contract.ExecutionResolution) (contract.ExecutionResult, error) {
	c.resolutionCalls++
	return c.spool.Resolve(ctx, input)
}

func TestManualJobResolutionPreservesHumanEvidenceWithoutFabricatedObservation(t *testing.T) {
	svc, base, _, closeService := newTestService(t)
	defer closeService()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "manual", Enabled: false, SpecYAML: "name: manual\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	store := control.NewStore(svc.db)
	planned, err := control.NewPlanner(store).Plan(ctx, control.PlanInput{ApplicationID: app.ID, ServerID: "srv-a", IntentID: "manual-operation", Action: control.ActionStop})
	if err != nil {
		t.Fatal(err)
	}
	job, claimed, err := store.Claim(ctx, planned.Job.ID, "worker", 0)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	spool, err := executionevents.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	session, _, err := spool.Begin(ctx, contract.RuntimeReconcileRequest{ExecutionID: job.ExecutionID, OperationID: job.IntentID, RunID: job.IntentID, JobID: job.ID, ApplicationID: job.ApplicationID, ServerID: job.ServerID, InstanceID: job.InstanceID, Action: job.Action})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Finish(ctx, contract.RuntimeReconcileResponse{}, context.Canceled); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUncertain(ctx, job, "transport lost"); err != nil {
		t.Fatal(err)
	}
	client := &manualTransportClient{activityTransportClient: &activityTransportClient{fakeRuntimeClient: base, spool: spool, db: svc.db, serverID: job.ServerID}}
	svc.runtimeClient = client
	if _, err := svc.ResolveExecutionManually(ctx, job.ExecutionID, "succeeded", "checked remote process"); err == nil {
		t.Fatal("unauthenticated declaration accepted")
	}
	if client.resolutionCalls != 0 {
		t.Fatal("unauthenticated request reached Agent")
	}
	userCtx := activitylog.WithActor(ctx, activitylog.Actor{Kind: "user", ID: "admin", Name: "operator"})
	if _, err := svc.ResolveExecutionManually(userCtx, job.ExecutionID, "succeeded", "checked remote process and containers"); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.State != control.JobSucceeded || resolved.ExecutionID != job.ExecutionID {
		t.Fatalf("incorrect resolved job: %#v", resolved)
	}
	var observedState string
	if err := svc.db.QueryRow(`SELECT observed_state FROM application_instances WHERE id=?`, job.InstanceID).Scan(&observedState); err != nil {
		t.Fatal(err)
	}
	if observedState != "unknown" {
		t.Fatalf("manual declaration fabricated runtime observation: %s", observedState)
	}
	var manual, unknown, closed int
	for _, item := range []struct {
		event string
		count *int
	}{{"execution.verified", &manual}, {"uncertainty.detected", &unknown}, {"stream.closed", &closed}} {
		if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events WHERE execution_id=? AND event_type=?`, job.ExecutionID, item.event).Scan(item.count); err != nil {
			t.Fatal(err)
		}
	}
	if manual != 1 || unknown < 1 || closed != 1 {
		t.Fatalf("evidence not retained: manual=%d unknown=%d closed=%d", manual, unknown, closed)
	}
	if _, err := svc.ResolveExecutionManually(userCtx, job.ExecutionID, "failed", "different declaration"); err == nil {
		t.Fatal("terminal declaration was overwritten")
	}
	if client.resolutionCalls != 1 {
		t.Fatal("terminal execution invoked Agent twice")
	}
}
