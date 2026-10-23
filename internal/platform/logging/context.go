package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

type contextKey struct{}

// Context carries a required log stream and module for aggregation.
type Context struct {
	StreamID    string
	Module      string
	OperationID string
	ExecutionID string
	RequestID   string
}

func (c Context) valid() bool {
	return strings.TrimSpace(c.StreamID) != "" && strings.TrimSpace(c.Module) != ""
}

func (c Context) normalized() Context {
	c.StreamID = strings.TrimSpace(c.StreamID)
	c.Module = strings.TrimSpace(c.Module)
	c.OperationID = strings.TrimSpace(c.OperationID)
	c.ExecutionID = strings.TrimSpace(c.ExecutionID)
	c.RequestID = strings.TrimSpace(c.RequestID)
	return c
}

// NewStreamID returns a unique stream identifier.
func NewStreamID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "stream-fallback"
	}
	return "stream-" + hex.EncodeToString(buf[:])
}

// With attaches logging Context to ctx. Empty StreamID/Module are rejected by From.
func With(ctx context.Context, logCtx Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	base, _ := ctx.Value(contextKey{}).(Context)
	merged := mergeContext(base, logCtx.normalized())
	return context.WithValue(ctx, contextKey{}, merged)
}

// Merge keeps the parent StreamID unless the child explicitly sets a new one.
func mergeContext(base, overlay Context) Context {
	out := base
	if overlay.StreamID != "" {
		out.StreamID = overlay.StreamID
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

// Ensure attaches a valid stream when missing, and overlays module/ids when set.
func Ensure(ctx context.Context, logCtx Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing, ok := Lookup(ctx)
	if !ok {
		if strings.TrimSpace(logCtx.StreamID) == "" {
			logCtx.StreamID = NewStreamID()
		}
		if strings.TrimSpace(logCtx.Module) == "" {
			logCtx.Module = "process"
		}
		return With(ctx, logCtx)
	}
	if logCtx.StreamID == "" {
		logCtx.StreamID = existing.StreamID
	}
	if logCtx.Module == "" {
		logCtx.Module = existing.Module
	}
	return With(ctx, logCtx)
}
