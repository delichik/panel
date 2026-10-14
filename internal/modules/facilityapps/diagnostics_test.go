package facilityapps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	server "panel/internal/modules/servers"
	"panel/internal/orchestrator"
)

type diagnosticLogAgent struct {
	fakeStorageAgent
	calls int
	fail  bool
}

func (a *diagnosticLogAgent) DockerContainerLogs(_ context.Context, _ string, id string, tail int) (agentcontract.DockerContainerLogsResponse, error) {
	a.calls++
	if a.fail {
		return agentcontract.DockerContainerLogsResponse{}, errors.New("token=do-not-expose")
	}
	return agentcontract.DockerContainerLogsResponse{ContainerID: id, Logs: `2026/09/15 05:36:16 [error] 30#30: *6426 panel-web could not be resolved (2: Server failure), client: 1.2.3.4, server: web.example.test`}, nil
}

func TestProxyRequestDiagnosticsExplainAndGroupErrors(t *testing.T) {
	dns := `2026/09/15 05:36:16 [error] 30#30: *6426 panel-vaultwarden could not be resolved (2: Server failure), client: 1.2.3.4, server: vault.example.test, request: "GET /?token=private HTTP/2.0", host: "vault.example.test"`
	tls := `2026/09/15 05:41:39 [error] 29#29: *6428 upstream SSL certificate verify error: (2:unable to get issuer certificate) while SSL handshaking to upstream, client: 1.2.3.4, server: app.example.test, request: "GET /secret HTTP/1.1", upstream: "https://user:password@10.0.0.2:443/private?token=secret", host: "app.example.test"`
	issues, truncated := parseProxyRequestDiagnostics(dns + "\n" + dns + "\n" + tls + "\n" + `2026/09/15 05:42:00 [error] 1#1: unclassified private request /?token=secret`)
	if truncated || len(issues) != 3 {
		t.Fatalf("issues=%#v", issues)
	}
	if issues[0].Code != "upstream_name_resolution_failed" || issues[0].Count != 2 || issues[0].Upstream != "panel-vaultwarden" {
		t.Fatalf("DNS error not explained: %#v", issues[0])
	}
	if issues[1].Code != "upstream_tls_untrusted" || issues[1].Upstream != "10.0.0.2:443" || issues[1].Domain != "app.example.test" {
		t.Fatalf("TLS error not explained: %#v", issues[1])
	}
	for _, issue := range issues {
		if strings.Contains(issue.Evidence, "private") || strings.Contains(issue.Evidence, "secret") || strings.Contains(issue.Evidence, "1.2.3.4") {
			t.Fatal("request data leaked into diagnostic evidence")
		}
	}
	if issues[2].Code != "proxy_request_error" || issues[2].Evidence != "" {
		t.Fatal("unknown error must use safe fallback")
	}
	if clean, _ := parseProxyRequestDiagnostics(`1.2.3.4 "GET /.env HTTP/1.1" 404`); len(clean) != 0 {
		t.Fatal("access log treated as deployment error")
	}
	if spoofed, _ := parseProxyRequestDiagnostics(`1.2.3.4 - - [15/Sep/2026:05:40:13 +0000] "GET /?token=secret HTTP/2.0" 404 0 "-" "[error] upstream timed out"`); len(spoofed) != 0 {
		t.Fatal("access log can spoof a proxy error")
	}
}

