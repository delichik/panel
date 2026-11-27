package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

type contextKey struct{}

// Context carries a required event id and module for aggregation.
// StreamID is accepted as a deprecated alias for EventID during migration.
type Context struct {
	EventID     string
	StreamID    string // deprecated: prefer EventID; normalized() syncs both
	Module      string
	OperationID string
	ExecutionID string
	RequestID   string
}

func (c Context) valid() bool {
	id := strings.TrimSpace(c.EventID)
	if id == "" {
		id = strings.TrimSpace(c.StreamID)
	}
	return id != "" && strings.TrimSpace(c.Module) != ""
}

func (c Context) normalized() Context {
	c.EventID = strings.TrimSpace(c.EventID)
	c.StreamID = strings.TrimSpace(c.StreamID)
	if c.EventID == "" {
		c.EventID = c.StreamID
	}
	if c.StreamID == "" {
		c.StreamID = c.EventID
	}
	c.Module = strings.TrimSpace(c.Module)
	c.OperationID = strings.TrimSpace(c.OperationID)
	c.ExecutionID = strings.TrimSpace(c.ExecutionID)
	c.RequestID = strings.TrimSpace(c.RequestID)
	return c
}

// NewEventID returns a unique event identifier.
func NewEventID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "event-fallback"
	}
	return "event-" + hex.EncodeToString(buf[:])
}

// NewStreamID is a deprecated alias for NewEventID.
func NewStreamID() string { return NewEventID() }

// With attaches logging Context to ctx. Empty StreamID/Module are rejected by From.
func With(ctx context.Context, logCtx Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	base, _ := ctx.Value(contextKey{}).(Context)
	merged := mergeContext(base, logCtx.normalized())
	return context.WithValue(ctx, contextKey{}, merged)
}

// Merge keeps the parent EventID unless the child explicitly sets a new one.
func mergeContext(base, overlay Context) Context {
	out := base
	if overlay.EventID != "" || overlay.StreamID != "" {
		id := overlay.EventID
		if id == "" {
			id = overlay.StreamID
		}
		out.EventID = id
		out.StreamID = id
	}
	if overlay.Module != "" {
		out.Module = overlay.Module
	}
	if overlay.OperationID != "" {
		out.OperationID = overlay.OperationID
	}
	if overlay.ExecutionID != "" {
		out.ExecutionID = overlay.ExecutionID
	}
	if overlay.RequestID != "" {
		out.RequestID = overlay.RequestID
	}
	return out
}

// From returns a Logger bound to the logging Context in ctx.
// Missing stream/module yields a discard logger (and bumps an internal counter).
func From(ctx context.Context) *Logger {
	if ctx == nil {
		return invalidLogger()
	}
	logCtx, _ := ctx.Value(contextKey{}).(Context)
	logCtx = logCtx.normalized()
	if !logCtx.valid() {
		return invalidLogger()
	}
	return &Logger{goCtx: ctx, ctx: logCtx}
}

// Lookup returns the logging Context if present and valid.
func Lookup(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	logCtx, _ := ctx.Value(contextKey{}).(Context)
	logCtx = logCtx.normalized()
	if !logCtx.valid() {
		return Context{}, false
	}
	return logCtx, true
}

// TestContext attaches a valid test stream for unit tests.
func TestContext(t testing.TB) context.Context {
	t.Helper()
	return With(context.Background(), Context{
		StreamID: NewStreamID(),
		Module:   "test",
	})
}

// Background returns a background context with a named module stream.
func Background(module string) context.Context {
	return With(context.Background(), Context{
		StreamID: NewStreamID(),
		Module:   module,
	})
}

// Ensure attaches a valid event id when missing, and overlays module/ids when set.
func Ensure(ctx context.Context, logCtx Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing, ok := Lookup(ctx)
	if !ok {
		logCtx = logCtx.normalized()
		if logCtx.EventID == "" {
			logCtx.EventID = NewEventID()
			logCtx.StreamID = logCtx.EventID
		}
		if strings.TrimSpace(logCtx.Module) == "" {
			logCtx.Module = "process"
		}
		return With(ctx, logCtx)
	}
	logCtx = logCtx.normalized()
	if logCtx.EventID == "" {
		logCtx.EventID = existing.EventID
		if logCtx.EventID == "" {
			logCtx.EventID = existing.StreamID
		}
		logCtx.StreamID = logCtx.EventID
	}
	if logCtx.Module == "" {
		logCtx.Module = existing.Module
	}
	return With(ctx, logCtx)
}
