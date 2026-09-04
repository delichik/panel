package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func createQueuedTask(t *testing.T, svc *Service, concurrencyKey, serverID string) Task {
	t.Helper()
	input := CreateInput{
		Type:           "test",
		ConcurrencyKey: concurrencyKey,
		ResourceType:   "server",
		ResourceID:     serverID,
		ServerID:       serverID,
	}
	task, err := svc.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	// Keep created_at ordering deterministic even when timestamps collide.
	time.Sleep(time.Millisecond)
	return task
}

func assertFirstActive(t *testing.T, svc *Service, key, wantID string) {
	t.Helper()
	got, ok, err := svc.FirstActiveByConcurrencyKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected an active task for key %q, got none", key)
	}
	if got.ID != wantID {
		t.Fatalf("expected first active %q for key %q, got %q", wantID, key, got.ID)
	}
}

func assertNoActive(t *testing.T, svc *Service, key string) {
	t.Helper()
	got, ok, err := svc.FirstActiveByConcurrencyKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected no active task for key %q, got %q", key, got.ID)
	}
}

func TestFirstActiveByConcurrencyKeyHandoffOnComplete(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	second := createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	assertFirstActive(t, svc, "k1", first.ID) // cache hit path

	if err := svc.Complete(ctx, first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	assertFirstActive(t, svc, "k1", second.ID)

	if err := svc.Complete(ctx, second.ID, "done"); err != nil {
		t.Fatal(err)
	}
	assertNoActive(t, svc, "k1")
}

func TestFirstActiveByConcurrencyKeyHandoffOnFail(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	second := createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	if err := svc.Fail(ctx, first.ID, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	assertFirstActive(t, svc, "k1", second.ID)
}

func TestFirstActiveByConcurrencyKeyInvalidatesOnBlock(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	second := createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	if err := svc.Block(ctx, first.ID, errors.New("blocked")); err != nil {
		t.Fatal(err)
	}
	assertFirstActive(t, svc, "k1", second.ID)
}

func TestFirstActiveByConcurrencyKeyInvalidatesOnCancel(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	second := createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	if err := svc.Cancel(ctx, first.ID, "user"); err != nil {
		t.Fatal(err)
	}
	assertFirstActive(t, svc, "k1", second.ID)
	if err := svc.Cancel(ctx, second.ID, "user"); err != nil {
		t.Fatal(err)
	}
	assertNoActive(t, svc, "k1")
}

func TestFirstActiveByConcurrencyKeyInvalidatesOnCancelByServer(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	_ = createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	count, err := svc.CancelByServer(ctx, "srv_1", "server removed")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 cancelled tasks, got %d", count)
	}
	assertNoActive(t, svc, "k1")
}

func TestFirstActiveByConcurrencyKeyInvalidatesOnExpireStaleQueued(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	_ = createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	now := time.Now().UTC().Add(2 * time.Minute)
	expired, err := svc.ExpireStaleQueued(ctx, now, time.Minute, []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if expired != 2 {
		t.Fatalf("expected 2 expired tasks, got %d", expired)
	}
	assertNoActive(t, svc, "k1")
}

func TestFirstActiveByConcurrencyKeyInvalidatesOnFailRunningWithoutExecution(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	input := CreateInput{Type: "test", ConcurrencyKey: "k1", Status: StatusRunning, ResourceType: "server", ResourceID: "srv_1", ServerID: "srv_1"}
	first, err := svc.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(ctx, CreateInput{Type: "test", ConcurrencyKey: "k1", ResourceType: "server", ResourceID: "srv_1", ServerID: "srv_1"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)

	assertFirstActive(t, svc, "k1", first.ID)

	// Simulate an orphaned running task: it is in the DB but not in the
	// in-process running execution registry.
	svc.runningMu.Lock()
	delete(svc.runningExecutions, first.ID)
	svc.runningMu.Unlock()

	failed, err := svc.FailRunningWithoutExecution(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("expected 1 orphaned task to fail, got %d", failed)
	}
	assertFirstActive(t, svc, "k1", second.ID)
}

func TestFirstActiveByConcurrencyKeyKeepsFirstOnFailRetryable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	createQueuedTask(t, svc, "k1", "srv_1")

	assertFirstActive(t, svc, "k1", first.ID)
	if err := svc.FailRetryable(ctx, first.ID, errors.New("retry later")); err != nil {
		t.Fatal(err)
	}
	// failed_retryable is still an active status, so the first task must not
	// change and the cache must stay valid.
	assertFirstActive(t, svc, "k1", first.ID)
}

func TestFirstActiveByConcurrencyKeyConcurrent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	first := createQueuedTask(t, svc, "k1", "srv_1")
	second := createQueuedTask(t, svc, "k1", "srv_1")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, ok, err := svc.FirstActiveByConcurrencyKey(ctx, "k1")
			if err != nil {
				t.Error(err)
				return
			}
			if !ok || got.ID != first.ID {
				t.Errorf("concurrent read expected first %q, got ok=%v id=%q", first.ID, ok, got.ID)
			}
		}()
	}
	wg.Wait()

	if err := svc.Complete(ctx, first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	assertFirstActive(t, svc, "k1", second.ID)
}

func TestFirstActiveByConcurrencyKeyEmptyKey(t *testing.T) {
	svc := newTestService(t)
	assertNoActive(t, svc, "")
}
