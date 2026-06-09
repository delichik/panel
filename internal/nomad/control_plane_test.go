package nomad

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/server"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestControlPlaneIsUnconfiguredWhenNomadUnavailableAndNoTasks(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	createControlPlaneServer(t, svc.servers, credSvc, ctx, "first", "10.0.0.30")
	fake.statusErr = errors.New("connection refused")

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ControlPlaneUnconfigured {
		t.Fatalf("status = %q", got.Status)
	}
	if len(got.BootstrapCandidates) != 1 {
		t.Fatalf("bootstrap candidates = %#v", got.BootstrapCandidates)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Kind != ProjectedNodeMissing || got.Nodes[0].Status != "nomad_unreachable" {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
}

func TestControlPlaneShowsBootstrappingServerAsPendingNode(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createControlPlaneServer(t, svc.servers, credSvc, ctx, "first", "10.0.0.31")
	fake.statusErr = errors.New("connection refused")
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeServerBootstrap, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Summary: "Bootstrapping Nomad server", Status: tasks.StatusRunning})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ControlPlaneBootstrapping {
		t.Fatalf("status = %q", got.Status)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	node := got.Nodes[0]
	if node.Kind != ProjectedNodePending || node.Role != ProjectedNodeRoleServer || node.Status != "bootstrapping" || node.TaskID != task.ID {
		t.Fatalf("unexpected projected node: %#v", node)
	}
}

func TestControlPlaneTimesOutNomadStatusAndStillReturnsProjection(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	createControlPlaneServer(t, svc.servers, credSvc, ctx, "first", "10.0.0.36")
	fake.blockStatus = true
	oldTimeout := controlPlaneNomadQueryTimeout
	controlPlaneNomadQueryTimeout = 20 * time.Millisecond
	defer func() { controlPlaneNomadQueryTimeout = oldTimeout }()

	start := time.Now()
	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("control-plane should not wait on Nomad indefinitely, took %s", elapsed)
	}
	if got.Status != ControlPlaneUnconfigured {
		t.Fatalf("status = %q", got.Status)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].Kind != ProjectedNodeMissing || got.Nodes[0].Status != "nomad_unreachable" {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	if len(got.BootstrapCandidates) != 1 {
		t.Fatalf("bootstrap candidates = %#v", got.BootstrapCandidates)
	}
}