// FAC-RP-012: a later successful node must not hide another node's failure.
func TestFacilityDeploymentDiagnosticsKeepsEachNodesFailure(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.db.Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('diagnostic-credential','diagnostic','password','root',?,?)`, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO applications(id,name,spec_yaml,job_id,created_at,updated_at) VALUES(?,?,'','',?,?)`, proxyApplicationID, "facility", time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	planner := orchestrator.NewPlanner(orchestrator.NewStore(svc.db))
	var firstID string
	for _, serverID := range []string{"failed-node", "successful-node"} {
		if _, err := svc.db.Exec(`INSERT INTO servers(id,name,host,port,credential_id,created_at,updated_at) VALUES(?,?,?,22,'diagnostic-credential',?,?)`, serverID, serverID, "127.0.0.1", time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
		result, err := planner.Plan(ctx, orchestrator.PlanInput{ApplicationID: proxyApplicationID, ServerID: serverID, InstanceID: proxyApplicationID + "-" + serverID, DesiredState: "running", Action: "apply", DesiredGeneration: 1, IntentID: "intent-" + serverID})
		if err != nil {
			t.Fatal(err)
		}
		state, code := "succeeded", ""
		if serverID == "failed-node" {
			state, code = "failed_retryable", "container_not_running"
			firstID = result.Job.ID
		}
		if _, err := svc.db.Exec(`UPDATE jobs SET state=?,error_code=?,error_message='startup failed',error_detail='exitCode=1',attempts=2,next_run_at=? WHERE id=?`, state, code, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), result.Job.ID); err != nil {
			t.Fatal(err)
		}
	}
	deployments, err := svc.deploymentDiagnostics(ctx, []string{"failed-node", "successful-node", "not-planned"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 3 {
		t.Fatalf("missing node: %#v", deployments)
	}
	op := aggregateDeploymentOperation(deployments)
	if op == nil || op.ID != firstID || op.ErrorCode != "container_not_running" || op.Attempt != 2 || op.NextRunAt == nil || op.Generation != 1 || op.Type != "apply" {
		t.Fatalf("failure hidden: %#v", op)
	}
	if _, err := svc.DiagnoseReverseProxy(ctx, "unrelated-node"); err == nil {
		t.Fatal("diagnosis must reject nodes without a facility instance")
	}
	agent := &diagnosticLogAgent{}
	svc.agent = agent
	svc.servers = facilityTestServers{items: map[string]server.Server{"failed-node": {ID: "failed-node", Traits: map[string]string{agentcontract.TraitURL: "https://node:9786", agentcontract.TraitStatus: agentcontract.StatusCompatible}}}}
	if _, err := svc.db.Exec(`UPDATE application_instances SET observed_container_id='container-a' WHERE server_id='failed-node'`); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := svc.DiagnoseReverseProxy(ctx, "failed-node")
		if err != nil || result.Status != "sampled" || len(result.Issues) != 1 {
			t.Fatalf("diagnostics failed: %#v %v", result, err)
		}
	}
	agent.fail = true
	if result, err := svc.DiagnoseReverseProxy(ctx, "failed-node"); err != nil || result.Status != "unavailable" || len(result.Issues) != 0 {
		t.Fatalf("read failure did not degrade safely: %#v %v", result, err)
	}
	if err := svc.db.QueryRow(`SELECT count(*) FROM activity_events`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || agent.calls != 3 {
		t.Fatal("diagnostics wrote activity events or retried reads")
	}
	if _, err := svc.db.Exec(`UPDATE application_instances SET desired_state='purged' WHERE server_id='failed-node'`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE jobs SET state='failed' WHERE id=?`, firstID); err != nil {
		t.Fatal(err)
	}
	remaining, err := svc.deploymentDiagnostics(ctx, []string{"successful-node"})
	if err != nil {
		t.Fatal(err)
	}
	if op := aggregateDeploymentOperation(remaining); op == nil || op.ID != firstID {
		t.Fatal("failed cleanup disappeared after removing gateway")
	}
}

func TestFacilityPendingOperationNotHiddenByNodeWithoutJob(t *testing.T) {
	pending := FacilityDeployment{Operation: &applications.LifecycleOperation{ID: "pending-job", Status: "pending"}}
	for _, nodes := range [][]FacilityDeployment{{{}, pending}, {pending, {}}} {
		if op := aggregateDeploymentOperation(nodes); op == nil || op.ID != "pending-job" {
			t.Fatal("node order hid pending operation")
		}
	}
}
