package compose

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

type noopExecutor struct{}

func (noopExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}
func (noopExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}
func (noopExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error     { return nil }
func (noopExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error { return nil }

type recordingExecutor struct {
	mu       sync.Mutex
	commands []string
}

func (r *recordingExecutor) Exec(_ context.Context, _ sshx.Target, cmd sshx.CommandSpec) (sshx.CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, cmd.Command)
	return sshx.CommandResult{}, nil
}
func (r *recordingExecutor) ExecSudo(ctx context.Context, target sshx.Target, cmd sshx.CommandSpec) (sshx.CommandResult, error) {
	return r.Exec(ctx, target, cmd)
}
func (r *recordingExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error { return nil }
func (r *recordingExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func TestRenderRequiresMissingVariables(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "web",
		ComposeYAML: "services:\n  web:\n    image: {{ .image }}\n",
		Variables:   []TemplateVariable{{Key: "image", Required: true}},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, err = svc.RenderTemplate(ctx, tpl.ID, RenderRequest{})
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}

func TestTemplateUpdateMarksLinkedServicesDrifted(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\n", Variables: []TemplateVariable{{Key: "image", Required: true}}})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web", Values: map[string]any{"image": "nginx:1"}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	updated, err := svc.UpdateTemplate(ctx, tpl.ID, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\nname: v2\n", Variables: []TemplateVariable{{Key: "image", Required: true}}})
	if err != nil {
		t.Fatalf("update template: %v", err)
	}
	if updated.Version != tpl.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, tpl.Version+1)
	}
	got, err := svc.GetService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if !got.Drifted {
		t.Fatal("linked service must be marked drifted")
	}
}

func TestFilesServerVariablesAndLifecycleStayLocal(t *testing.T) {
	ctx := context.Background()
	svc, srv, taskSvc := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := svc.CreateTemplateFile(ctx, tpl.ID, FileKindTemplate, SaveFileRequest{Path: "conf/app.conf", Body: "PORT={{ .port }}"}); err != nil {
		t.Fatalf("create template file: %v", err)
	}
	if _, err := svc.CreateTemplateFile(ctx, tpl.ID, FileKindBinary, SaveFileRequest{Path: "static/logo.bin", Base64: "AAECAw=="}); err != nil {
		t.Fatalf("create binary file: %v", err)
	}
	if _, err := svc.PutServerVariables(ctx, srv.ID, map[string]any{"port": 8080}); err != nil {
		t.Fatalf("put server vars: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web", Values: map[string]any{"image": "nginx:1"}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	rendered, err := svc.RenderService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("render service: %v", err)
	}
	if rendered.ComposeYAML != "image: nginx:1\n" || len(rendered.Files) != 2 {
		t.Fatalf("unexpected render result: %#v", rendered)
	}

	task, err := svc.LifecycleTask(ctx, deployed.ID, "deploy")
	if err != nil {
		t.Fatalf("deploy task: %v", err)
	}
	waitForTask(t, taskSvc, task.ID)
	logs, _, err := taskSvc.Logs(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	joined := ""
	for _, l := range logs {
		joined += l.Line + "\n"
	}
	if !strings.Contains(joined, "local metadata closed loop") || !strings.Contains(joined, "remote docker compose execution is not connected yet") {
		t.Fatalf("task logs do not describe local closure: %s", joined)
	}
	got, err := svc.GetService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if got.Status != ServiceStatusReady || got.Drifted {
		t.Fatalf("service status=%s drifted=%v", got.Status, got.Drifted)
	}
}

func TestDeployRunsRemoteComposeWhenExecutorIsConfigured(t *testing.T) {
	ctx := context.Background()
	svc, srv, taskSvc, exec := testServiceWithRecordingExec(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "services:\n  web:\n    image: {{ .image }}\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web", Values: map[string]any{"image": "nginx:1"}})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	task, err := svc.LifecycleTask(ctx, deployed.ID, "deploy")
	if err != nil {
		t.Fatalf("deploy task: %v", err)
	}
	waitForTask(t, taskSvc, task.ID)
	joined := strings.Join(exec.commands, "\n")
	if !strings.Contains(joined, "compose.yaml") || !strings.Contains(joined, "docker compose pull && docker compose up -d") {
		t.Fatalf("expected remote compose deployment commands, got:\n%s", joined)
	}
}

func testService(t *testing.T) (*Service, server.Server, *tasks.Service) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','test','password','root','now','now')`)
	if err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	serverSvc := server.NewService(store.AppDB(), noopExecutor{}, taskSvc)
	srv, err := serverSvc.Create(context.Background(), server.SaveRequest{Name: "srv", Host: "127.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return NewService(store.AppDB(), cfg.DataRoot, serverSvc, taskSvc), srv, taskSvc
}

func testServiceWithRecordingExec(t *testing.T) (*Service, server.Server, *tasks.Service, *recordingExecutor) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','test','password','root','now','now')`)
	if err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &recordingExecutor{}
	serverSvc := server.NewService(store.AppDB(), exec, taskSvc)
	srv, err := serverSvc.Create(context.Background(), server.SaveRequest{Name: "srv", Host: "127.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return NewService(store.AppDB(), cfg.DataRoot, serverSvc, taskSvc, exec), srv, taskSvc, exec
}

func waitForTask(t *testing.T, svc *tasks.Service, taskID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := svc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed {
			if task.Status == tasks.StatusFailed {
				t.Fatalf("task failed: %s", task.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}
