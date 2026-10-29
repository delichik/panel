package logging

import (
	"context"
	"sync"
	"testing"
)

type recordingStore struct {
	mu     sync.Mutex
	upsert []ContextRecord
	touch  []struct {
		eventID string
		level   string
		msg     string
	}
}

func (s *recordingStore) Upsert(_ context.Context, row ContextRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsert = append(s.upsert, row)
	return nil
}

func (s *recordingStore) Touch(_ context.Context, eventID string, level string, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touch = append(s.touch, struct {
		eventID string
		level   string
		msg     string
	}{eventID: eventID, level: level, msg: msg})
	return nil
}

func TestLogDoesNotUpsertEventsWithoutRecordContext(t *testing.T) {
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	ctx := With(context.Background(), Context{EventID: "evt-log-only", Module: "test"})
	From(ctx).Info("plain diagnostic line")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsert) != 0 {
		t.Fatalf("log must not Upsert events, got %#v", store.upsert)
	}
	if len(store.touch) != 1 || store.touch[0].eventID != "evt-log-only" || store.touch[0].msg != "plain diagnostic line" {
		t.Fatalf("expected single Touch for existing-or-missing id, got %#v", store.touch)
	}
}

func TestRecordContextThenLogTouchesExistingProcess(t *testing.T) {
	store := &recordingStore{}
	SetContextStore(store)
	t.Cleanup(func() { SetContextStore(nil) })

	ctx := With(context.Background(), Context{EventID: "evt-process", Module: "deploy"})
	if err := RecordContext(ctx, ContextMeta{
		Origin:  "user",
		Title:   "Deploy agent",
		Summary: "started",
		State:   "running",
	}); err != nil {
		t.Fatal(err)
	}
	From(ctx).Warn("step failed briefly")

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsert) != 1 || store.upsert[0].EventID != "evt-process" || store.upsert[0].Title != "Deploy agent" {
		t.Fatalf("RecordContext should Upsert once: %#v", store.upsert)
	}
	if len(store.touch) != 1 || store.touch[0].eventID != "evt-process" || store.touch[0].msg != "step failed briefly" {
		t.Fatalf("follow-up log should Touch: %#v", store.touch)
	}
}
