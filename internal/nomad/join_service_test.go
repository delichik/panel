package nomad

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestJoinCandidatesExcludeServersAlreadyLinkedByNomadMeta(t *testing.T) {
	svc, credSvc, fake, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	linked := createJoinTestServer(t, svc.servers, credSvc, ctx, "linked", "10.0.0.10")
	unlinked := createJoinTestServer(t, svc.servers, credSvc, ctx, "unlinked", "10.0.0.11")
	fake.nodes = []NodeListItem{{ID: "node-1", Meta: map[string]string{"panel_server_id": linked.ID}}}

	got, err := svc.Candidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != unlinked.ID {
		t.Fatalf("expected only unlinked server, got %#v", got)
	}
}

func TestJoinCandidatesIgnoreNodeAfterCompletedRemove(t *testing.T) {
	svc, credSvc, fake, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "removed", "10.0.0.13")
	fake.nodes = []NodeListItem{{ID: "node-removed", Meta: map[string]string{"panel_server_id": srv.ID}}}
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

	got, err := svc.Candidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != srv.ID {
		t.Fatalf("expected removed server to be joinable, got %#v", got)
	}
}

func TestJoinClientCreatesTask(t *testing.T) {
	svc, credSvc, _, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "worker one", "10.0.0.12")

	task, err := svc.JoinClient(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}

	if task.Type != TaskTypeClientJoin || task.ResourceType != "server" || task.ResourceID != srv.ID {
		t.Fatalf("unexpected task metadata: %#v", task)
	}
}

func TestRunJoinClientRunsNomadClientScript(t *testing.T) {
	svc, credSvc, _, fake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "worker one", "10.0.0.12")
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeClientJoin, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Summary: "Joining server to Nomad"})
	if err != nil {
		t.Fatal(err)
	}

	svc.runJoinClient(ctx, task.ID, srv)

	if len(fake.sudoCommands) != 1 {
		t.Fatalf("expected one sudo command, got %#v", fake.sudoCommands)
	}
	command := fake.sudoCommands[0]
	for _, want := range []string{
		"command -v nomad",
		"apt-get install -y docker.io",
		"apt-get install -y containernetworking-plugins",
		"find /etc/nomad.d -maxdepth 1 -type f",
		"-name '*.hcl'",
		"-name '*.json'",
		"server {",
		"enabled = false",
		"panel_server_id = \"" + srv.ID + "\"",
		`servers = ["10.0.0.1:4647"]`,
		"systemctl restart nomad",
		"systemctl is-active --quiet nomad",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("join script missing %q:\n%s", want, command)
		}
	}
	if strings.Contains(command, "\nserver_join {") {
		t.Fatalf("join script should not write a top-level server_join block:\n%s", command)
	}
	waitForTaskStatus(t, svc.tasks, ctx, task.ID, tasks.StatusCompleted)
}

func TestRunJoinClientInfersBootstrappedServerWhenConfigIsLocal(t *testing.T) {
	svc, credSvc, _, fake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	svc.cfg.Address = "http://127.0.0.1:4646"
	control := createJoinTestServer(t, svc.servers, credSvc, ctx, "control one", "10.0.0.20")
	worker := createJoinTestServer(t, svc.servers, credSvc, ctx, "worker one", "10.0.0.12")
	if _, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeServerBootstrap,
		ServerID:     control.ID,
		ResourceType: "server",
		ResourceID:   control.ID,
		Status:       tasks.StatusCompleted,
		Summary:      "Nomad server bootstrap requested",
	}); err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeClientJoin, ServerID: worker.ID, ResourceType: "server", ResourceID: worker.ID, Summary: "Joining server to Nomad"})
	if err != nil {
		t.Fatal(err)
	}

	svc.runJoinClient(ctx, task.ID, worker)

	if len(fake.sudoCommands) != 1 {
		t.Fatalf("expected one sudo command, got %#v", fake.sudoCommands)
	}
	if command := fake.sudoCommands[0]; !strings.Contains(command, `servers = ["10.0.0.20:4647"]`) {
		t.Fatalf("expected join script to use bootstrapped server host:\n%s", command)
	}
}

