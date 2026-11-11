package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
)

func TestStartupFailureLogsScopeRedactionAndLimits(t *testing.T) {
	for _, framed := range []bool{false, true} {
		t.Run(map[bool]string{false: "tty", true: "framed"}[framed], func(t *testing.T) {
			var calls int
			started := "2026-09-15T05:40:23.098604815Z"
			finished := "2026-09-15T05:40:23.336761596Z"
			payload := "certificate file missing\npassword=secret-password\n{\"token\":\"json-secret\"}\n-----BEGIN PRIVATE KEY-----\nprivate-data\n-----END PRIVATE KEY-----\n" + strings.Repeat("useful line\n", 1000)
			client := &http.Client{Transport: startupDockerTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				since, sinceErr := time.Parse(time.RFC3339Nano, req.URL.Query().Get("since"))
				until, untilErr := time.Parse(time.RFC3339Nano, req.URL.Query().Get("until"))
				startedAt, _ := time.Parse(time.RFC3339Nano, started)
				finishedAt, _ := time.Parse(time.RFC3339Nano, finished)
				if req.URL.Path != "/containers/exact-id/logs" || sinceErr != nil || untilErr != nil || since.After(startedAt) || until.Before(finishedAt) || req.URL.Query().Get("tail") != "50" || req.URL.Query().Get("follow") != "" {
					t.Fatalf("unscoped log request: %s", req.URL)
				}
				body := []byte(payload)
				if framed {
					var h [8]byte
					h[0] = 2
					binary.BigEndian.PutUint32(h[4:], uint32(len(body)))
					body = append(h[:], body...)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
			})}
			runtime := &LocalRuntime{client: &dockerAPIClient{host: "http://docker", client: client}}
			status := appruntime.InstanceStatus{ContainerID: "exact-id", Status: appruntime.StatusFailed, StartedAt: started, FinishedAt: finished}
			logs := runtime.startupFailureLogs(context.Background(), status, "exact-id", appruntime.Spec{})
			if calls != 1 || !strings.Contains(logs, "certificate file missing") || !strings.Contains(logs, "[truncated]") {
				t.Fatalf("unexpected evidence: %q, calls=%d", logs, calls)
			}
			for _, secret := range []string{"secret-password", "json-secret", "private-data"} {
				if strings.Contains(logs, secret) {
					t.Fatalf("secret leaked: %s", secret)
				}
			}
			if len([]rune(logs)) > startupLogRunes+100 {
				t.Fatal("log snapshot exceeded bound")
			}
			status.ContainerID = "replacement"
			if got := runtime.startupFailureLogs(context.Background(), status, "exact-id", appruntime.Spec{}); !strings.Contains(got, "container id mismatch") || calls != 1 {
				t.Fatalf("mismatch must explain why logs are unavailable: %q", got)
			}
			status.ContainerID, status.StartedAt = "exact-id", ""
			if got := runtime.startupFailureLogs(context.Background(), status, "exact-id", appruntime.Spec{}); !strings.Contains(got, "unavailable") || calls != 1 {
				t.Fatal("read unbounded historical logs")
			}
		})
	}
}

func TestStartupFailureLogsPreservesNginxEmergDespiteManagedConfig(t *testing.T) {
	started := "2026-11-09T19:21:56.786585368Z"
	finished := "2026-11-09T19:21:57.070795528Z"
	nginxConf := "server {\n    listen 443 ssl;\n    server_name ha.esrv.imiku.moe;\n    ssl_certificate /etc/panel-certs/cert-1/certificate.pem;\n    ssl_certificate_key /etc/panel-certs/cert-1/private-key.pem;\n}\n"
	emerg := "nginx: [emerg] cannot load certificate \"/etc/panel-certs/cert-1/certificate.pem\": BIO_new_file() failed (SSL: error:02001002:system library:fopen:No such file or directory)\n"
	client := &http.Client{Transport: startupDockerTransport(func(req *http.Request) (*http.Response, error) {
		since, err := time.Parse(time.RFC3339Nano, req.URL.Query().Get("since"))
		if err != nil {
			t.Fatalf("since = %q", req.URL.Query().Get("since"))
		}
		until, err := time.Parse(time.RFC3339Nano, req.URL.Query().Get("until"))
		if err != nil {
			t.Fatalf("until = %q", req.URL.Query().Get("until"))
		}
		startedAt, _ := time.Parse(time.RFC3339Nano, started)
		finishedAt, _ := time.Parse(time.RFC3339Nano, finished)
		if since.After(startedAt) || until.Before(finishedAt) {
			t.Fatalf("time window must expand around the run: since=%s until=%s", since, until)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(emerg)), Header: make(http.Header)}, nil
	})}
	runtime := &LocalRuntime{client: &dockerAPIClient{host: "http://docker", client: client}}
	status := appruntime.InstanceStatus{ContainerID: "deadbeefcafebabe", Status: appruntime.StatusFailed, StartedAt: started, FinishedAt: finished, ExitCode: 1}
	logs := runtime.startupFailureLogs(context.Background(), status, "deadbeefcafebabe", appruntime.Spec{
		Files: []appruntime.ManagedFile{{Path: "nginx.conf", Content: []byte(nginxConf)}},
	})
	if !strings.Contains(logs, "nginx: [emerg] cannot load certificate") || !strings.Contains(logs, "No such file or directory") {
		t.Fatalf("nginx emerg must remain actionable after managed-file redaction: %q", logs)
	}
	if strings.Contains(logs, nginxConf) {
		t.Fatalf("full managed nginx config must not leak: %q", logs)
	}
}

func TestStartupFailureLogsAcceptsTruncatedContainerID(t *testing.T) {
	started := "2026-09-15T05:40:23.098604815Z"
	finished := "2026-09-15T05:40:23.336761596Z"
	client := &http.Client{Transport: startupDockerTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/containers/"+url.PathEscape("deadbeefcafebabe0123456789abcdef")+"/logs" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("boot failed\n")), Header: make(http.Header)}, nil
	})}
	runtime := &LocalRuntime{client: &dockerAPIClient{host: "http://docker", client: client}}
	status := appruntime.InstanceStatus{ContainerID: "deadbeefcafebabe", Status: appruntime.StatusFailed, StartedAt: started, FinishedAt: finished, ExitCode: 1}
	logs := runtime.startupFailureLogs(context.Background(), status, "deadbeefcafebabe0123456789abcdef", appruntime.Spec{})
	if !strings.Contains(logs, "boot failed") {
		t.Fatalf("truncated id match must still attach logs: %q", logs)
	}
}

func TestStartupFailureLogsTimeoutDoesNotRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		calls := 0
		client := &http.Client{Transport: startupDockerTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		runtime := &LocalRuntime{client: &dockerAPIClient{host: "http://docker", client: client}}
		status := appruntime.InstanceStatus{ContainerID: "id", Status: appruntime.StatusFailed, StartedAt: start.Add(-time.Second).Format(time.RFC3339Nano), FinishedAt: start.Format(time.RFC3339Nano)}
		logs := runtime.startupFailureLogs(context.Background(), status, "id", appruntime.Spec{})
		if calls != 1 || time.Since(start) != 2*time.Second || !strings.Contains(logs, "unavailable") {
			t.Fatalf("unbounded fallback: %d %s %q", calls, time.Since(start), logs)
		}
	})
}
