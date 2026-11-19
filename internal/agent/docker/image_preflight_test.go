package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

// probeDockerRecorder captures the ordered Docker API calls a reconcile makes
// so tests can pin ordering (pull -> preflight -> replace) and cleanup.
type probeDockerRecorder struct {
	mu       sync.Mutex
	calls    []string
	payloads map[string]map[string]any
}

func newProbeDockerRecorder() *probeDockerRecorder {
	return &probeDockerRecorder{payloads: map[string]map[string]any{}}
}

func (r *probeDockerRecorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *probeDockerRecorder) recordPayload(name string, payload map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads[name] = payload
}

func (r *probeDockerRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *probeDockerRecorder) payload(name string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.payloads[name]
}

func (r *probeDockerRecorder) index(t *testing.T, call string) int {
	t.Helper()
	for index, value := range r.snapshot() {
		if value == call {
			return index
		}
	}
	t.Fatalf("expected Docker request %q in %#v", call, r.snapshot())
	return -1
}

// newProbeRuntime builds a LocalRuntime against a fake Docker daemon.
func newProbeRuntime(t *testing.T, server *httptest.Server) *LocalRuntime {
	t.Helper()
	client := server.Client()
	return &LocalRuntime{root: t.TempDir(), client: &dockerAPIClient{host: server.URL, client: client, pullClient: client}}
}

// writeProbeSpec returns a spec shaped like the reverse-proxy image spec: a
// managed nginx config file mounted read-only into the container.
func writeProbeSpec() appruntime.Spec {
	return appruntime.Spec{
		ApplicationID: "app-1",
		InstanceID:    "inst-1",
		ContainerName: "panel-web",
		Image:         "nginx:1.28-alpine",
		Generation:    2,
		SpecHash:      "new-hash",
		Files: []appruntime.ManagedFile{
			{Path: "/etc/panel-nginx/nginx.conf", Content: []byte("events {}\nhttp {}\n"), Mode: "0644"},
		},
		Mounts: []appruntime.Mount{
			{Type: "managed_file", Source: "/etc/panel-nginx/nginx.conf", Target: "/etc/panel-nginx/nginx.conf", ReadOnly: true},
		},
	}
}

func oldContainerJSON() string {
	return `{"Id":"container-old","Name":"/panel-web","Config":{"Image":"nginx:1.27-alpine","Labels":{"panel.application.managed":"true","panel.application.id":"app-1","panel.application.instance.id":"inst-1","panel.application.generation":"1","panel.application.spec.hash":"old-hash"}},"HostConfig":{"NetworkMode":"panel-apps"},"State":{"Status":"running","Running":true,"StartedAt":"2026-01-01T00:00:00Z"}}`
}

