package logging

import (
	"context"
	"strings"
	"testing"
)

func TestEmitProcessUpsertsReadableRowAndLogs(t *testing.T) {
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	_ = EmitProcess(context.Background(), ProcessSpec{
		EventID:     "intent-1",
		Module:      "orchestrator",
		OperationID: "intent-1",
		ExecutionID: "exec-1",
		Title:       "Deploy hermes → Oracle Phoenix",
		Summary:     "Entered monitor: dispatch wait exceeded (15s)",
		State:       ProcessRunning,
		EventType:   "deploy.monitor_entered",
		Resources: []Resource{
			ResourceRef("application", "app-1", "hermes", "target"),
		},
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsert) != 1 {
		t.Fatalf("expected one Upsert, got %#v", store.upsert)
	}
	row := store.upsert[0]
	if row.EventID != "intent-1" || row.Title != "Deploy hermes → Oracle Phoenix" {
		t.Fatalf("bad title/id: %#v", row)
	}
	if row.Summary == "" || row.Summary == row.Title || row.Summary == "execution evidence" {
		t.Fatalf("summary must be readable progress: %#v", row)
	}
	if row.State != ProcessRunning || row.OperationID != "intent-1" || row.ExecutionID != "exec-1" {
		t.Fatalf("correlation/state: %#v", row)
	}
	if row.ResourcesJSON == "" || !strings.Contains(row.ResourcesJSON, "hermes") {
		t.Fatalf("resources must include name: %s", row.ResourcesJSON)
	}
}

func TestTouchProcessUpdatesSummary(t *testing.T) {
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	_ = EmitProcess(context.Background(), ProcessSpec{
		EventID: "intent-2",
		Module:  "orchestrator",
		Title:   "Deploy app → srv",
		Summary: "Claimed job",
		State:   ProcessRunning,
	})
	_ = TouchProcess(context.Background(), ProcessSpec{
		EventID:  "intent-2",
		Module:   "orchestrator",
		Title:    "Deploy app → srv",
		Summary:  "Succeeded; observed running",
		State:    ProcessSucceeded,
		Finished: true,
	})
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsert) != 2 {
		t.Fatalf("expected two Upserts, got %d", len(store.upsert))
	}
	last := store.upsert[1]
	if last.State != ProcessSucceeded || last.Summary != "Succeeded; observed running" || last.FinishedAt == "" {
		t.Fatalf("touch must finish process: %#v", last)
	}
}

func TestDeployTitle(t *testing.T) {
	if got := DeployTitle("apply", "hermes", "Oracle Phoenix"); got != "Deploy hermes → Oracle Phoenix" {
		t.Fatalf("got %q", got)
	}
	if got := DeployTitle("stop", "", ""); got != "Stop application → server" {
		t.Fatalf("got %q", got)
	}
}
