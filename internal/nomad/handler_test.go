package nomad

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/httpx"
	"panel/internal/server"
	"panel/internal/tasks"
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

func TestHandlerJoinCandidatesAndJoin(t *testing.T) {
	fake := &fakeJoinService{
		candidates: []server.Server{{ID: "srv_1", Name: "worker-1", Host: "10.0.0.10", Port: 22}},
		controlPlane: ControlPlane{
			Status: ControlPlaneBootstrapping,
			Nodes: []ProjectedNode{{
				Kind:     ProjectedNodePending,
				ServerID: "srv_1",
				Name:     "worker-1",
				Role:     ProjectedNodeRoleServer,
				Status:   "bootstrapping",
				TaskID:   "task_bootstrap",
			}},
			BootstrapCandidates: []server.Server{{ID: "srv_1", Name: "worker-1", Host: "10.0.0.10", Port: 22}},
		},
		task:      tasks.Task{ID: "task_1"},
		bootstrap: tasks.Task{ID: "task_bootstrap"},
	}
	handler := NewHandler(&fakeInventoryClient{}, fake)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nomad/join-candidates", nil)
	handler.JoinCandidates(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("candidates status = %d body=%s", rec.Code, rec.Body.String())
	}
	var listEnv struct {
		Data []server.Server `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listEnv); err != nil {
		t.Fatal(err)
	}
	if len(listEnv.Data) != 1 || listEnv.Data[0].ID != "srv_1" {
		t.Fatalf("unexpected candidates: %#v", listEnv.Data)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/nomad/control-plane", nil)
	handler.ControlPlane(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("control-plane status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cpEnv struct {
		Data ControlPlane `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cpEnv); err != nil {
		t.Fatal(err)
	}
	if cpEnv.Data.Status != ControlPlaneBootstrapping || len(cpEnv.Data.Nodes) != 1 || cpEnv.Data.Nodes[0].TaskID != "task_bootstrap" {
		t.Fatalf("unexpected control-plane result: %#v", cpEnv.Data)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/join", bytes.NewBufferString(`{"serverId":"srv_1"}`))
	handler.JoinClient(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("join status = %d body=%s", rec.Code, rec.Body.String())
	}
	var joinEnv struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &joinEnv); err != nil {
		t.Fatal(err)
	}
	if fake.joinedServerID != "srv_1" || joinEnv.Data["taskId"] != "task_1" {
		t.Fatalf("unexpected join result joined=%q body=%#v", fake.joinedServerID, joinEnv.Data)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/bootstrap-server", bytes.NewBufferString(`{"serverId":"srv_1"}`))
	handler.BootstrapServer(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("bootstrap status = %d body=%s", rec.Code, rec.Body.String())
	}
	var bootstrapEnv struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bootstrapEnv); err != nil {
		t.Fatal(err)
	}
	if fake.bootstrappedServerID != "srv_1" || bootstrapEnv.Data["taskId"] != "task_bootstrap" {
		t.Fatalf("unexpected bootstrap result server=%q body=%#v", fake.bootstrappedServerID, bootstrapEnv.Data)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/remove-node", bytes.NewBufferString(`{"serverId":"srv_1","nodeId":"node_1"}`))
	handler.RemoveNode(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("remove status = %d body=%s", rec.Code, rec.Body.String())
	}
	var removeEnv struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &removeEnv); err != nil {
		t.Fatal(err)
	}
	if fake.removed.ServerID != "srv_1" || fake.removed.NodeID != "node_1" || removeEnv.Data["taskId"] != "task_remove" {
		t.Fatalf("unexpected remove result input=%#v body=%#v", fake.removed, removeEnv.Data)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/nomad/reverse-proxy", bytes.NewBufferString(`{"serverId":"srv_1","enabled":true,"staticFiles":true}`))
	handler.UpdateReverseProxy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reverse proxy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.reverseProxy.ServerID != "srv_1" || !fake.reverseProxy.Enabled || !fake.reverseProxy.StaticFiles {
		t.Fatalf("unexpected reverse proxy input=%#v", fake.reverseProxy)
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

type fakeJoinService struct {
	candidates           []server.Server
	controlPlane         ControlPlane
	task                 tasks.Task
	bootstrap            tasks.Task
	remove               tasks.Task
	removed              RemoveNodeInput
	reverseProxy         ReverseProxyInput
	joinedServerID       string
	bootstrappedServerID string
}

func (f *fakeJoinService) Candidates(context.Context) ([]server.Server, error) {
	return f.candidates, nil
}

func (f *fakeJoinService) ControlPlane(context.Context) (ControlPlane, error) {
	return f.controlPlane, nil
}

func (f *fakeJoinService) JoinClient(_ context.Context, serverID string) (tasks.Task, error) {
	f.joinedServerID = serverID
	return f.task, nil
}

func (f *fakeJoinService) BootstrapServer(_ context.Context, serverID string) (tasks.Task, error) {
	f.bootstrappedServerID = serverID
	return f.bootstrap, nil
}

func (f *fakeJoinService) RemoveNode(_ context.Context, in RemoveNodeInput) (tasks.Task, error) {
	f.removed = in
	if f.remove.ID == "" {
		f.remove.ID = "task_remove"
	}
	return f.remove, nil
}

func (f *fakeJoinService) UpdateReverseProxy(_ context.Context, in ReverseProxyInput) (server.Server, error) {
	f.reverseProxy = in
	return server.Server{ID: in.ServerID, Name: "worker-1", Traits: map[string]string{
		TraitReverseProxyEnabled:     "true",
		TraitReverseProxyStaticFiles: "true",
	}}, nil
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
