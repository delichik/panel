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
		status: StatusResponse{Connected: true, Leader: "127.0.0.1:4647"},
		nodes:  []NodeListItem{{ID: "node-1", Name: "node", Status: "ready"}},
	}
	handler := NewHandler(fake)

	cases := []struct {
		name string
		path string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "status", path: "/api/v1/nomad/status", call: handler.Status},
		{name: "nodes", path: "/api/v1/nomad/nodes", call: handler.Nodes},
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
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/redeploy-node", bytes.NewBufferString(`{"serverId":"srv_1","role":"server"}`))
	handler.RedeployNode(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("redeploy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.redeploy.ServerID != "srv_1" || fake.redeploy.Role != "server" {
		t.Fatalf("unexpected redeploy input=%#v", fake.redeploy)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/rebuild-cluster", bytes.NewBufferString(`{"serverId":"srv_1"}`))
	handler.RebuildCluster(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("rebuild status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.rebuild.ServerID != "srv_1" {
		t.Fatalf("unexpected rebuild input=%#v", fake.rebuild)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nomad/switch-server", bytes.NewBufferString(`{"serverId":"srv_1"}`))
	handler.SwitchServer(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("switch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fake.switched.ServerID != "srv_1" {
		t.Fatalf("unexpected switch input=%#v", fake.switched)
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
	var proxyEnv struct {
		Data ReverseProxyUpdateResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &proxyEnv); err != nil {
		t.Fatal(err)
	}
	if proxyEnv.Data.TaskID != "task_proxy" || proxyEnv.Data.Server.ID != "srv_1" {
		t.Fatalf("unexpected reverse proxy result=%#v", proxyEnv.Data)
	}
}

type fakeInventoryClient struct {
	status StatusResponse
	nodes  []NodeListItem
}

type fakeJoinService struct {
	candidates           []server.Server
	controlPlane         ControlPlane
	task                 tasks.Task
	bootstrap            tasks.Task
	remove               tasks.Task
	redeploy             RedeployNodeInput
	rebuild              RebuildClusterInput
	switched             SwitchServerInput
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

func (f *fakeJoinService) RedeployNode(_ context.Context, in RedeployNodeInput) (tasks.Task, error) {
	f.redeploy = in
	return tasks.Task{ID: "task_redeploy"}, nil
}

func (f *fakeJoinService) RebuildCluster(_ context.Context, in RebuildClusterInput) (tasks.Task, error) {
	f.rebuild = in
	return tasks.Task{ID: "task_rebuild"}, nil
}

func (f *fakeJoinService) SwitchServer(_ context.Context, in SwitchServerInput) (tasks.Task, error) {
	f.switched = in
	return tasks.Task{ID: "task_switch"}, nil
}

func (f *fakeJoinService) RemoveNode(_ context.Context, in RemoveNodeInput) (tasks.Task, error) {
	f.removed = in
	if f.remove.ID == "" {
		f.remove.ID = "task_remove"
	}
	return f.remove, nil
}

func (f *fakeJoinService) UpdateReverseProxy(_ context.Context, in ReverseProxyInput) (ReverseProxyUpdateResult, error) {
	f.reverseProxy = in
	return ReverseProxyUpdateResult{TaskID: "task_proxy", Server: server.Server{ID: in.ServerID, Name: "worker-1", Traits: map[string]string{
		TraitReverseProxyEnabled:     "true",
		TraitReverseProxyStaticFiles: "true",
	}}}, nil
}

func (f *fakeInventoryClient) Status(ctx context.Context) (StatusResponse, error) {
	return f.status, nil
}

func (f *fakeInventoryClient) Nodes(ctx context.Context) ([]NodeListItem, error) {
	return f.nodes, nil
}
