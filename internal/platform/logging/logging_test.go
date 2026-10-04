package logging

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestHTTPMiddlewareLogsSuccessfulRequestsAtDebug(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()

	handler := HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	entries := logs.FilterMessage("http request completed").All()
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Level != zapcore.DebugLevel {
		t.Fatalf("level = %s, want debug", entries[0].Level)
	}
}

func TestHTTPMiddlewareKeepsClientErrorsAtWarn(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()

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
