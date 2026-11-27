package server

import (
	"context"
	"strings"
	"testing"

	"panel/internal/modules/tasks"
)

// TestManualAgentDeployConflictsWithRunningTask guards a regression that reached
// production: a deploy task left in running (for example because its remote step
// timed out) made every later manual redeploy a silent no-op. The endpoint
// answered 202 with the very same task, the UI reported an accepted task, and
// nothing happened -- no new execution, no new log line, and no way for the
// operator to tell whether the click had registered.
func TestManualAgentDeployConflictsWithRunningTask(t *testing.T) {
	svc, taskSvc, serverID, _, _ := newDeployTestService(t, map[string]string{})

	running, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:         agentDeployTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		Status:       tasks.StatusRunning,
		Summary:      "already running",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.DeployAgent(context.Background(), serverID)
	if err == nil {
		t.Fatal("expected a manual redeploy against a running task to be refused")
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Fatalf("expected an agent_deploy_in_progress conflict, got %v", err)
	}

	// The refused request must not have queued another deployment.
	page, err := taskSvc.List(context.Background(), tasks.ListFilter{ServerID: serverID, Type: agentDeployTaskType})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != running.ID {
		t.Fatalf("expected only the existing running task, got %#v", page.Items)
	}
}