// TestReconcileImagePreflightRunsDisposableContainerBeforeReplace pins the P1
// order: pull -> preflight disposable container -> version probe -> stop/remove.
// The disposable containers must never publish ports and must be removed again.
func TestReconcileImagePreflightRunsDisposableContainerBeforeReplace(t *testing.T) {
	recorder := newProbeDockerRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, oldContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			recorder.record("POST /images/create")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode create payload: %v", err)
			}
			recorder.recordPayload(name, payload)
			recorder.record("POST /containers/create?name=" + name)
			w.Header().Set("Content-Type", "application/json")
			switch name {
			case "panel-web-preflight-probe":
				_, _ = io.WriteString(w, `{"Id":"probe-preflight"}`)
			case "panel-web-version-probe":
				_, _ = io.WriteString(w, `{"Id":"probe-version"}`)
			default:
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"create refused"}`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/containers/panel-web/stop":
			recorder.record("POST /containers/panel-web/stop")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/start"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"StatusCode":0}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
			recorder.record(r.Method + " " + r.URL.Path)
			if strings.Contains(r.URL.Path, "probe-version") {
				_, _ = io.WriteString(w, "nginx version: nginx/1.28.0\n")
				return
			}
			_, _ = io.WriteString(w, "nginx: the configuration file /etc/panel-nginx/nginx.conf syntax is ok\nnginx: configuration file /etc/panel-nginx/nginx.conf test is successful\n")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	result, err := rt.Reconcile(context.Background(), agentcontract.RuntimeReconcileRequest{
		ExecutionID: "exec-preflight-1", ApplicationID: "app-1", InstanceID: "inst-1", Action: "apply",
		DesiredGeneration: 2, DesiredSpecHash: "new-hash",
		Spec:                  writeProbeSpec(),
		ImagePreflightCommand: []string{"nginx", "-t", "-c", "/etc/panel-nginx/nginx.conf"},
		ImageVersionCommand:   []string{"nginx", "-v"},
	})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want the create failure to stop the apply")
	}
	if result.ErrorCode != "create_container_failed" {
		t.Fatalf("ErrorCode = %q, want create_container_failed (probe must not fail)", result.ErrorCode)
	}
	if result.ImageVersion != "nginx version: nginx/1.28.0" {
		t.Fatalf("ImageVersion = %q, want the nginx -v output", result.ImageVersion)
	}
	if !strings.Contains(result.PreflightOutput, "test is successful") {
		t.Fatalf("PreflightOutput = %q, want the nginx -t output", result.PreflightOutput)
	}

	// Disposable containers are created from the spec mounts/network with no
	// published ports and never join the managed-application identity.
	preflight := recorder.payload("panel-web-preflight-probe")
	if preflight == nil {
		t.Fatal("preflight disposable container was not created")
	}
	if _, hasExposedPorts := preflight["ExposedPorts"]; hasExposedPorts {
		t.Fatalf("preflight must not expose ports: %#v", preflight["ExposedPorts"])
	}
	hostConfig, _ := preflight["HostConfig"].(map[string]any)
	if _, hasPortBindings := hostConfig["PortBindings"]; hasPortBindings {
		t.Fatalf("preflight must not publish ports: %#v", hostConfig["PortBindings"])
	}
	if hostConfig["NetworkMode"] != managedBridgeNetwork {
		t.Fatalf("NetworkMode = %#v, want %q", hostConfig["NetworkMode"], managedBridgeNetwork)
	}
	if binds, _ := hostConfig["Binds"].([]any); len(binds) != 1 || !strings.Contains(fmt.Sprint(binds[0]), "nginx.conf") {
		t.Fatalf("preflight must reuse spec mounts, got %#v", hostConfig["Binds"])
	}
	if cmd, _ := preflight["Cmd"].([]any); len(cmd) != 4 || cmd[0] != "nginx" || cmd[1] != "-t" {
		t.Fatalf("Cmd = %#v, want the preflight command", preflight["Cmd"])
	}
	labels, _ := preflight["Labels"].(map[string]any)
	if labels[labelImageProbe] != imageProbePurposePreflight {
		t.Fatalf("probe label = %#v", preflight["Labels"])
	}
	if labels["panel.application.managed"] != nil {
		t.Fatalf("probe must not be a managed application container: %#v", labels)
	}
	if version := recorder.payload("panel-web-version-probe"); version == nil {
		t.Fatal("version disposable container was not created")
	}

	// Order and cleanup: probes finish and are removed before the old container
	// is stopped; the pull is still first.
	pull := recorder.index(t, "POST /images/create")
	preflightCreate := recorder.index(t, "POST /containers/create?name=panel-web-preflight-probe")
	preflightRemove := recorder.index(t, "DELETE /containers/probe-preflight")
	versionCreate := recorder.index(t, "POST /containers/create?name=panel-web-version-probe")
	versionRemove := recorder.index(t, "DELETE /containers/probe-version")
	stop := recorder.index(t, "POST /containers/panel-web/stop")
	remove := recorder.index(t, "DELETE /containers/panel-web")
	if !(pull < preflightCreate && preflightCreate < preflightRemove && preflightRemove < versionCreate && versionCreate < versionRemove && versionRemove < stop && stop < remove) {
		t.Fatalf("probe/cleanup/replace order violated: %#v", recorder.snapshot())
	}
	for _, step := range result.Steps {
		if step.Name == "image_preflight" && step.Status != "succeeded" {
			t.Fatalf("image_preflight step = %#v, want succeeded", step)
		}
		if step.Name == "image_version" && step.Status != "succeeded" {
			t.Fatalf("image_version step = %#v, want succeeded", step)
		}
	}
	steps := result.Steps
	pullStep := reconcileStepIndex(t, steps, "ensure_image")
	preflightStep := reconcileStepIndex(t, steps, "image_preflight")
	versionStep := reconcileStepIndex(t, steps, "image_version")
	stopStep := reconcileStepIndex(t, steps, "replace_stop")
	if !(pullStep < preflightStep && preflightStep < versionStep && versionStep < stopStep) {
		t.Fatalf("steps out of order: %#v", steps)
	}
}

// TestReconcileImagePreflightFailureKeepsOldContainer verifies the safety
// contract: a failed preflight aborts the apply, removes the disposable
// container, and never touches the serving container.
func TestReconcileImagePreflightFailureKeepsOldContainer(t *testing.T) {
	recorder := newProbeDockerRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, oldContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			recorder.record("POST /containers/create?name=" + name)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Id":"probe-preflight"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"StatusCode":1}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
			_, _ = io.WriteString(w, "nginx: [emerg] unknown directive \"server_tokens\" in /etc/panel-nginx/conf.d/example.conf:1\nnginx: configuration file /etc/panel-nginx/nginx.conf test failed\n")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	result, err := rt.Reconcile(context.Background(), agentcontract.RuntimeReconcileRequest{
		ExecutionID: "exec-preflight-fail-1", ApplicationID: "app-1", InstanceID: "inst-1", Action: "apply",
		DesiredGeneration: 2, DesiredSpecHash: "new-hash",
		Spec:                  writeProbeSpec(),
		ImagePreflightCommand: []string{"nginx", "-t", "-c", "/etc/panel-nginx/nginx.conf"},
		ImageVersionCommand:   []string{"nginx", "-v"},
	})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want the preflight failure")
	}
	if result.ErrorCode != "image_preflight_failed" || result.ErrorClass != "image_preflight" || !result.Retryable {
		t.Fatalf("result = %#v, want retryable image_preflight_failed", result)
	}
	if !strings.Contains(result.ErrorDetail, "unknown directive") || !strings.Contains(result.PreflightOutput, "test failed") {
		t.Fatalf("probe output missing from result: detail=%q output=%q", result.ErrorDetail, result.PreflightOutput)
	}
	if result.ImageVersion != "" {
		t.Fatalf("ImageVersion = %q, want empty when preflight failed", result.ImageVersion)
	}

	for _, call := range recorder.snapshot() {
		if strings.Contains(call, "/stop") || call == "DELETE /containers/panel-web" || call == "POST /containers/create?name=panel-web" {
			t.Fatalf("serving container must not be touched after a failed preflight, got %#v", recorder.snapshot())
		}
	}
	recorder.index(t, "DELETE /containers/probe-preflight")
	for _, step := range result.Steps {
		if step.Name == "image_preflight" && step.Status != "failed" {
			t.Fatalf("image_preflight step = %#v, want failed", step)
		}
		if step.Name == "image_version" {
			t.Fatalf("version probe must not run after a failed preflight: %#v", result.Steps)
		}
		if step.Name == "replace_stop" || step.Name == "replace_remove" {
			t.Fatalf("replace steps must not run after a failed preflight: %#v", result.Steps)
		}
	}
}

// TestReconcileImagePreflightVersionFailureDoesNotBlockDeploy keeps version
// reporting an observation: `nginx -v` is used by Panel for its version-line
// check, but a broken version probe must not keep a validated image from
// deploying. The failed step still records the problem.
func TestReconcileImagePreflightVersionFailureDoesNotBlockDeploy(t *testing.T) {
	recorder := newProbeDockerRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, oldContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			name := r.URL.Query().Get("name")
			recorder.record("POST /containers/create?name=" + name)
			w.Header().Set("Content-Type", "application/json")
			switch name {
			case "panel-web-preflight-probe":
				_, _ = io.WriteString(w, `{"Id":"probe-preflight"}`)
			case "panel-web-version-probe":
				_, _ = io.WriteString(w, `{"Id":"probe-version"}`)
			default:
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"create refused"}`)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/containers/panel-web/stop":
			recorder.record("POST /containers/panel-web/stop")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.URL.Path, "probe-version") {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"wait failed"}`)
				return
			}
			_, _ = io.WriteString(w, `{"StatusCode":0}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
			_, _ = io.WriteString(w, "nginx: configuration file /etc/panel-nginx/nginx.conf test is successful\n")
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	result, err := rt.Reconcile(context.Background(), agentcontract.RuntimeReconcileRequest{
		ExecutionID: "exec-version-fail-1", ApplicationID: "app-1", InstanceID: "inst-1", Action: "apply",
		DesiredGeneration: 2, DesiredSpecHash: "new-hash",
		Spec:                  writeProbeSpec(),
		ImagePreflightCommand: []string{"nginx", "-t", "-c", "/etc/panel-nginx/nginx.conf"},
		ImageVersionCommand:   []string{"nginx", "-v"},
	})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want the app create failure")
	}
	if result.ErrorCode != "create_container_failed" {
		t.Fatalf("ErrorCode = %q, want create_container_failed (version probe must be best effort)", result.ErrorCode)
	}
	if result.ImageVersion != "" {
		t.Fatalf("ImageVersion = %q, want empty after a failed version probe", result.ImageVersion)
	}
	if !strings.Contains(result.PreflightOutput, "test is successful") {
		t.Fatalf("PreflightOutput = %q", result.PreflightOutput)
	}
	recorder.index(t, "POST /containers/panel-web/stop")
	recorder.index(t, "DELETE /containers/probe-preflight")
	recorder.index(t, "DELETE /containers/probe-version")
	versionFailed := false
	for _, step := range result.Steps {
		if step.Name == "image_version" {
			versionFailed = step.Status == "failed"
		}
	}
	if !versionFailed {
		t.Fatalf("image_version step must be recorded as failed: %#v", result.Steps)
	}
}