func TestControlPlaneRestoresNomadAddressFromCompletedBootstrap(t *testing.T) {
	svc, credSvc, nomadFake, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	svc.cfg.Address = "http://127.0.0.1:4646"
	control := createJoinTestServer(t, svc.servers, credSvc, ctx, "control one", "10.0.0.20")
	if _, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeServerBootstrap,
		ServerID:     control.ID,
		ResourceType: "server",
		ResourceID:   control.ID,
		Status:       tasks.StatusCompleted,
		Summary:      "Nomad server bootstrap requested",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.ControlPlane(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if nomadFake.address != "http://10.0.0.20:4646" {
		t.Fatalf("expected restored nomad address, got %q", nomadFake.address)
	}
}

func TestRunJoinClientStreamsScriptOutputBeforeCommandReturns(t *testing.T) {
	svc, credSvc, _, fake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "worker one", "10.0.0.12")
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{Type: TaskTypeClientJoin, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Summary: "Joining server to Nomad"})
	if err != nil {
		t.Fatal(err)
	}
	fake.stdoutLines = []string{"installing nomad", "nomad started"}
	fake.stderrLines = []string{"warning"}
	fake.duringRun = func() {
		logs, _, err := svc.tasks.Logs(ctx, task.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(logs) != 2 || logs[1].Stream != "stdout" || logs[1].Line != "installing nomad" {
			t.Fatalf("expected stdout to be logged while command is still running, got %#v", logs)
		}
	}

	svc.runJoinClient(ctx, task.ID, srv)

	logs, _, err := svc.tasks.Logs(ctx, task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, log := range logs {
		got = append(got, log.Stream+":"+log.Line)
	}
	for _, want := range []string{"stdout:installing nomad", "stderr:warning", "stdout:nomad started"} {
		found := false
		for _, line := range got {
			if line == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing streamed line %q in %#v", want, got)
		}
	}
}

func TestBootstrapServerCreatesTaskAndRunsNomadServerScript(t *testing.T) {
	svc, credSvc, nomadFake, execFake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "control one", "10.0.0.20")
	execFake.sudoCalled = make(chan struct{}, 1)

	task, err := svc.BootstrapServer(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-execFake.sudoCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bootstrap sudo command")
	}

	if task.Type != TaskTypeServerBootstrap || task.ResourceType != "server" || task.ResourceID != srv.ID {
		t.Fatalf("unexpected task metadata: %#v", task)
	}
	if nomadFake.address != "http://10.0.0.20:4646" {
		t.Fatalf("expected runtime Nomad address to point at bootstrapped server, got %q", nomadFake.address)
	}
	if len(execFake.sudoCommands) != 1 {
		t.Fatalf("expected one sudo command, got %#v", execFake.sudoCommands)
	}
	command := execFake.sudoCommands[0]
	for _, want := range []string{
		"server {",
		"bootstrap_expect = 1",
		"client {",
		"apt-get install -y docker.io",
		"apt-get install -y containernetworking-plugins",
		"find /etc/nomad.d -maxdepth 1 -type f",
		"-name '*.hcl'",
		"-name '*.json'",
		"panel_server_id = \"" + srv.ID + "\"",
		"systemctl restart nomad",
		"systemctl is-active --quiet nomad",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("bootstrap script missing %q:\n%s", want, command)
		}
	}
	waitForTaskStatus(t, svc.tasks, ctx, task.ID, tasks.StatusCompleted)
}

func TestRemoveManagedNodeStopsServiceAndPurgesNomadNode(t *testing.T) {
	svc, credSvc, nomadFake, execFake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "worker one", "10.0.0.30")

	task, err := svc.RemoveNode(ctx, RemoveNodeInput{ServerID: srv.ID, NodeID: "node-1"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, svc.tasks, ctx, task.ID, tasks.StatusCompleted)

	if len(execFake.sudoCommands) != 1 || !strings.Contains(execFake.sudoCommands[0], "systemctl disable --now nomad") {
		t.Fatalf("expected nomad stop command, got %#v", execFake.sudoCommands)
	}
	if command := execFake.sudoCommands[0]; !strings.Contains(command, "find /etc/nomad.d -maxdepth 1 -type f") {
		t.Fatalf("expected nomad config cleanup command, got %s", command)
	}
	if len(nomadFake.purgedNodes) != 1 || nomadFake.purgedNodes[0] != "node-1" {
		t.Fatalf("purged nodes = %#v", nomadFake.purgedNodes)
	}
	stored, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed task, got %#v", stored)
	}
}

