package logging

import (
	"context"
	"sync/atomic"
)

// ContextRecord is the row shape upserted into log_contexts on each valid log write.
type ContextRecord struct {
	StreamID      string
	Module        string
	Origin        string
	OperationID   string
	ExecutionID   string
	RequestID     string
	Title         string
	Summary       string
	Result        string
	HadError      bool
	ActorJSON     string
	ResourcesJSON string
	LastSourceSeq int64
	LogFile       string
	StartedAt     string
	UpdatedAt     string
	FinishedAt    string
}

// ContextStore persists stream metadata for unified file+ctx logging.
// Implementation lives under platform/logcontexts; set via SetContextStore at bootstrap.
type ContextStore interface {
	Upsert(ctx context.Context, row ContextRecord) error
	Touch(ctx context.Context, streamID string, level string, msg string) error
}

type contextStoreBox struct {
	store ContextStore
}

var contextStore atomic.Pointer[contextStoreBox]

// SetContextStore registers the AppDB-backed context store (may be nil to clear).
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
