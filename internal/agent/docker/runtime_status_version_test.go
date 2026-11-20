package docker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
)

func statusVersionContainerJSON() string {
	return `{"Id":"container-1","Name":"/panel-web","Config":{"Image":"nginx:1.28-alpine"},"State":{"Status":"running","Running":true,"StartedAt":"2026-01-01T00:00:00Z","FinishedAt":"0001-01-01T00:00:00Z","ExitCode":0,"Error":""}}`
}

// TestRuntimeStatusWithVersionParsesNginxVersion pins the version collection
// path: inspect -> docker exec of the requested command -> parse nginx/x.y.z
// from the combined stdout/stderr output.
func TestRuntimeStatusWithVersionParsesNginxVersion(t *testing.T) {
	var execCommand []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			_, _ = io.WriteString(w, statusVersionContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/containers/panel-web/exec":
			var payload struct {
				Cmd []string `json:"Cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode exec payload: %v", err)
			}
			execCommand = payload.Cmd
			_, _ = io.WriteString(w, `{"Id":"exec-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/exec/exec-1/start":
			// nginx -v writes to stderr, captured here as the combined stream.
			_, _ = io.WriteString(w, "nginx version: nginx/1.28.3\n")
		case r.Method == http.MethodGet && r.URL.Path == "/exec/exec-1/json":
			_, _ = io.WriteString(w, `{"Running":false,"ExitCode":0}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &LocalRuntime{client: &dockerAPIClient{host: server.URL, client: server.Client()}}
	status, version, err := r.StatusWithVersion(context.Background(), "inst-1", "panel-web", "server-1", []string{"nginx", "-v"})
	if err != nil {
		t.Fatalf("StatusWithVersion() error = %v", err)
	}
	if status.Status != appruntime.StatusRunning {
		t.Fatalf("status = %q, want running", status.Status)
	}
	if version != "1.28.3" {
		t.Fatalf("image version = %q, want 1.28.3", version)
	}
	if len(execCommand) != 2 || execCommand[0] != "nginx" || execCommand[1] != "-v" {
		t.Fatalf("exec command = %#v, want [nginx -v]", execCommand)
	}
}

// TestRuntimeStatusWithVersionCommandFailureIsEmpty verifies that a failing
// version command is an observation failure: the status query still succeeds and
// the version is empty.
func TestRuntimeStatusWithVersionCommandFailureIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name     string
		start    func(w http.ResponseWriter)
		execJSON string
	}{
		{
			name: "exec request fails",
			start: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"exec failed"}`)
			},
			execJSON: `{"Running":false,"ExitCode":0}`,
		},
		{
			name:     "command exits non-zero",
			start:    func(w http.ResponseWriter) { _, _ = io.WriteString(w, "nginx: [emerg] unknown directive\n") },
			execJSON: `{"Running":false,"ExitCode":1}`,
		},
		{
			name:     "output has no nginx version",
			start:    func(w http.ResponseWriter) { _, _ = io.WriteString(w, "some unrelated output\n") },
			execJSON: `{"Running":false,"ExitCode":0}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
					_, _ = io.WriteString(w, statusVersionContainerJSON())
				case r.Method == http.MethodPost && r.URL.Path == "/containers/panel-web/exec":
					_, _ = io.WriteString(w, `{"Id":"exec-1"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/exec/exec-1/start":
					tc.start(w)
				case r.Method == http.MethodGet && r.URL.Path == "/exec/exec-1/json":
					_, _ = io.WriteString(w, tc.execJSON)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			r := &LocalRuntime{client: &dockerAPIClient{host: server.URL, client: server.Client()}}
			status, version, err := r.StatusWithVersion(context.Background(), "inst-1", "panel-web", "server-1", []string{"nginx", "-v"})
			if err != nil {
				t.Fatalf("StatusWithVersion() error = %v", err)
			}
			if status.Status != appruntime.StatusRunning {
				t.Fatalf("status = %q, want running", status.Status)
			}
			if version != "" {
				t.Fatalf("image version = %q, want empty", version)
			}
		})
	}
}

// TestRuntimeStatusWithoutVersionCommandSkipsExec pins the unchanged path for
// callers that do not request a version: no docker exec is attempted.
func TestRuntimeStatusWithoutVersionCommandSkipsExec(t *testing.T) {
	var execCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json" {
			_, _ = io.WriteString(w, statusVersionContainerJSON())
			return
		}
		execCalls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r := &LocalRuntime{client: &dockerAPIClient{host: server.URL, client: server.Client()}}
	status, err := r.Status(context.Background(), "inst-1", "panel-web", "server-1")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Status != appruntime.StatusRunning {
		t.Fatalf("status = %q, want running", status.Status)
	}
	if execCalls.Load() != 0 {
		t.Fatalf("docker exec calls = %d, want 0", execCalls.Load())
	}
}

// TestRuntimeStatusWithVersionHonorsContextCancellation verifies a blocked exec
// cannot hang status: the parent deadline cancels the probe, the version is
// empty and the status itself still succeeds.
func TestRuntimeStatusWithVersionHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			_, _ = io.WriteString(w, statusVersionContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/containers/panel-web/exec":
			_, _ = io.WriteString(w, `{"Id":"exec-1"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/exec/exec-1/start":
			// Finish after the caller's deadline so the test proves the probe is
			// bounded by the context instead of waiting for the command output.
			time.Sleep(250 * time.Millisecond)
			_, _ = io.WriteString(w, "nginx version: nginx/1.28.0\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	r := &LocalRuntime{client: &dockerAPIClient{host: server.URL, client: server.Client()}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	status, version, err := r.StatusWithVersion(ctx, "inst-1", "panel-web", "server-1", []string{"nginx", "-v"})
	if err != nil {
		t.Fatalf("StatusWithVersion() error = %v", err)
	}
	if status.Status != appruntime.StatusRunning {
		t.Fatalf("status = %q, want running", status.Status)
	}
	if version != "" {
		t.Fatalf("image version = %q, want empty", version)
	}
}

func TestParseImageVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
		want   string
	}{
		{name: "nginx version line", output: "nginx version: nginx/1.28.0\n", want: "1.28.0"},
		{name: "stderr and stdout mixed", output: "built by gcc\nnginx version: nginx/1.27.5\n", want: "1.27.5"},
		{name: "four component version", output: "nginx version: nginx/1.25.3.1\n", want: "1.25.3.1"},
		{name: "uppercase prefix", output: "NGINX/1.30.0\n", want: "1.30.0"},
		{name: "no version", output: "nginx: [emerg] unknown directive\n", want: ""},
		{name: "empty output", output: "", want: ""},
		{name: "unrelated path", output: "/etc/nginx/conf.d/default.conf\n", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseImageVersion(tc.output); got != tc.want {
				t.Fatalf("parseImageVersion(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
	// Guard against a pathological, unbounded token being echoed back.
	if got := parseImageVersion("nginx/" + strings.Repeat("9", maxImageVersionLength+10)); got != "" {
		t.Fatalf("overlong version = %q, want empty", got)
	}
}