func TestRemoveUnmanagedNodePurgesOnlyNomadNode(t *testing.T) {
	svc, _, nomadFake, execFake, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()

	task, err := svc.RemoveNode(ctx, RemoveNodeInput{NodeID: "node-2"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, svc.tasks, ctx, task.ID, tasks.StatusCompleted)

	if len(execFake.sudoCommands) != 0 {
		t.Fatalf("unexpected ssh commands = %#v", execFake.sudoCommands)
	}
	if len(nomadFake.purgedNodes) != 1 || nomadFake.purgedNodes[0] != "node-2" {
		t.Fatalf("purged nodes = %#v", nomadFake.purgedNodes)
	}
}

func TestUpdateReverseProxyStoresNodeConfigAndRegistersNginxJob(t *testing.T) {
	svc, credSvc, nomadFake, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "edge one", "10.0.0.40")
	svc.SetApplicationProxySource(staticApplicationProxySource{configs: []ApplicationReverseProxyConfig{{
		ApplicationID:   "app-1",
		ApplicationName: "web",
		DeploymentMode:  "all",
		Routes: []ReverseProxyRoute{{
			Domain:     "app.example.com",
			TargetPort: 8080,
			Paths:      []ReverseProxyPath{{Path: "/", WebSocket: true}, {Path: "/api"}},
		}},
	}}})

	updated, err := svc.UpdateReverseProxy(ctx, ReverseProxyInput{
		ServerID: srv.ID,
		Enabled:  true,
		StaticSites: []ReverseProxyStaticSite{{
			Domain: "static.example.com",
			Root:   "/srv/www/static",
			Index:  "index.html index.htm",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !traitBool(updated.Traits, TraitReverseProxyEnabled) || !traitBool(updated.Traits, TraitReverseProxyStaticFiles) {
		t.Fatalf("reverse proxy traits = %#v", updated.Traits)
	}
	if nomadFake.registeredJob.ID != reverseProxyJobID || len(nomadFake.registeredJob.TaskGroups) != 1 {
		t.Fatalf("registered job = %#v", nomadFake.registeredJob)
	}
	group := nomadFake.registeredJob.TaskGroups[0]
	if len(group.Constraints) != 1 || group.Constraints[0].RTarget != srv.ID {
		t.Fatalf("job constraints = %#v", group.Constraints)
	}
	task := group.Tasks[0]
	config := joinTemplateContent(task.Templates)
	for _, want := range []string{"include /local/nginx.conf.d/*.conf;", "server_name app.example.com;", "proxy_pass http://127.0.0.1:8080;", "proxy_set_header Upgrade $http_upgrade;", "server_name static.example.com;", "root /panel-static/static-0;", "try_files $uri $uri/ /index.html;"} {
		if !strings.Contains(config, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, config)
		}
	}
	if !hasTemplateDest(task.Templates, "local/nginx.conf.d/panel-empty.conf") {
		t.Fatalf("expected empty include template, got %#v", task.Templates)
	}
	mounts, ok := task.Config["mounts"].([]map[string]any)
	if !ok || len(mounts) != 1 || mounts[0]["source"] != "/srv/www/static" {
		t.Fatalf("static mounts = %#v", task.Config["mounts"])
	}
}

func TestUpdateReverseProxyWithoutRoutesStillCreatesNginxIncludeDirectory(t *testing.T) {
	svc, credSvc, nomadFake, _, cleanup := newJoinTestService(t)
	defer cleanup()
	ctx := context.Background()
	srv := createJoinTestServer(t, svc.servers, credSvc, ctx, "edge one", "10.0.0.41")

	if _, err := svc.UpdateReverseProxy(ctx, ReverseProxyInput{ServerID: srv.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if nomadFake.registeredJob.ID != reverseProxyJobID || len(nomadFake.registeredJob.TaskGroups) != 1 {
		t.Fatalf("registered job = %#v", nomadFake.registeredJob)
	}
	task := nomadFake.registeredJob.TaskGroups[0].Tasks[0]
	if !hasTemplateDest(task.Templates, "local/nginx.conf.d/panel-empty.conf") {
		t.Fatalf("expected empty include template, got %#v", task.Templates)
	}
	if !strings.Contains(joinTemplateContent(task.Templates), "return 404;") {
		t.Fatalf("base nginx config missing default server: %#v", task.Templates)
	}
}

func joinTemplateContent(templates []Template) string {
	parts := make([]string, 0, len(templates))
	for _, template := range templates {
		parts = append(parts, template.EmbeddedTmpl)
	}
	return strings.Join(parts, "\n")
}

func hasTemplateDest(templates []Template, dest string) bool {
	for _, template := range templates {
		if template.DestPath == dest {
			return true
		}
	}
	return false
}

type staticApplicationProxySource struct {
	configs []ApplicationReverseProxyConfig
}

func (s staticApplicationProxySource) ApplicationReverseProxyConfigs(context.Context) ([]ApplicationReverseProxyConfig, error) {
	return s.configs, nil
}

func waitForTaskStatus(t *testing.T, svc *tasks.Service, ctx context.Context, id, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := svc.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("task %s status = %s, want %s", id, task.Status, status)
}

func newJoinTestService(t *testing.T) (*JoinService, *credential.Service, *joinFakeNomadClient, *joinFakeExecutor, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.Nomad.Address = "http://10.0.0.1:4646"
	cfg.Nomad.Datacenter = "dc1"
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &joinFakeExecutor{}
	credSvc := credential.NewService(store.AppDB(), cfg)
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	nomadClient := &joinFakeNomadClient{}
	return NewJoinService(serverSvc, nomadClient, exec, taskSvc, cfg.Nomad), credSvc, nomadClient, exec, func() { _ = store.Close() }
}

func createJoinTestServer(t *testing.T, svc *server.Service, credSvc *credential.Service, ctx context.Context, name, host string) server.Server {
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

type joinFakeNomadClient struct {
	nodes         []NodeListItem
	address       string
	purgedNodes   []string
	registeredJob Job
	stoppedJobs   []string
}

func (f *joinFakeNomadClient) Nodes(context.Context) ([]NodeListItem, error) {
	return f.nodes, nil
}

func (f *joinFakeNomadClient) SetAddress(address string) {
	f.address = address
}

func (f *joinFakeNomadClient) PurgeNode(_ context.Context, id string) error {
	f.purgedNodes = append(f.purgedNodes, id)
	return nil
}

func (f *joinFakeNomadClient) ValidateJob(context.Context, Job) (ValidateResponse, error) {
	return ValidateResponse{}, nil
}

func (f *joinFakeNomadClient) PlanJob(_ context.Context, _ string, _ Job) (PlanResponse, error) {
	return PlanResponse{}, nil
}

func (f *joinFakeNomadClient) RegisterJob(_ context.Context, _ string, job Job) (RegisterResponse, error) {
	f.registeredJob = job
	return RegisterResponse{EvalID: "eval-proxy"}, nil
}

func (f *joinFakeNomadClient) StopJob(_ context.Context, id string, _ bool) (StopResponse, error) {
	f.stoppedJobs = append(f.stoppedJobs, id)
	return StopResponse{}, nil
}

type joinFakeExecutor struct {
	sudoCommands []string
	stdoutLines  []string
	stderrLines  []string
	duringRun    func()
	sudoCalled   chan struct{}
}

func (f *joinFakeExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *joinFakeExecutor) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
	if f.sudoCalled != nil {
		select {
		case f.sudoCalled <- struct{}{}:
		default:
		}
	}
	var stdout, stderr []string
	if len(f.stdoutLines) > 0 {
		command.OnStdout(f.stdoutLines[0])
		stdout = append(stdout, f.stdoutLines[0])
		if f.duringRun != nil {
			f.duringRun()
		}
		for _, line := range f.stdoutLines[1:] {
			command.OnStdout(line)
			stdout = append(stdout, line)
		}
	} else if f.duringRun != nil {
		f.duringRun()
	}
	for _, line := range f.stderrLines {
		command.OnStderr(line)
		stderr = append(stderr, line)
	}
	return sshx.CommandResult{Stdout: strings.Join(stdout, "\n"), Stderr: strings.Join(stderr, "\n"), ExitCode: 0}, nil
}

func (f *joinFakeExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *joinFakeExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
