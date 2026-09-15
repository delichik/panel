package applications

import (
	"context"
	"errors"
	"strings"
	"testing"

	appruntime "panel/internal/modules/applications/runtime"
	panelerr "panel/internal/platform/errors"
)

// APP-PLAN-001: a failure before any Job exists is durable, attributed and
// deduplicated, without rewriting the user's configuration or runtime state.
func TestPlanningFailureIsVisibleWithoutJobAndClearsAfterSuccessfulPlan(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/app.conf\n    target: /etc/app.conf\n"
	if _, err := svc.db.Exec(`UPDATE applications SET enabled=1,spec_yaml=? WHERE id=?`, invalid, app.ID); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := DeploymentPlanRequest{ApplicationID: app.ID, ObservedRuntimeDrift: true, TriggerType: "agent_report"}
	for i := 0; i < 2; i++ {
		if _, err := svc.PlanApplicationDeployment(ctx, req); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
	current, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	issue := current.PlanningError
	if issue == nil || issue.Code != "application_file_name_invalid" || issue.FileName != "config/app.conf" || issue.Field != "mounts.source" || issue.Retryable || issue.OperationID == "" {
		t.Fatalf("missing diagnostic: %#v", issue)
	}
	if current.Version != before.Version || !current.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("planning error changed user configuration metadata")
	}
	if jobs := jobsForApplication(t, svc, app.ID); len(jobs) != 0 {
		t.Fatalf("failed plan created jobs: %#v", jobs)
	}
	var events, executions int
	if err := svc.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(execution_id<>''),0) FROM activity_events WHERE event_type='operation.failed' AND operation_id=?`, issue.OperationID).Scan(&events, &executions); err != nil || events != 1 || executions != 0 {
		t.Fatalf("failure evidence: events=%d executions=%d err=%v", events, executions, err)
	}
	page, err := svc.ListSummaries(ctx, 1, 20, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].PlanningError == nil {
		t.Fatalf("list lost planning error: %#v %v", page, err)
	}
	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil || runtime.PlanningError == nil || runtime.Operation != nil {
		t.Fatalf("runtime lost planning-only error: %#v %v", runtime, err)
	}
	if _, err := svc.db.Exec(`UPDATE applications SET spec_yaml=?,version=version+1 WHERE id=?`, "name: web\nimage: nginx\n", app.ID); err != nil {
		t.Fatal(err)
	}
	result, err := svc.PlanApplicationDeployment(ctx, req)
	if err != nil || len(result.JobIDs) == 0 {
		t.Fatalf("corrected plan failed: %#v %v", result, err)
	}
	current, err = svc.Get(ctx, app.ID)
	if err != nil || current.PlanningError != nil {
		t.Fatalf("stale planning error: %#v %v", current.PlanningError, err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM activity_events WHERE operation_id=? AND event_type='operation.failed'`, issue.OperationID).Scan(&events); err != nil || events != 1 {
		t.Fatal("historical failure was lost")
	}
}

// APP-PLAN-001: the public manual sync entry must record pre-Job validation
// failures, including for an application that was disabled before the request.
func TestManualDeployPersistsPlanningFailure(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/app.conf\n    target: /etc/app.conf\n"
	if _, err := svc.db.Exec(`UPDATE applications SET spec_yaml=? WHERE id=?`, invalid, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Deploy(ctx, app.ID); err == nil {
		t.Fatal("invalid manual deployment accepted")
	}
	current, err := svc.Get(ctx, app.ID)
	if err != nil || current.PlanningError == nil || current.PlanningError.FileName != "config/app.conf" {
		t.Fatalf("manual sync lost its diagnostic: %#v %v", current.PlanningError, err)
	}
	if len(jobsForApplication(t, svc, app.ID)) != 0 {
		t.Fatal("invalid manual deployment created execution jobs")
	}
}

func TestSatisfiedPlanClearsDiagnosticButSkippedPlanDoesNot(t *testing.T) {
	svc, _, servers, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`UPDATE jobs SET state='succeeded',finished_at=updated_at WHERE application_id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	for serverID := range servers.items {
		markRuntimeInstanceSatisfied(t, svc, app.ID, serverID, app.Generation, app.SpecHash, appruntime.StatusRunning, "")
	}
	if err := svc.recordPlanningOutcome(ctx, app, DeploymentPlanRequest{}, errors.New("temporary planning error")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, ServerIDs: []string{"no-matching-target"}}); err != nil {
		t.Fatal(err)
	}
	current, err := svc.Get(ctx, app.ID)
	if err != nil || current.PlanningError == nil {
		t.Fatal("skipped plan cleared diagnostic")
	}
	result, err := svc.Deploy(ctx, app.ID)
	if err != nil || !result.NoChange || result.Application.PlanningError != nil {
		t.Fatalf("validated no-change plan did not clear diagnostic: %#v %v", result, err)
	}
}

func TestPlanningOutcomeRedactsSecretsAndDoesNotOverwriteNewerConfiguration(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	req := DeploymentPlanRequest{ApplicationID: app.ID}
	err = svc.recordPlanningOutcome(ctx, app, req, panelerr.Validation("invalid_configuration", "password=private-test-value"))
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := svc.db.QueryRow(`SELECT planning_error_json FROM applications WHERE id=?`, app.ID).Scan(&raw); err != nil || strings.Contains(raw, "private-test-value") {
		t.Fatalf("unsafe error: %s %v", raw, err)
	}
	if err := svc.recordPlanningOutcome(ctx, app, req, errors.New("database path /secret/internal.db")); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT planning_error_json FROM applications WHERE id=?`, app.ID).Scan(&raw); err != nil || strings.Contains(raw, "/secret/") {
		t.Fatalf("unsafe internal error: %s %v", raw, err)
	}
	if _, err := svc.db.Exec(`UPDATE applications SET version=version+1 WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.recordPlanningOutcome(ctx, app, req, nil); err != nil {
		t.Fatal(err)
	}
	var after string
	if err := svc.db.QueryRow(`SELECT planning_error_json FROM applications WHERE id=?`, app.ID).Scan(&after); err != nil || after != raw {
		t.Fatal("outdated attempt changed newer configuration's diagnostic")
	}
}

// APP-RUN-004: uncertainty remains running for fencing, but must not be
// presented as ordinary deployment progress by either list or runtime APIs.
func TestUncertainJobIsVisibleInRuntimeAndApplicationList(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) == 0 {
		t.Fatal("no jobs")
	}
	if _, err := svc.db.Exec(`UPDATE jobs SET state='running',error_class='uncertainty',error_code='execution_result_unknown',error_message='Result not confirmed' WHERE id=?`, jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil || runtime.Status != "needs_attention" || runtime.Operation == nil || runtime.Operation.ErrorClass != "uncertainty" || runtime.Operation.Status != "running" {
		t.Fatalf("uncertainty hidden: %#v %v", runtime, err)
	}
	page, err := svc.ListSummaries(ctx, 1, 20, "")
	if err != nil || page.Items[0].RuntimeStatus != "needs_attention" {
		t.Fatalf("list hides uncertainty: %#v %v", page, err)
	}
}
