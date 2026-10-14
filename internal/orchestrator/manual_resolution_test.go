package orchestrator

import (
	"context"
	"panel/internal/platform/activitylog"
	"sync"
	"testing"
)

func TestManualResolutionFencesVersionAndConcurrentDeclarations(t *testing.T) {
	db := newOrchestratorTestDB(t)
	insertOrchestratorTestRows(t, db)
	store := NewStore(db)
	ctx := context.Background()
	job, claimed, err := store.Claim(ctx, "job-1", "worker", 0)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	actor := activitylog.Actor{Kind: "user", ID: "admin", Name: "operator"}
	if _, err := store.ResolveUncertainManually(ctx, job, "succeeded", "checked", actor); err == nil {
		t.Fatal("non-unknown execution accepted")
	}
	if err := store.MarkUncertain(ctx, job, "response lost"); err != nil {
		t.Fatal(err)
	}
	stale := job
	stale.DesiredGeneration++
	if _, err := store.ResolveUncertainManually(ctx, stale, "succeeded", "checked", actor); err == nil {
		t.Fatal("wrong version accepted")
	}
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for _, outcome := range []string{"succeeded", "failed"} {
		wg.Add(1)
		go func(result string) {
			defer wg.Done()
			ok, err := store.ResolveUncertainManually(ctx, job, result, "checked", actor)
			results <- ok && err == nil
		}(outcome)
	}
	wg.Wait()
	close(results)
	successful := 0
	for ok := range results {
		if ok {
			successful++
		}
	}
	if successful != 1 {
		t.Fatalf("concurrent declarations committed %d outcomes", successful)
	}
	var records int
	if err := db.QueryRow(`SELECT count(*) FROM activity_events WHERE execution_id=? AND event_type='execution.verified'`, job.ExecutionID).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 1 {
		t.Fatalf("multiple manual outcomes: %d", records)
	}
}
