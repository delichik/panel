package notifications

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func latestSeq(t *testing.T, h *Hub) uint64 {
	t.Helper()
	items, cursor := h.Wait(context.Background(), 0, 0)
	if len(items) != 0 {
		t.Fatalf("baseline must not replay history: %#v", items)
	}
	return cursor
}

func TestWaitBaselineNeverReplaysHistory(t *testing.T) {
	h := NewHub()
	if items, cursor := h.Wait(context.Background(), 0, 0); len(items) != 0 || cursor != 0 {
		t.Fatalf("empty hub baseline = (%#v, %d), want (nil, 0)", items, cursor)
	}

	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: "evt-b", Module: "deploy", State: "running"})
	if got := latestSeq(t, h); got != 2 {
		t.Fatalf("latest = %d, want 2", got)
	}

	// A client ahead of the server (e.g. Panel restarted) also gets a baseline.
	if items, cursor := h.Wait(context.Background(), 99, 0); len(items) != 0 || cursor != 2 {
		t.Fatalf("after>latest = (%#v, %d), want (nil, 2)", items, cursor)
	}
}

func TestWaitReturnsIncrementImmediately(t *testing.T) {
	h := NewHub()
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", Title: "Deploy app", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: "evt-b", Module: "deploy", Title: "Deploy app", State: "running"})
	_, cursor := h.Wait(context.Background(), 0, 0)
	if cursor != 2 {
		t.Fatalf("baseline cursor = %d, want 2", cursor)
	}

	h.PublishProcess(ProcessEvent{
		EventID:  "evt-a",
		Module:   "deploy",
		Title:    "Deploy app",
		Summary:  "finished",
		State:    "succeeded",
		Level:    "info",
		Origin:   "user",
		HadError: false,
	})

	items, next := h.Wait(context.Background(), cursor, 0)
	if len(items) != 1 || next != 3 {
		t.Fatalf("items = %#v, cursor = %d; want one item and cursor 3", items, next)
	}
	got := items[0]
	if got.Seq != 3 || got.EventID != "evt-a" || got.Module != "deploy" || got.Title != "Deploy app" {
		t.Fatalf("notification = %#v", got)
	}
	if got.State != "succeeded" || got.Level != "info" || got.Origin != "user" || got.HadError {
		t.Fatalf("terminal fields = %#v", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.UpdatedAt); err != nil {
		t.Fatalf("updatedAt %q is not RFC3339Nano: %v", got.UpdatedAt, err)
	}
}

func TestWaitWakesOnPublish(t *testing.T) {
	h := NewHub()
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "running"})
	_, cursor := h.Wait(context.Background(), 0, 0)
	if cursor != 1 {
		t.Fatalf("baseline cursor = %d, want 1", cursor)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "succeeded"})
	}()

	startedAt := time.Now()
	items, next := h.Wait(context.Background(), cursor, 5*time.Second)
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("waiter woke after %s, publish should wake it immediately", elapsed)
	}
	if len(items) != 1 || items[0].State != "succeeded" || next != 2 {
		t.Fatalf("items = %#v, cursor = %d", items, next)
	}
}

func TestWaitTimeoutAndContextCancellation(t *testing.T) {
	h := NewHub()
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "running"})

	startedAt := time.Now()
	items, cursor := h.Wait(context.Background(), 1, 60*time.Millisecond)
	if elapsed := time.Since(startedAt); elapsed < 50*time.Millisecond {
		t.Fatalf("wait returned after %s, want >= timeout", elapsed)
	}
	if len(items) != 0 || cursor != 1 {
		t.Fatalf("timeout result = (%#v, %d), want (nil, 1)", items, cursor)
	}

	// timeout <= 0 is an immediate non-blocking check.
	startedAt = time.Now()
	items, cursor = h.Wait(context.Background(), 1, 0)
	if elapsed := time.Since(startedAt); elapsed > time.Second || len(items) != 0 || cursor != 1 {
		t.Fatalf("non-blocking check = (%#v, %d) after %s", items, cursor, elapsed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startedAt = time.Now()
	items, cursor = h.Wait(ctx, 1, 5*time.Second)
	if elapsed := time.Since(startedAt); elapsed > time.Second || len(items) != 0 || cursor != 1 {
		t.Fatalf("cancelled wait = (%#v, %d) after %s", items, cursor, elapsed)
	}
}

func TestPublishProcessDedupsStartAndTerminal(t *testing.T) {
	h := NewHub()

	// First non-terminal transition publishes the start once.
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "queued"})
	if got := latestSeq(t, h); got != 1 {
		t.Fatalf("latest after start = %d, want 1", got)
	}
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: ""})
	if got := latestSeq(t, h); got != 1 {
		t.Fatalf("repeated running must not publish: latest = %d, want 1", got)
	}

	// Terminal publishes exactly once, then the event is closed.
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "succeeded"})
	if got := latestSeq(t, h); got != 2 {
		t.Fatalf("latest after terminal = %d, want 2", got)
	}
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "failed"})
	h.PublishProcess(ProcessEvent{EventID: "evt-a", Module: "deploy", State: "running"})
	if got := latestSeq(t, h); got != 2 {
		t.Fatalf("post-terminal updates must not publish: latest = %d, want 2", got)
	}

	// A lifecycle whose first observed transition is terminal still publishes
	// that single terminal notification (fast operations have no start tick).
	h.PublishProcess(ProcessEvent{EventID: "evt-b", Module: "deploy", State: "failed"})
	if got := latestSeq(t, h); got != 3 {
		t.Fatalf("terminal-first latest = %d, want 3", got)
	}
	h.PublishProcess(ProcessEvent{EventID: "evt-b", Module: "deploy", State: "succeeded"})
	if got := latestSeq(t, h); got != 3 {
		t.Fatalf("terminal-first must not publish twice: latest = %d, want 3", got)
	}

	// Every documented terminal state closes a lifecycle.
	for i, state := range []string{"succeeded", "success", "failed", "failure", "cancelled", "superseded", "partial", "partial_success"} {
		h.PublishProcess(ProcessEvent{EventID: fmt.Sprintf("evt-terminal-%d", i), Module: "deploy", State: state})
		h.PublishProcess(ProcessEvent{EventID: fmt.Sprintf("evt-terminal-%d", i), Module: "deploy", State: "succeeded"})
	}
	if got, want := latestSeq(t, h), uint64(11); got != want {
		t.Fatalf("terminal states latest = %d, want %d", got, want)
	}
}

