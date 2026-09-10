package applications

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	contract "panel/internal/agent/contract"
	"panel/internal/agent/executionevents"
	server "panel/internal/modules/servers"
	control "panel/internal/orchestrator"
	"panel/internal/platform/activitylog"
)

type activityTransportClient struct {
	*fakeRuntimeClient
	spool             *executionevents.Store
	db                *sql.DB
	serverID          string
	ackCount          int
	transform         func(contract.ExecutionEventsResponse) contract.ExecutionEventsResponse
	finishOnReconcile *executionevents.Session
	lastReconcile     contract.RuntimeReconcileRequest
}

func (c *activityTransportClient) ReadExecutionEvents(ctx context.Context, _ string, req contract.ExecutionEventsRequest) (contract.ExecutionEventsResponse, error) {
	page, err := c.spool.Read(ctx, req)
	if err == nil && c.transform != nil {
		page = c.transform(page)
	}
	return page, err
}
func (c *activityTransportClient) AckExecutionEvents(ctx context.Context, _ string, req contract.ExecutionEventsAck) error {
	var count int64
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM activity_events WHERE source_id=? AND source_epoch=? AND source_stream_id=? AND source_seq<=?`, "agent:"+c.serverID, req.SourceEpoch, req.SourceStreamID, req.ThroughSourceSeq).Scan(&count); err != nil {
		return err
	}
	if count != req.ThroughSourceSeq {
		return errors.New("ACK preceded durable contiguous receipt")
	}
	c.ackCount++
	return c.spool.Ack(ctx, req)
}
func (c *activityTransportClient) GetExecutionResult(ctx context.Context, _ string, id string) (contract.ExecutionResult, error) {
	return c.spool.Result(ctx, id)
}
func (c *activityTransportClient) RuntimeReconcile(ctx context.Context, _ string, req contract.RuntimeReconcileRequest) (contract.RuntimeReconcileResponse, error) {
	c.lastReconcile = req
	result := contract.RuntimeReconcileResponse{ObservedState: "running"}
	if c.finishOnReconcile != nil {
		if err := c.finishOnReconcile.Finish(ctx, result, nil); err != nil {
			return contract.RuntimeReconcileResponse{}, err
		}
	}
	return result, nil
}

func trackedTransport(t *testing.T) (*serviceRuntimeReconciler, *activityTransportClient, control.ReconcileRequestRPC, *executionevents.Session) {
	t.Helper()
	svc, base, _, closeService := newTestService(t)
	t.Cleanup(closeService)
	spool, err := executionevents.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { spool.Close() })
	req := control.ReconcileRequestRPC{OperationID: "original-operation", RunID: "original-run", JobID: "job", ExecutionID: "execution", ApplicationID: "app", InstanceID: "instance", ServerID: "srv-a", Action: "apply", DesiredGeneration: 7, DesiredSpecHash: "hash"}
	client := &activityTransportClient{fakeRuntimeClient: base, spool: spool, db: svc.db, serverID: req.ServerID}
	svc.runtimeClient = client
	_, err = activitylog.Append(context.Background(), svc.db, []activitylog.EventInput{{EventType: "execution.started", OperationID: req.OperationID, RunID: req.RunID, ExecutionID: req.ExecutionID, SourceID: "panel", SourceStreamID: "control", Actor: activitylog.Actor{Kind: "controller"}, Initiator: activitylog.Actor{Kind: "user", ID: "operator", Name: "Original operator"}, Resources: []activitylog.Resource{{Type: "application", ID: req.ApplicationID, Name: "Original app name", Generation: 7}}, Data: map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := spool.Begin(context.Background(), contract.RuntimeReconcileRequest{OperationID: req.OperationID, RunID: req.RunID, JobID: req.JobID, ExecutionID: req.ExecutionID, ApplicationID: req.ApplicationID, InstanceID: req.InstanceID, ServerID: req.ServerID, Action: req.Action, DesiredGeneration: req.DesiredGeneration, DesiredSpecHash: req.DesiredSpecHash})
	if err != nil {
		t.Fatal(err)
	}
	return &serviceRuntimeReconciler{service: svc}, client, req, session
}

func TestRuntimeReconcilePropagatesNormalizedSSHPort(t *testing.T) {
	r, client, req, session := trackedTransport(t)
	r.service.servers.(*fakeServerProvider).items[req.ServerID] = func() server.Server {
		srv := readyServer(req.ServerID)
		srv.Port = 22022
		srv.Traits[contract.TraitURL] = "https://127.0.0.1:10986"
		return srv
	}()
	channelChecks := 0
	r.service.servers.(*fakeServerProvider).firewallChannelCheck = func(context.Context, string) error {
		channelChecks++
		return nil
	}
	client.finishOnReconcile = session
	req.Action = "stop"
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if client.lastReconcile.SSHPort != 22022 || client.lastReconcile.AgentPort != 10986 {
		t.Fatalf("management ports = ssh:%d agent:%d", client.lastReconcile.SSHPort, client.lastReconcile.AgentPort)
	}
	if channelChecks != 2 {
		t.Fatalf("channel checks = %d, want pre/post", channelChecks)
	}
}

func TestRuntimeReconcileRejectsReservedSSHHostPortBeforeAgentCall(t *testing.T) {
	r, client, req, _ := trackedTransport(t)
	r.service.servers.(*fakeServerProvider).items[req.ServerID] = func() server.Server {
		srv := readyServer(req.ServerID)
		srv.Port = 22022
		return srv
	}()
	req.RenderedRuntimeSpec = []byte(`{"applicationId":"app","instanceId":"instance","containerName":"panel-app","image":"nginx","ports":[{"containerPort":8080,"hostPort":22022}]}`)
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "application_ssh_port_reserved" || result.Retryable {
		t.Fatalf("result = %#v", result)
	}
	if client.lastReconcile.ApplicationID != "" {
		t.Fatalf("agent must not be called, request = %#v", client.lastReconcile)
	}
}

func TestRuntimeReconcileRejectsReservedAgentHostPortBeforeAgentCall(t *testing.T) {
	r, client, req, _ := trackedTransport(t)
	r.service.servers.(*fakeServerProvider).items[req.ServerID].Traits[contract.TraitURL] = "https://127.0.0.1:10986"
	req.RenderedRuntimeSpec = []byte(`{"applicationId":"app","instanceId":"instance","containerName":"panel-app","image":"nginx","ports":[{"containerPort":8080,"hostPort":10986}]}`)
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "application_agent_port_reserved" || result.Retryable {
		t.Fatalf("result = %#v", result)
	}
	if client.lastReconcile.ApplicationID != "" {
		t.Fatalf("agent must not be called, request = %#v", client.lastReconcile)
	}
}

func TestRuntimeReconcileReportsFirewallChannelPreflightAsRetryableWithoutAgentCall(t *testing.T) {
	r, client, req, _ := trackedTransport(t)
	req.RenderedRuntimeSpec = []byte(`{"applicationId":"app","instanceId":"instance","containerName":"panel-app","image":"nginx"}`)
	r.service.servers.(*fakeServerProvider).firewallChannelCheck = func(context.Context, string) error {
		return errors.New("ssh unavailable")
	}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "firewall_channel_preflight_failed" || !result.Retryable {
		t.Fatalf("result = %#v", result)
	}
	if client.lastReconcile.ApplicationID != "" {
		t.Fatalf("agent must not be called, request = %#v", client.lastReconcile)
	}
}

func TestRuntimeReconcileReportsFirewallChannelPostflightAsRetryableKnownFailure(t *testing.T) {
	r, client, req, session := trackedTransport(t)
	req.RenderedRuntimeSpec = []byte(`{"applicationId":"app","instanceId":"instance","containerName":"panel-app","image":"nginx"}`)
	checks := 0
	r.service.servers.(*fakeServerProvider).firewallChannelCheck = func(context.Context, string) error {
		checks++
		if checks == 2 {
			return errors.New("agent reconnect failed")
		}
		return nil
	}
	client.finishOnReconcile = session
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "firewall_channel_postflight_failed" || !result.Retryable {
		t.Fatalf("result = %#v", result)
	}
	if client.lastReconcile.ApplicationID != req.ApplicationID {
		t.Fatalf("agent request = %#v", client.lastReconcile)
	}
}

func TestNormalizedRuntimeSSHPortDefaultsTo22(t *testing.T) {
	if got := normalizedRuntimeSSHPort(0); got != 22 {
		t.Fatalf("normalized port = %d, want 22", got)
	}
}
func TestActivityTransportPreservesOriginalIdentityAndDrainsClosedStream(t *testing.T) {
	ctx := context.Background()
	r, client, req, session := trackedTransport(t)
	// Receive the start now; later a mutable Job may belong to a newer intent.
	if end, err := r.collectExecutionEvents(ctx, "", req); err != nil || end != 0 {
		t.Fatalf("initial stream: end=%d err=%v", end, err)
	}
	for i := 0; i < 230; i++ {
		if err := session.Append(ctx, contract.ExecutionEvent{EventType: "output.chunk", Stream: "stdout", Text: "received output"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Finish(ctx, contract.RuntimeReconcileResponse{ObservedState: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	req.OperationID = "new-job-intent"
	req.RunID = "new-run"
	req.DesiredGeneration = 99
	end, err := r.collectExecutionEvents(ctx, "", req)
	if err != nil || end != 233 {
		t.Fatalf("closed stream end=%d err=%v", end, err)
	}
	if client.ackCount < 4 {
		t.Fatalf("expected paginated durable ACKs, got %d", client.ackCount)
	}
	rows, err := client.db.QueryContext(ctx, `SELECT operation_id,run_id,resources_json,initiator_json FROM activity_events WHERE source_id='agent:srv-a'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var op, run, resources, actor string
		if err = rows.Scan(&op, &run, &resources, &actor); err != nil {
			t.Fatal(err)
		}
		if op != "original-operation" || run != "original-run" || !strings.Contains(resources, "Original app name") || !strings.Contains(actor, "Original operator") {
			t.Fatalf("history changed: %s %s %s %s", op, run, resources, actor)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if replayEnd, err := r.collectExecutionEvents(ctx, "", req); err != nil || replayEnd != end {
		t.Fatalf("ACKed stream replay: %d %v", replayEnd, err)
	}
}

func TestActivityTransportRejectsWrongExecutionAssociationBeforeACK(t *testing.T) {
	r, client, req, _ := trackedTransport(t)
	client.transform = func(page contract.ExecutionEventsResponse) contract.ExecutionEventsResponse {
		page.Events[0].OperationID = "unrelated-operation"
		return page
	}
	if _, err := r.collectExecutionEvents(context.Background(), "", req); err == nil {
		t.Fatal("accepted unassociated event")
	}
	if client.ackCount != 0 {
		t.Fatal("invalid event was ACKed")
	}
	var count int
	if err := client.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE source_id='agent:srv-a'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid event committed: %d %v", count, err)
	}
}

