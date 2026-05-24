package nomad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/httpx"
)

func TestHandlerInventoryEndpoints(t *testing.T) {
	fake := &fakeInventoryClient{
		status:      StatusResponse{Connected: true, Leader: "127.0.0.1:4647"},
		nodes:       []NodeListItem{{ID: "node-1", Name: "node", Status: "ready"}},
		jobs:        []JobListItem{{ID: "panel-web", Name: "web", Status: "running"}},
		deployments: []Deployment{{ID: "dep-1", JobID: "panel-web", Status: "running"}},
		evaluations: []Evaluation{{ID: "eval-1", JobID: "panel-web", Status: "complete"}},
		services:    []ServiceRegistration{{ServiceName: "web", Namespace: "apps", Tags: []string{"public"}}},
	}
	handler := NewHandler(fake)

	cases := []struct {
		name string
		path string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "status", path: "/api/v1/nomad/status", call: handler.Status},
		{name: "nodes", path: "/api/v1/nomad/nodes", call: handler.Nodes},
		{name: "jobs", path: "/api/v1/nomad/jobs", call: handler.Jobs},
		{name: "deployments", path: "/api/v1/nomad/deployments", call: handler.Deployments},
		{name: "evaluations", path: "/api/v1/nomad/evaluations", call: handler.Evaluations},
		{name: "services", path: "/api/v1/nomad/services", call: handler.Services},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			tc.call(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var env httpx.Envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Data == nil {
				t.Fatal("expected data")
			}
		})
	}
}

type fakeInventoryClient struct {
	status      StatusResponse
	nodes       []NodeListItem
	jobs        []JobListItem
	deployments []Deployment
	evaluations []Evaluation
	services    []ServiceRegistration
}

func (f *fakeInventoryClient) Status(ctx context.Context) (StatusResponse, error) {
	return f.status, nil
}

func (f *fakeInventoryClient) Nodes(ctx context.Context) ([]NodeListItem, error) {
	return f.nodes, nil
}

func (f *fakeInventoryClient) ListJobs(ctx context.Context, prefix string) ([]JobListItem, error) {
	return f.jobs, nil
}

func (f *fakeInventoryClient) Deployments(ctx context.Context) ([]Deployment, error) {
	return f.deployments, nil
}

func (f *fakeInventoryClient) Evaluations(ctx context.Context) ([]Evaluation, error) {
	return f.evaluations, nil
}

func (f *fakeInventoryClient) Services(ctx context.Context) ([]ServiceRegistration, error) {
	return f.services, nil
}
