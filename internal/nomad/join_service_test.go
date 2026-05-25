package nomad

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
		"panel_server_id = \"" + srv.ID + "\"",
		"server_join",
		"10.0.0.1:4647",
		"systemctl restart nomad",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("join script missing %q:\n%s", want, command)
		}
	}
	stored, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed task, got %#v", stored)
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

	task, err := svc.BootstrapServer(ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc.runBootstrapServer(ctx, task.ID, srv)

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
		"panel_server_id = \"" + srv.ID + "\"",
		"systemctl restart nomad",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("bootstrap script missing %q:\n%s", want, command)
		}
	}
	stored, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed task, got %#v", stored)
	}
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
	nodes   []NodeListItem
	address string
}

func (f *joinFakeNomadClient) Nodes(context.Context) ([]NodeListItem, error) {
	return f.nodes, nil
}

func (f *joinFakeNomadClient) SetAddress(address string) {
	f.address = address
}

type joinFakeExecutor struct {
	sudoCommands []string
	stdoutLines  []string
	stderrLines  []string
	duringRun    func()
}

func (f *joinFakeExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *joinFakeExecutor) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
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
