package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

type startupDockerTransport func(*http.Request) (*http.Response, error)

func (f startupDockerTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ORCH-CTRL-007: exercise the complete apply path with Docker changing state
// after its first successful start/inspect, using virtual time rather than
// shortening the production observation window.
func TestReconcileRequiresStableStartup(t *testing.T) {
	for _, tc := range []struct {
		name, initial, after string
		changeAt             time.Duration
		wantSuccess          bool
	}{
		{name: "restart crashes after first running sample", initial: "exited", after: "exited", changeAt: 250 * time.Millisecond},
		{name: "new container crashes", initial: "missing", after: "exited", changeAt: 250 * time.Millisecond},
		{name: "recent running container crashes", initial: "running", after: "exited", changeAt: 250 * time.Millisecond},
		{name: "clean exit is not a running service", initial: "exited", after: "clean_exit", changeAt: time.Second},
		{name: "crash at end of window", initial: "exited", after: "exited", changeAt: 9900 * time.Millisecond},
		{name: "container disappears", initial: "exited", after: "missing", changeAt: time.Second},
		{name: "same name replaced", initial: "exited", after: "replacement", changeAt: time.Second},
		{name: "process restarted between polls", initial: "exited", after: "restart", changeAt: time.Second},
		{name: "inspect fails during observation", initial: "exited", after: "inspect_error", changeAt: time.Second},
		{name: "cancel during observation", initial: "exited", after: "cancel", changeAt: time.Second},
		{name: "deadline during observation", initial: "exited", after: "deadline", changeAt: time.Second},
		{name: "stable restarted container", initial: "exited", wantSuccess: true},
		{name: "stable new container", initial: "missing", wantSuccess: true},
		{name: "stable recent running container", initial: "running", wantSuccess: true},
		{name: "existing stable container is immediate", initial: "stable", wantSuccess: true},
		{name: "missing startup timestamp requires observation", initial: "no_timestamp", wantSuccess: true},
		{name: "invalid startup timestamp requires observation", initial: "invalid_timestamp", wantSuccess: true},
		{name: "future startup timestamp requires observation", initial: "future_timestamp", wantSuccess: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if tc.after == "deadline" {
					var deadlineCancel context.CancelFunc
					ctx, deadlineCancel = context.WithTimeout(ctx, tc.changeAt)
					defer deadlineCancel()
				}
				if tc.after == "cancel" {
					go func() { time.Sleep(tc.changeAt); cancel() }()
				}
				inspectCalls, startCalls := 0, 0
				transport := startupDockerTransport(func(req *http.Request) (*http.Response, error) {
					code, body := http.StatusOK, `{}`
					switch {
					case req.Method == http.MethodGet && req.URL.Path == "/containers/container-1/logs":
						body = "startup failed: certificate file missing\n"
						if req.URL.Query().Get("since") == "" || req.URL.Query().Get("until") == "" {
							t.Fatal("failure logs must be bounded to this run")
						}
					case req.Method == http.MethodGet && req.URL.Path == "/containers/panel-web/json":
						inspectCalls++
						state, running, exitCode, containerID := "running", true, 0, "container-1"
						startedAt := start
						if tc.initial == "stable" {
							startedAt = start.Add(-time.Hour)
						}
						if inspectCalls == 1 && tc.initial == "exited" {
							state, running = "exited", false
						}
						if inspectCalls == 1 && tc.initial == "missing" {
							code = http.StatusNotFound
						}
						if inspectCalls > 1 && tc.after != "" && time.Since(start) >= tc.changeAt {
							switch tc.after {
							case "exited":
								state, running, exitCode = "exited", false, 1
							case "clean_exit":
								state, running = "exited", false
							case "missing":
								code = http.StatusNotFound
							case "replacement":
								containerID = "container-2"
							case "restart":
								startedAt = start.Add(tc.changeAt)
							case "inspect_error":
								code = http.StatusInternalServerError
							}
						}
						data := map[string]any{
							"Id": containerID, "Name": "/panel-web",
							"Config":     map[string]any{"Image": "example/web:1", "Labels": map[string]string{"panel.application.managed": "true", "panel.application.id": "app-1", "panel.application.instance.id": "inst-1", "panel.application.generation": "2", "panel.application.spec.hash": "hash"}},
							"HostConfig": map[string]any{"NetworkMode": "panel-apps"},
							"State":      map[string]any{"Status": state, "Running": running, "ExitCode": exitCode, "StartedAt": startedAt.Format(time.RFC3339Nano)},
						}
						if !running {
							data["State"].(map[string]any)["FinishedAt"] = start.Add(tc.changeAt).Format(time.RFC3339Nano)
						}
						switch tc.initial {
						case "no_timestamp":
							delete(data["State"].(map[string]any), "StartedAt")
						case "invalid_timestamp":
							data["State"].(map[string]any)["StartedAt"] = "invalid"
						case "future_timestamp":
							data["State"].(map[string]any)["StartedAt"] = start.Add(time.Hour).Format(time.RFC3339Nano)
						}
						encoded, _ := json.Marshal(data)
						body = string(encoded)
					case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/start"):
						startCalls++
						code = http.StatusNoContent
					case req.Method == http.MethodPost && req.URL.Path == "/containers/create":
						body = `{"Id":"container-1"}`
					case req.Method == http.MethodGet && req.URL.Path == "/networks/panel-apps":
						body = `{"Name":"panel-apps","Driver":"bridge"}`
					case req.Method == http.MethodPost && req.URL.Path == "/images/create":
						body = ""
					default:
						t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
					}
					return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
				})
				client := &http.Client{Transport: transport}
				runtime := &LocalRuntime{root: t.TempDir(), client: &dockerAPIClient{host: "http://docker", client: client, pullClient: client}}
				result, err := runtime.Reconcile(ctx, agentcontract.RuntimeReconcileRequest{
					ExecutionID: "exec-1", ApplicationID: "app-1", InstanceID: "inst-1", Action: "apply", DesiredGeneration: 2, DesiredSpecHash: "hash",
					Spec: appruntime.Spec{ApplicationID: "app-1", InstanceID: "inst-1", ContainerName: "panel-web", Image: "example/web:1", Generation: 2, SpecHash: "hash"},
				})
				if tc.wantSuccess {
					if err != nil || result.ErrorCode != "" || result.ObservedState != appruntime.StatusRunning {
						t.Fatalf("unexpected failure: %#v, %v", result, err)
					}
					if tc.initial == "stable" {
						if time.Since(start) != 0 {
							t.Fatal("already stable container waited again")
						}
					} else if time.Since(start) < 10*time.Second {
						t.Fatalf("success before stability window: %s", time.Since(start))
					}
				} else {
					if err == nil || result.ErrorCode != "container_not_running" || result.ErrorClass != "container_start_failed" || !result.Retryable {
						t.Fatalf("startup failure was not retryable: %#v, %v", result, err)
					}
					if tc.after == "exited" || tc.after == "clean_exit" {
						wantCode := 1
						if tc.after == "clean_exit" {
							wantCode = 0
						}
						if !strings.Contains(result.ErrorDetail, fmt.Sprintf("exitCode=%d", wantCode)) || !strings.Contains(result.ErrorDetail, "finishedAt=") {
							t.Fatalf("missing exit evidence: %#v", result)
						}
					}
					for _, step := range result.Steps {
						if step.Name == "cleanup_legacy_workspace" {
							t.Fatal("failed startup ran success cleanup")
						}
					}
					last := result.Steps[len(result.Steps)-1]
					if last.Name != "verify_running" || last.Status != "failed" {
						t.Fatalf("verification did not fail: %#v", last)
					}
				}
				wantStarts := 1
				if tc.initial != "exited" && tc.initial != "missing" {
					wantStarts = 0
				}
				if startCalls != wantStarts {
					t.Fatalf("start calls = %d, want %d", startCalls, wantStarts)
				}
			})
		})
	}
}
