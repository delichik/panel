package docker

import (
	"net/http"
	"testing"
)

func TestResourceOperationParsesContainerLifecycleActions(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		action string
	}{
		{name: "start", method: http.MethodPost, path: "/api/v1/servers/server-1/docker/containers/abc123/start", action: "start"},
		{name: "stop", method: http.MethodPost, path: "/api/v1/servers/server-1/docker/containers/abc123/stop", action: "stop"},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/servers/server-1/docker/containers/abc123", action: "delete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			op, ok := resourceOperation(req)
			if !ok {
				t.Fatal("expected container operation to be recognized")
			}
			if op.Kind != "container" || op.Action != tc.action || op.ID != "abc123" {
				t.Fatalf("unexpected operation: %#v", op)
			}
		})
	}
}
