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
	"panel/internal/platform/logcontexts"
	"panel/internal/platform/logging"
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
	store := logcontexts.NewStore(c.db)
	rec, err := store.Get(ctx, req.ExecutionID)
	if err != nil {
		return err
	}
	if rec.LastSourceSeq < req.ThroughSourceSeq {
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
	logging.SetContextStore(logcontexts.NewStore(svc.db))
	t.Cleanup(func() { logging.SetContextStore(nil) })
	seedCtx := logging.With(context.Background(), logging.Context{
		StreamID: req.ExecutionID, Module: "agent-evidence", OperationID: req.OperationID, ExecutionID: req.ExecutionID, RequestID: req.RunID,
	})
	if err = logging.RecordContext(seedCtx, logging.ContextMeta{
		Origin:        logcontexts.OriginProcess,
		Title:         req.Action,
		Summary:       "execution.started",
		ActorJSON:     logging.MarshalActorJSON(map[string]string{"kind": "user", "id": "operator", "name": "Original operator"}),
		ResourcesJSON: logging.MarshalResourcesJSON([]map[string]any{{"resourceType": "application", "resourceId": req.ApplicationID, "nameSnapshot": "Original app name", "generation": 7}}),
	}); err != nil {
		t.Fatal(err)
	}
	if err = logcontexts.NewStore(svc.db).SetLastSourceSeq(seedCtx, req.ExecutionID, 0); err != nil {
		t.Fatal(err)
	}
	session, _, err := spool.Begin(context.Background(), contract.RuntimeReconcileRequest{OperationID: req.OperationID, RunID: req.RunID, JobID: req.JobID, ExecutionID: req.ExecutionID, ApplicationID: req.ApplicationID, InstanceID: req.InstanceID, ServerID: req.ServerID, Action: req.Action, DesiredGeneration: req.DesiredGeneration, DesiredSpecHash: req.DesiredSpecHash})
	if err != nil {
		t.Fatal(err)
	}
	return &serviceRuntimeReconciler{service: svc}, client, req, session
}

type recordingOperationQueue struct {
	serverIDs []string
}

func (q *recordingOperationQueue) Execute(ctx context.Context, serverID string, run func(context.Context) error) error {
	q.serverIDs = append(q.serverIDs, serverID)
	return run(ctx)
}

func TestRuntimeReconcileUsesSharedContainerOperationQueue(t *testing.T) {
	r, client, req, session := trackedTransport(t)
	queue := &recordingOperationQueue{}
	r.service.operationQueue = queue
	client.finishOnReconcile = session
	req.Action = "stop"
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(queue.serverIDs) != 1 || queue.serverIDs[0] != req.ServerID {
		t.Fatalf("reconcile must enter shared per-server queue, got %#v", queue.serverIDs)
	}
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

func TestRuntimeReconcileRejectsPrimitiveApplyWithoutReconcileClient(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	r := &serviceRuntimeReconciler{service: svc}
	req := control.ReconcileRequestRPC{
		OperationID: "op", RunID: "run", JobID: "job", ExecutionID: "exec",
		ApplicationID: "app", InstanceID: "instance", ServerID: "srv-a",
		Action: control.ActionApply, DesiredGeneration: 1, DesiredSpecHash: "hash",
		RenderedRuntimeSpec: []byte(`{"applicationId":"app","instanceId":"instance","containerName":"panel-app","image":"nginx"}`),
	}
	result, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "agent_capability_missing" || !result.Retryable {
		t.Fatalf("primitive apply must be refused for ORCH-CTRL-007, got %#v", result)
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
	rec, err := logcontexts.NewStore(client.db).Get(ctx, req.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.OperationID != "original-operation" || !strings.Contains(rec.ResourcesJSON, "Original app name") || !strings.Contains(rec.ActorJSON, "Original operator") {
		t.Fatalf("history changed: %#v", rec)
	}
	if rec.LastSourceSeq != end {
		t.Fatalf("last_source_seq=%d want %d", rec.LastSourceSeq, end)
	}
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
	rec, err := logcontexts.NewStore(client.db).Get(context.Background(), req.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.FinishedAt != "" || rec.LastSourceSeq != 0 {
		t.Fatalf("invalid event committed into log_contexts: %#v", rec)
	}
}

func TestResolveExecutionMissingReleasesUncertaintyToBoundedRetry(t *testing.T) {
	r, _, req, _ := trackedTransport(t)
	req.ExecutionID = "execution-not-on-agent"
	response, finished, err := r.ResolveExecution(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !finished || !response.Retryable || response.ErrorCode != "execution_evidence_missing" {
		t.Fatalf("missing execution did not become bounded retry: finished=%v response=%#v", finished, response)
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
	if client.ackCount != 0 {
		t.Fatal("gap acknowledged before receipt")
	}
	client.transform = nil
	if end, err := r.collectExecutionEvents(ctx, "", req); err != nil || end == 0 {
		t.Fatalf("retransmit: %d %v", end, err)
	}
	rec, err := logcontexts.NewStore(client.db).Get(ctx, req.ExecutionID)
	if err != nil || rec.LastSourceSeq == 0 || rec.FinishedAt == "" {
		t.Fatalf("expected closed evidence context: %#v %v", rec, err)
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