func TestActivityTransportRecordsAndResolvesMissingEvidence(t *testing.T) {
	ctx := context.Background()
	r, client, req, session := trackedTransport(t)
	if err := session.Finish(ctx, contract.RuntimeReconcileResponse{ObservedState: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	client.transform = func(page contract.ExecutionEventsResponse) contract.ExecutionEventsResponse {
		page.Events = nil
		page.HasMore = false
		return page
	}
	if _, err := r.collectExecutionEvents(ctx, "", req); err == nil {
		t.Fatal("missing source records were ignored")
	}
	if _, err := r.collectExecutionEvents(ctx, "", req); err == nil {
		t.Fatal("repeated missing source records were ignored")
	}
	var gaps int
	if err := client.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE event_type='evidence.gap_detected'`).Scan(&gaps); err != nil || gaps != 1 {
		t.Fatalf("gap must persist once: %d %v", gaps, err)
	}
	if client.ackCount != 0 {
		t.Fatal("gap acknowledged before receipt")
	}
	client.transform = nil
	if end, err := r.collectExecutionEvents(ctx, "", req); err != nil || end == 0 {
		t.Fatalf("retransmit: %d %v", end, err)
	}
	var resolved int
	if err := client.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE event_type='evidence.gap_resolved' AND causation_event_id<>''`).Scan(&resolved); err != nil || resolved != 1 {
		t.Fatalf("missing resolution fact: %d %v", resolved, err)
	}
}

func TestTrackedCallDoesNotTreatUnclosedOutputAsComplete(t *testing.T) {
	r, client, req, _ := trackedTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := r.callTracked(ctx, "", req, client, contract.RuntimeReconcileRequest{}); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Fatalf("unclosed evidence accepted: %v", err)
	}
}
