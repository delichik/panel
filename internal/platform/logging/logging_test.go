package logging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPMiddlewareLogsSuccessfulRequestsAtDebug(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	prev := stdoutLogger
	stdoutLogger = zap.New(core)
	defer func() { stdoutLogger = prev }()
	_ = SetLevel("debug")

	var sawStream bool
	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logCtx, ok := Lookup(r.Context())
		if !ok || logCtx.StreamID == "" || logCtx.Module != "http" {
			t.Fatalf("request context missing log stream: ok=%v ctx=%#v", ok, logCtx)
		}
		sawStream = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !sawStream {
		t.Fatal("handler did not observe log stream")
	}

	entries := logs.FilterMessage("http request completed").All()
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Level != zapcore.DebugLevel {
		t.Fatalf("level = %s, want debug", entries[0].Level)
	}
	if entries[0].ContextMap()["module"] != "http" {
		t.Fatalf("module = %#v", entries[0].ContextMap()["module"])
	}
	if entries[0].ContextMap()["streamId"] == nil || entries[0].ContextMap()["streamId"] == "" {
		t.Fatalf("missing streamId: %#v", entries[0].ContextMap())
	}
}

func TestHTTPMiddlewareKeepsClientErrorsAtWarn(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	prev := stdoutLogger
	stdoutLogger = zap.New(core)
	defer func() { stdoutLogger = prev }()

	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	entries := logs.FilterMessage("http request completed").All()
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Fatalf("level = %s, want warn", entries[0].Level)
	}
}

func TestFromDiscardsInvalidContext(t *testing.T) {
	resetInvalidCount()
	before := InvalidCount()
	From(context.Background()).Info("should discard")
	if InvalidCount() <= before {
		t.Fatal("expected invalid counter to increase")
	}
}

func TestWithMergesModuleKeepsStream(t *testing.T) {
	ctx := With(context.Background(), Context{StreamID: "stream-a", Module: "http"})
	ctx = With(ctx, Context{Module: "servers"})
	logCtx, ok := Lookup(ctx)
	if !ok {
		t.Fatal("expected valid context")
	}
	if logCtx.StreamID != "stream-a" || logCtx.Module != "servers" {
		t.Fatalf("got %#v", logCtx)
	}
}
