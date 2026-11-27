package logging

import (
	"context"
	"sync/atomic"
)

// ContextRecord is the row shape upserted into events by RecordContext (process registration).
type ContextRecord struct {
	EventID       string
	Module        string
	Origin        string
	OperationID   string
	ExecutionID   string
	RequestID     string
	Title         string
	Summary       string
	State         string
	Level         string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	LogFile       string
	StartedAt     string
	UpdatedAt     string
	FinishedAt    string
}

// ContextStore persists process event metadata.
// Upsert registers/updates an events row (explicit process start via RecordContext).
// Touch refreshes an existing row from ordinary log lines without creating one.
// Implementation lives under platform/events; set via SetContextStore at bootstrap.
type ContextStore interface {
	Upsert(ctx context.Context, row ContextRecord) error
	Touch(ctx context.Context, eventID string, level string, msg string) error
}

type contextStoreBox struct {
	store ContextStore
}

var contextStore atomic.Pointer[contextStoreBox]

// SetContextStore registers the AppDB-backed event store (may be nil to clear).
func SetContextStore(store ContextStore) {
	contextStore.Store(&contextStoreBox{store: store})
}

func getContextStore() ContextStore {
	box := contextStore.Load()
	if box == nil {
		return nil
	}
	return box.store
}
