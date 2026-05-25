package nomad

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
	if len(got.Nodes) != 0 {
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
	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %#v", got.Nodes)
	}
	if got.Nodes[0].Kind != ProjectedNodeManaged || got.Nodes[0].ServerID != managed.ID {
		t.Fatalf("managed node = %#v", got.Nodes[0])
	}
	if got.Nodes[1].Kind != ProjectedNodeUnmanaged || got.Nodes[1].ServerID != "" || got.Nodes[1].Status != "unmanaged" {
		t.Fatalf("unmanaged node = %#v", got.Nodes[1])
	}
	if len(got.JoinCandidates) != 1 || got.JoinCandidates[0].ID != unjoined.ID {
		t.Fatalf("join candidates = %#v", got.JoinCandidates)
	}
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
	taskSvc := tasks.NewService(store.AppDB())
	credSvc := credential.NewService(store.AppDB(), cfg)
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	fake := &controlPlaneFakeNomad{}
	return NewJoinService(serverSvc, fake, &joinFakeExecutor{}, taskSvc, cfg.Nomad), credSvc, fake, func() { _ = store.Close() }
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
	return srv
}

type controlPlaneFakeNomad struct {
	status    StatusResponse
	statusErr error
	nodes     []NodeListItem
	nodesErr  error
}

func (f *controlPlaneFakeNomad) Status(context.Context) (StatusResponse, error) {
	return f.status, f.statusErr
}

func (f *controlPlaneFakeNomad) Nodes(context.Context) ([]NodeListItem, error) {
	return f.nodes, f.nodesErr
}
