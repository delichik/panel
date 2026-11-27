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
	if entries[0].ContextMap()["eventId"] == nil || entries[0].ContextMap()["eventId"] == "" {
		t.Fatalf("missing eventId: %#v", entries[0].ContextMap())
	}
}

// TestHTTPMiddlewareLogsAllStatusesAtDebug verifies ENG-LOG-009: every
// "http request completed" line is debug, even for 4xx/5xx responses, so the
// default log filter and the event notification channel stay free of HTTP noise.
func TestHTTPMiddlewareLogsAllStatusesAtDebug(t *testing.T) {
	prevLevel := Level()
	t.Cleanup(func() { _ = SetLevel(prevLevel) })
	if err := SetLevel("debug"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		status  int
		handler http.HandlerFunc
	}{
		{"ok", http.StatusOK, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }},
		{"client error", http.StatusNotFound, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }},
		{"server error", http.StatusInternalServerError, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			prev := stdoutLogger
			stdoutLogger = zap.New(core)
			defer func() { stdoutLogger = prev }()

			HTTPMiddleware(tc.handler).ServeHTTP(
				httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "/x", nil),
			)

			entries := logs.FilterMessage("http request completed").All()
			if len(entries) != 1 {
				t.Fatalf("entries = %#v", entries)
			}
			if entries[0].Level != zapcore.DebugLevel {
				t.Fatalf("level = %s, want debug", entries[0].Level)
			}
			status, ok := entries[0].ContextMap()["status"].(int64)
			if !ok || int(status) != tc.status {
				t.Fatalf("status = %#v, want %d", entries[0].ContextMap()["status"], tc.status)
			}
		})
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
