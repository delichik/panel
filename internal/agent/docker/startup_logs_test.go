package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
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
				if req.URL.Path != "/containers/exact-id/logs" || req.URL.Query().Get("since") != started || req.URL.Query().Get("until") != finished || req.URL.Query().Get("tail") != "50" || req.URL.Query().Get("follow") != "" {
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
			if got := runtime.startupFailureLogs(context.Background(), status, "exact-id", appruntime.Spec{}); got != "" || calls != 1 {
				t.Fatal("read replacement container logs")
			}
			status.ContainerID, status.StartedAt = "exact-id", ""
			if got := runtime.startupFailureLogs(context.Background(), status, "exact-id", appruntime.Spec{}); !strings.Contains(got, "unavailable") || calls != 1 {
				t.Fatal("read unbounded historical logs")
			}
		})
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
