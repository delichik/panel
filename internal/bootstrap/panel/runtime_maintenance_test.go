package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRuntimeMaintenanceExemptsClearAndReadOnlyDiagnostics(t *testing.T) {
	a := &App{}
	a.runtimeWriters.Pause()
	h := a.runtimeMaintenanceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/v1/debug/clear-runtime-data", 204},
		{http.MethodPost, "/api/v1/auth/login", 204},
		{http.MethodPost, "/api/v1/auth/logout", 204},
		{http.MethodPost, "/api/v1/auth/account", 503},
		{http.MethodPost, "/api/v1/auth/jwt-secret", 503},
		{http.MethodGet, "/api/v1/debug/clear-runtime-data", 204},
		{http.MethodGet, "/api/v1/debug/runtime", 204},
		{http.MethodGet, "/api/v1/debug/tasks", 204},
		{http.MethodGet, "/api/v1/debug/databases", 204},
		{http.MethodPost, "/api/v1/applications", 503},
		{http.MethodDelete, "/api/v1/servers/example", 503},
		{http.MethodGet, "/api/v1/activity/operations", 503},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(test.method, test.path, nil))
		if w.Code != test.want {
			t.Fatalf("%s %s: %d", test.method, test.path, w.Code)
		}
	}
	a.runtimeWriters.Resume()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/applications", nil))
	if w.Code != 204 {
		t.Fatal("mutation remained blocked after resume")
	}
}

func TestClearRequestAuditFinishesBeforeWriterDrain(t *testing.T) {
	a := &App{}
	entered, release, requestDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := a.runtimeMaintenanceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(runtimeClearAuditContextKey{}) == true {
			t.Error("first clear request skipped audit")
		}
		close(entered)
		<-release
	}))
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/debug/clear-runtime-data", nil))
		close(requestDone)
	}()
	<-entered
	drained := make(chan struct{})
	go func() { a.runtimeWriters.Pause(); close(drained) }()
	deadline := time.After(time.Second)
	for !a.runtimeWriters.Paused() {
		select {
		case <-deadline:
			t.Fatal("cleanup did not pause admission")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case <-drained:
		t.Fatal("cleanup drained before request audit tail finished")
	default:
	}
	duplicate := a.runtimeMaintenanceMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(runtimeClearAuditContextKey{}) != true {
			t.Error("duplicate clear request would write audit during deletion")
		}
	}))
	duplicate.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/debug/clear-runtime-data", nil))
	close(release)
	<-requestDone
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("request did not release cleanup admission")
	}
	a.runtimeWriters.Resume()
}