// TestReconcileWithoutPreflightKeepsLegacyBehavior guards the opt-in property:
// a request without image preflight fields behaves exactly like today (pull,
// stop/remove, create) and creates no disposable containers.
func TestReconcileWithoutPreflightKeepsLegacyBehavior(t *testing.T) {
	recorder := newProbeDockerRecorder()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, oldContainerJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/images/create":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			recorder.record("POST /containers/create?name=" + r.URL.Query().Get("name"))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"message":"create refused"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
			recorder.record(r.Method + " " + r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	result, err := rt.Reconcile(context.Background(), agentcontract.RuntimeReconcileRequest{
		ExecutionID: "exec-legacy-1", ApplicationID: "app-1", InstanceID: "inst-1", Action: "apply",
		DesiredGeneration: 2, DesiredSpecHash: "new-hash", Spec: writeProbeSpec(),
	})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want the create failure to stop the apply")
	}
	if result.PreflightOutput != "" || result.ImageVersion != "" {
		t.Fatalf("legacy request must not report probe output: %#v", result)
	}
	for _, call := range recorder.snapshot() {
		if strings.Contains(call, "-probe") || strings.Contains(call, "/wait") {
			t.Fatalf("legacy request must not create probe containers: %#v", recorder.snapshot())
		}
	}
	for _, step := range result.Steps {
		if step.Name == "image_preflight" || step.Name == "image_version" {
			t.Fatalf("legacy request must not record probe steps: %#v", result.Steps)
		}
	}
	recorder.index(t, "POST /containers/panel-web/stop")
	recorder.index(t, "DELETE /containers/panel-web")
}