func TestControlPlaneKeepsCompletedJoinVisibleUntilNomadNodeRegisters(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createControlPlaneServer(t, svc.servers, credSvc, ctx, "worker", "10.0.0.34")
	fake.status = StatusResponse{Connected: true, Leader: "10.0.0.1:4647"}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeClientJoin, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Summary: "Nomad client join requested", Status: tasks.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ControlPlaneConnected {
		t.Fatalf("status = %q", got.Status)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	node := got.Nodes[0]
	if node.Kind != ProjectedNodePending || node.Role != ProjectedNodeRoleClient || node.Status != "registering" || node.TaskID != task.ID {
		t.Fatalf("unexpected projected node: %#v", node)
	}
	if len(got.JoinCandidates) != 0 {
		t.Fatalf("completed join should stay out of candidates while registering: %#v", got.JoinCandidates)
	}
}

func TestControlPlaneShowsCompletedRebuildAsRegisteringUntilNodeRegisters(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createControlPlaneServer(t, svc.servers, credSvc, ctx, "control", "10.0.0.37")
	fake.status = StatusResponse{Connected: true, Leader: "10.0.0.37:4647"}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeClusterRebuild, ServerID: srv.ID, ResourceType: "nomad_cluster", ResourceID: srv.ID, Summary: "Nomad cluster rebuild requested", Status: tasks.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	node := got.Nodes[0]
	if node.Kind != ProjectedNodePending || node.Role != ProjectedNodeRoleServer || node.Status != "registering" || node.TaskID != task.ID {
		t.Fatalf("unexpected projected node: %#v", node)
	}
}

func TestControlPlaneProjectsManagedAndUnmanagedNodes(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	managed := createControlPlaneServer(t, svc.servers, credSvc, ctx, "managed", "10.0.0.32")
	unjoined := createControlPlaneServer(t, svc.servers, credSvc, ctx, "unjoined", "10.0.0.33")
	fake.status = StatusResponse{Connected: true, Leader: "10.0.0.1:4647"}
	fake.nodes = []NodeListItem{
		{ID: "node-1", Name: "managed-node", Status: "ready", Meta: map[string]string{"panel_server_id": managed.ID}},
		{ID: "node-2", Name: "manual-node", Status: "ready"},
	}

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ControlPlaneConnected {
		t.Fatalf("status = %q", got.Status)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	managedNode := findProjectedNode(got.Nodes, ProjectedNodeManaged, managed.ID)
	if managedNode == nil || managedNode.NodeID != "node-1" || managedNode.Status != "ready" {
		t.Fatalf("managed node = %#v", got.Nodes)
	}
	missingNode := findProjectedNode(got.Nodes, ProjectedNodeMissing, unjoined.ID)
	if missingNode == nil || missingNode.Status != "missing" {
		t.Fatalf("missing server node = %#v", got.Nodes)
	}
	unmanagedNode := findProjectedNode(got.Nodes, ProjectedNodeUnmanaged, "")
	if unmanagedNode == nil || unmanagedNode.NodeID != "node-2" || unmanagedNode.Status != "unmanaged" {
		t.Fatalf("unmanaged node = %#v", got.Nodes)
	}
	if len(got.JoinCandidates) != 1 || got.JoinCandidates[0].ID != unjoined.ID {
		t.Fatalf("join candidates = %#v", got.JoinCandidates)
	}
}

func TestControlPlaneHidesServerAfterCompletedRemove(t *testing.T) {
	svc, credSvc, fake, cleanup := newControlPlaneTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createControlPlaneServer(t, svc.servers, credSvc, ctx, "removed", "10.0.0.35")
	fake.status = StatusResponse{Connected: true, Leader: "10.0.0.1:4647"}
	fake.nodes = []NodeListItem{{ID: "node-removed", Name: "old-node", Status: "down", Meta: map[string]string{"panel_server_id": srv.ID}}}
	if _, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeNodeRemove,
		ServerID:     srv.ID,
		NodeID:       "node-removed",
		ResourceType: "nomad_node",
		ResourceID:   "node-removed",
		Summary:      "Nomad node remove requested",
		Status:       tasks.StatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 0 {
		t.Fatalf("removed server should be hidden from node list, got %#v", got.Nodes)
	}
	if len(got.JoinCandidates) != 1 || got.JoinCandidates[0].ID != srv.ID {
		t.Fatalf("removed server should be available to rejoin, got %#v", got.JoinCandidates)
	}
}

func findProjectedNode(nodes []ProjectedNode, kind, serverID string) *ProjectedNode {
	for i := range nodes {
		if nodes[i].Kind == kind && nodes[i].ServerID == serverID {
			return &nodes[i]
		}
	}
	return nil
}

func newControlPlaneTestService(t *testing.T) (*JoinService, *credential.Service, *controlPlaneFakeNomad, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tlsAssets, err := EnsureTLSAssets(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	credSvc := credential.NewService(store.AppDB(), cfg)
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	unregister := registerJoinTestDB(serverSvc, store.AppDB())
	fake := &controlPlaneFakeNomad{}
	return NewJoinService(serverSvc, fake, &joinFakeExecutor{}, taskSvc, cfg.Nomad, tlsAssets), credSvc, fake, func() {
		unregister()
		_ = store.Close()
	}
}

func createControlPlaneServer(t *testing.T, svc *server.Service, credSvc *credential.Service, ctx context.Context, name, host string) server.Server {
	t.Helper()
	cred, err := credSvc.Create(ctx, credential.CreateRequest{Name: name + "-cred", Type: credential.TypePassword, Username: "root", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := svc.Create(ctx, server.SaveRequest{Name: name, Host: host, Port: 22, SSHUsername: "root", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	markJoinTestServerEligible(t, svc, srv.ID)
	stored, err := svc.Get(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

type controlPlaneFakeNomad struct {
	status      StatusResponse
	statusErr   error
	blockStatus bool
	nodes       []NodeListItem
	nodesErr    error
}

func (f *controlPlaneFakeNomad) Status(ctx context.Context) (StatusResponse, error) {
	if f.blockStatus {
		<-ctx.Done()
		return StatusResponse{Connected: false}, ctx.Err()
	}
	return f.status, f.statusErr
}

func (f *controlPlaneFakeNomad) Nodes(context.Context) ([]NodeListItem, error) {
	return f.nodes, f.nodesErr
}