func TestPublishProcessIgnoresHTTPAndEmptyEventID(t *testing.T) {
	h := NewHub()
	h.PublishProcess(ProcessEvent{EventID: "evt-http", Module: "http", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: "evt-http-space", Module: " http ", State: "succeeded"})
	h.PublishProcess(ProcessEvent{EventID: "", Module: "deploy", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: "   ", Module: "deploy", State: "running"})
	if got := latestSeq(t, h); got != 0 {
		t.Fatalf("http/empty event notifications must be ignored: latest = %d", got)
	}

	// Non-http process events still publish; ids and modules are trimmed.
	h.PublishProcess(ProcessEvent{EventID: " evt-deploy ", Module: " deploy ", State: "running"})
	h.PublishProcess(ProcessEvent{EventID: " evt-deploy ", Module: " deploy ", State: "succeeded"})
	items, cursor := h.Wait(context.Background(), 1, 0)
	if len(items) != 1 || cursor != 2 {
		t.Fatalf("items = %#v, cursor = %d; want one item and cursor 2", items, cursor)
	}
	if items[0].EventID != "evt-deploy" || items[0].Module != "deploy" {
		t.Fatalf("trimmed notification = %#v", items[0])
	}
}

func TestHubRingBufferKeepsNewestNotifications(t *testing.T) {
	h := NewHub()
	total := BufferSize + 44
	for i := 0; i < total; i++ {
		h.PublishProcess(ProcessEvent{
			EventID: fmt.Sprintf("evt-%d", i),
			Module:  "deploy",
			State:   "running",
		})
	}
	if got := latestSeq(t, h); got != uint64(total) {
		t.Fatalf("latest = %d, want %d", got, total)
	}

	items, cursor := h.Wait(context.Background(), 44, 0)
	if len(items) != BufferSize {
		t.Fatalf("buffered items = %d, want %d (oldest dropped)", len(items), BufferSize)
	}
	if items[0].Seq != 45 || items[len(items)-1].Seq != uint64(total) || cursor != uint64(total) {
		t.Fatalf("buffer window = [%d..%d], cursor = %d", items[0].Seq, items[len(items)-1].Seq, cursor)
	}
}

func TestHubTrackingTableIsBounded(t *testing.T) {
	h := NewHub()
	for i := 0; i < MaxTrackedEvents+128; i++ {
		h.PublishProcess(ProcessEvent{EventID: fmt.Sprintf("evt-%d", i), Module: "deploy", State: "running"})
	}
	h.mu.Lock()
	tracked := len(h.track)
	h.mu.Unlock()
	if tracked > MaxTrackedEvents {
		t.Fatalf("tracked events = %d, want <= %d", tracked, MaxTrackedEvents)
	}
}

func TestHubConcurrentPublishAndWait(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waiters sync.WaitGroup
	for w := 0; w < 2; w++ {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			for i := 0; i < 20; i++ {
				h.Wait(ctx, 1, 10*time.Millisecond)
			}
		}()
	}

	const publishers = 8
	const perPublisher = 25
	var pub sync.WaitGroup
	for g := 0; g < publishers; g++ {
		pub.Add(1)
		go func(g int) {
			defer pub.Done()
			for i := 0; i < perPublisher; i++ {
				id := fmt.Sprintf("evt-%d-%d", g, i)
				h.PublishProcess(ProcessEvent{EventID: id, Module: "deploy", State: "running"})
				h.PublishProcess(ProcessEvent{EventID: id, Module: "deploy", State: "succeeded"})
			}
		}(g)
	}
	pub.Wait()
	waiters.Wait()

	if got, want := latestSeq(t, h), uint64(publishers*perPublisher*2); got != want {
		t.Fatalf("latest = %d, want %d", got, want)
	}
}

func TestResetForTestClearsDefaultHub(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)
	PublishProcess(ProcessEvent{EventID: "evt-reset", Module: "deploy", State: "running"})
	if _, cursor := Wait(context.Background(), 0, 0); cursor != 1 {
		t.Fatalf("published cursor = %d, want 1", cursor)
	}
	ResetForTest()
	if items, cursor := Wait(context.Background(), 0, 0); len(items) != 0 || cursor != 0 {
		t.Fatalf("after reset = (%#v, %d), want (nil, 0)", items, cursor)
	}
}