// TestRunImageProbeRemovesLeftoverProbeAfterCrash covers the crash-recovery
// path: a name conflict with a leftover probe container is cleaned up and
// retried, while a non-probe occupant is never removed.
func TestRunImageProbeRemovesLeftoverProbeAfterCrash(t *testing.T) {
	var creates int
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			creates++
			name := r.URL.Query().Get("name")
			if name != "panel-web-preflight-probe" {
				t.Errorf("create name = %q", name)
			}
			w.Header().Set("Content-Type", "application/json")
			if creates == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"message":"Conflict. The container name \"/panel-web-preflight-probe\" is already in use by container \"probe-old\"."}`)
				return
			}
			_, _ = io.WriteString(w, `{"Id":"probe-new"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web-preflight-probe/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Id":"probe-old","Name":"/panel-web-preflight-probe","Config":{"Image":"nginx:1.28-alpine","Labels":{"`+labelImageProbe+`":"preflight"}},"State":{"Status":"exited","Running":false}}`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wait"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"StatusCode":0}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
			_, _ = io.WriteString(w, "nginx: configuration file /etc/panel-nginx/nginx.conf test is successful\n")
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	result, err := rt.runImageProbe(context.Background(), writeProbeSpec(), imageProbePurposePreflight, []string{"nginx", "-t"})
	if err != nil {
		t.Fatalf("runImageProbe() error = %v", err)
	}
	if result.ExitCode != 0 || !strings.Contains(result.Output, "test is successful") {
		t.Fatalf("probe result = %#v", result)
	}
	if creates != 2 {
		t.Fatalf("creates = %d, want a retry after the leftover probe was removed", creates)
	}
	joined := strings.Join(deleted, ",")
	if !strings.Contains(joined, "/containers/probe-old") || !strings.Contains(joined, "/containers/probe-new") {
		t.Fatalf("deleted containers = %#v, want leftover and finished probe", deleted)
	}
}

func TestRunImageProbeRejectsNonProbeNameConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/networks/"+managedBridgeNetwork:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Name":"panel-apps"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"message":"Conflict. The container name \"/panel-web-preflight-probe\" is already in use by container \"user-container\"."}`)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/panel-web-preflight-probe/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"Id":"user-container","Name":"/panel-web-preflight-probe","Config":{"Image":"user:latest","Labels":{}}}`)
		case r.Method == http.MethodDelete:
			t.Errorf("must not delete a non-probe container: %s", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	rt := newProbeRuntime(t, server)
	if _, err := rt.runImageProbe(context.Background(), writeProbeSpec(), imageProbePurposePreflight, []string{"nginx", "-t"}); err == nil {
		t.Fatal("runImageProbe() error = nil, want a non-probe name conflict error")
	}
}
