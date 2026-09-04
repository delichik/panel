package backups

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInitRestarterUnsupportedWithoutRestartURL(t *testing.T) {
	t.Setenv(InitRestartURLEnv, "")
	t.Setenv(InitRestartTokenEnv, "secret")
	r := NewPanelInitRestarter(t.TempDir())
	if r.Supported() {
		t.Fatal("expected restart to be unsupported without panel_init restart URL")
	}
}

func TestInitRestarterUnsupportedWithoutRestartToken(t *testing.T) {
	t.Setenv(InitRestartURLEnv, "http://127.0.0.1/restart")
	t.Setenv(InitRestartTokenEnv, "")
	r := NewPanelInitRestarter(t.TempDir())
	if r.Supported() {
		t.Fatal("expected restart to be unsupported without panel_init restart token")
	}
}

func TestInitRestarterPostsRequestedMode(t *testing.T) {
	received := make(chan restartRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(InitRestartTokenHeader); got != "secret-token" {
			t.Errorf("token header = %q, want secret-token", got)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req restartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		received <- req
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv(InitRestartURLEnv, server.URL)
	t.Setenv(InitRestartTokenEnv, "secret-token")
	r := NewPanelInitRestarter(t.TempDir())

	r.RestartSoon(MaintenanceModeRestore)

	select {
	case req := <-received:
		if req.Mode != MaintenanceModeRestore {
			t.Fatalf("mode = %q, want %q", req.Mode, MaintenanceModeRestore)
		}
	case <-time.After(time.Second):
		t.Fatal("expected restart request")
	}
}
