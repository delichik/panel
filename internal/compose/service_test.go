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
		ComposeYAML: "image: {{ .image }}\n",
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

func TestTemplateYAMLIsServiceBodyAndRenderWrapsComposeDocument(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "web",
		ComposeYAML: "image: {{ .image }}\nrestart: unless-stopped\nports:\n  - \"8080:80\"\n",
		Variables:   []TemplateVariable{{Key: "image", Required: true, Default: "nginx:latest"}},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	rendered, err := svc.RenderTemplate(ctx, tpl.ID, RenderRequest{ServerID: srv.ID})
	if err != nil {
		t.Fatalf("render template: %v", err)
	}

	if strings.Contains(tpl.ComposeYAML, "services:") {
		t.Fatalf("stored template YAML should be service body only:\n%s", tpl.ComposeYAML)
	}
	for _, want := range []string{
		"services:",
		"  web:",
		"    image: nginx:latest",
		"    restart: unless-stopped",
	} {
		if !strings.Contains(rendered.ComposeYAML, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, rendered.ComposeYAML)
		}
	}
}

func TestCreateTemplateRejectsInvalidComposeYAML(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := testService(t)

	_, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "broken",
		ComposeYAML: "image: [unterminated\n",
	})
	if err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Fatalf("expected YAML validation error, got %v", err)
	}

	_, err = svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "missing-services",
		ComposeYAML: "restart: unless-stopped\n",
	})
	if err == nil || !strings.Contains(err.Error(), "image or build") {
		t.Fatalf("expected service body validation error, got %v", err)
	}
}

func TestCreateTemplateRequiresComposeSafeServiceName(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := testService(t)

	_, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "Web App",
		ComposeYAML: "image: nginx:latest\n",
	})
	if err == nil || !strings.Contains(err.Error(), "service name") {
		t.Fatalf("expected service name validation error, got %v", err)
	}
}

func TestRenderIncludesBuiltInServerAndFileVariables(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name: "builtins",
		ComposeYAML: strings.Join([]string{
			"image: nginx:latest",
			"environment:",
			"  SERVER_NAME: {{ .server.name }}",
			"  SERVER_HOST: {{ .server.host }}",
			"  SERVER_COUNT: \"{{ len .servers }}\"",
			"  FIRST_FILE: {{ (index .files 0).path }}",
			"",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := svc.CreateTemplateFile(ctx, tpl.ID, FileKindTemplate, SaveFileRequest{
		Path: "conf/{{ .server.name }}/app.conf",
		Body: "host={{ .server.host }} user={{ .server.sshUsername }}",
	}); err != nil {
		t.Fatalf("create template file: %v", err)
	}

	rendered, err := svc.RenderTemplate(ctx, tpl.ID, RenderRequest{ServerID: srv.ID})
	if err != nil {
		t.Fatalf("render template: %v", err)
	}
	if !strings.Contains(rendered.ComposeYAML, "SERVER_NAME: srv") || !strings.Contains(rendered.ComposeYAML, "SERVER_COUNT: \"1\"") {
		t.Fatalf("compose YAML missing built-ins:\n%s", rendered.ComposeYAML)
	}
	if len(rendered.Files) != 1 {
		t.Fatalf("rendered files = %d, want 1", len(rendered.Files))
	}
	if rendered.Files[0].Path != "conf/srv/app.conf" {
		t.Fatalf("rendered file path = %q, want conf/srv/app.conf", rendered.Files[0].Path)
	}
	if rendered.Files[0].Content != "host=127.0.0.1 user=root" {
		t.Fatalf("rendered file content = %q", rendered.Files[0].Content)
	}
}

func TestTemplateUpdateMarksLinkedServicesDrifted(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\n", Variables: []TemplateVariable{{Key: "image", Required: true, Default: "nginx:1"}}})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web"})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	updated, err := svc.UpdateTemplate(ctx, tpl.ID, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\nlabels:\n  version: v2\n", Variables: []TemplateVariable{{Key: "image", Required: true}}})
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

func TestTemplateDependenciesRejectSelfAndCycles(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := testService(t)
	base, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "db",
		ComposeYAML: "image: postgres:16\n",
	})
	if err != nil {
		t.Fatalf("create base template: %v", err)
	}

	_, err = svc.UpdateTemplate(ctx, base.ID, SaveTemplateRequest{
		Name:         "db",
		ComposeYAML:  "image: postgres:16\n",
		Dependencies: []string{base.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "depend on itself") {
		t.Fatalf("expected self dependency validation error, got %v", err)
	}

	api, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:         "api",
		ComposeYAML:  "image: nginx:latest\n",
		Dependencies: []string{base.ID},
	})
	if err != nil {
		t.Fatalf("create api template: %v", err)
	}
	_, err = svc.UpdateTemplate(ctx, base.ID, SaveTemplateRequest{
		Name:         "db",
		ComposeYAML:  "image: postgres:16\n",
		Dependencies: []string{api.ID},
	})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle validation error, got %v", err)
	}
}

func TestCreateServiceRejectsDuplicateTemplateOnSameServer(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: nginx:latest\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web"}); err != nil {
		t.Fatalf("create first service: %v", err)
	}
	_, err = svc.CreateService(ctx, SaveServiceRequest{Name: "web-again", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web-again"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate service validation error, got %v", err)
	}
}

func TestCreateServiceAutomaticallyStartsDeployTask(t *testing.T) {
	ctx := context.Background()
	svc, srv, taskSvc := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: nginx:latest\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if deployed.LastTaskID == "" {
		t.Fatalf("created service must expose auto deploy task id: %#v", deployed)
	}

	task, err := taskSvc.Get(ctx, deployed.LastTaskID)
	if err != nil {
		t.Fatalf("get auto deploy task: %v", err)
	}
	if task.Type != "compose_service_deploy" {
		t.Fatalf("task type = %q, want compose_service_deploy", task.Type)
	}
	waitForTask(t, taskSvc, task.ID)
	got, err := svc.GetService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if got.Status != ServiceStatusReady || got.LastTaskID != task.ID {
		t.Fatalf("service status=%s lastTaskId=%s, want ready and %s", got.Status, got.LastTaskID, task.ID)
	}
}

func TestRemoveServiceDeletesRecordAndAllowsRecreate(t *testing.T) {
	ctx := context.Background()
	svc, srv, taskSvc := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: nginx:latest\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	task, err := svc.LifecycleTask(ctx, deployed.ID, "remove")
	if err != nil {
		t.Fatalf("remove task: %v", err)
	}
	waitForTask(t, taskSvc, task.ID)
	if _, err := svc.GetService(ctx, deployed.ID); err == nil {
		t.Fatal("removed service should no longer be returned")
	}
	if _, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID}); err != nil {
		t.Fatalf("recreate after remove: %v", err)
	}
}

func TestServiceDefaultsAndLabelsMatchTemplateAssociationContract(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: nginx:latest\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if deployed.RemotePath != "/opt/panel/services/web" {
		t.Fatalf("remote path = %q, want /opt/panel/services/web", deployed.RemotePath)
	}
	rendered, err := svc.RenderService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("render service: %v", err)
	}
	for _, want := range []string{
		"panel.service_id:",
		"panel.template_id:",
		"panel.template_name: web",
		"panel.template_version:",
		"panel.server_id:",
	} {
		if !strings.Contains(rendered.ComposeYAML, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, rendered.ComposeYAML)
		}
	}
	if rendered.Values["system_template_name"] != "web" {
		t.Fatalf("system_template_name = %#v, want web", rendered.Values["system_template_name"])
	}
}

func TestListServicesMergesRuntimeLabels(t *testing.T) {
	ctx := context.Background()
	svc, srv, _ := testService(t)
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: nginx:latest\n"})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web", ServerID: srv.ID, TemplateID: tpl.ID})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	payload := `[{"id":"abc","name":"web-1","state":"running","status":"Up","labels":{"panel.managed":"true","panel.service_id":"` + deployed.ID + `","panel.template_id":"` + tpl.ID + `","panel.template_name":"web","panel.server_id":"` + srv.ID + `"}},{"id":"orphan","name":"old-1","state":"running","status":"Up","labels":{"panel.managed":"true","panel.service_id":"svc_missing","panel.template_id":"tmpl_missing","panel.template_name":"old","panel.server_id":"` + srv.ID + `"}},{"id":"raw","name":"redis","state":"running","status":"Up","labels":{}}]`
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO docker_runtime_cache(server_id,resource,payload,refreshed_at) VALUES(?,?,?,?)`, srv.ID, "services", payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert runtime cache: %v", err)
	}
	rows, err := svc.ListServices(ctx)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	var managed, orphan, unmanaged bool
	for _, row := range rows {
		if row.ID == deployed.ID && row.ManagementState == "managed" && row.RuntimeStatus == "running" {
			managed = true
		}
		if row.ManagementState == "orphaned" && row.TemplateName == "old" {
			orphan = true
		}
		if row.ManagementState == "unmanaged" && row.Name == "redis" {
			unmanaged = true
		}
	}
	if !managed || !orphan || !unmanaged {
		t.Fatalf("expected managed, orphaned, and unmanaged rows, got %#v", rows)
	}
}

func TestDeployCreatesAndAppliesTemplateDependenciesFirst(t *testing.T) {
	ctx := context.Background()
	svc, srv, taskSvc, exec := testServiceWithRecordingExec(t)
	db, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:        "db",
		ComposeYAML: "image: postgres:16\n",
	})
	if err != nil {
		t.Fatalf("create dependency template: %v", err)
	}
	api, err := svc.CreateTemplate(ctx, SaveTemplateRequest{
		Name:         "api",
		ComposeYAML:  "image: nginx:latest\n",
		Dependencies: []string{db.ID},
	})
	if err != nil {
		t.Fatalf("create api template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "api", ServerID: srv.ID, TemplateID: api.ID, RemotePath: "/opt/api"})
	if err != nil {
		t.Fatalf("create api service: %v", err)
	}

	task, err := svc.LifecycleTask(ctx, deployed.ID, "deploy")
	if err != nil {
		t.Fatalf("deploy task: %v", err)
	}
	waitForTask(t, taskSvc, task.ID)

	services, err := svc.ListServices(ctx)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %d, want 2 including auto-created dependency: %#v", len(services), services)
	}
	var dbService DeployedService
	for _, item := range services {
		if item.TemplateID == db.ID {
			dbService = item
		}
	}
	if dbService.ID == "" || dbService.ServerID != srv.ID || dbService.RemotePath != "/opt/panel/services/db" {
		t.Fatalf("dependency service not auto-created correctly: %#v", dbService)
	}
	joined := strings.Join(exec.commands, "\n")
	dbIndex := strings.Index(joined, "cd '/opt/panel/services/db'")
	apiIndex := strings.Index(joined, "cd '/opt/api'")
	if dbIndex < 0 || apiIndex < 0 || dbIndex > apiIndex {
		t.Fatalf("dependency must be deployed before api, got commands:\n%s", joined)
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
	if _, err := svc.PutServerVariables(ctx, srv.ID, map[string]any{"image": "nginx:1", "port": 8080}); err != nil {
		t.Fatalf("put server vars: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web"})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	rendered, err := svc.RenderService(ctx, deployed.ID)
	if err != nil {
		t.Fatalf("render service: %v", err)
	}
	if !strings.Contains(rendered.ComposeYAML, "image: nginx:1") || !strings.Contains(rendered.ComposeYAML, "panel.managed: \"true\"") || len(rendered.Files) != 2 {
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
	tpl, err := svc.CreateTemplate(ctx, SaveTemplateRequest{Name: "web", ComposeYAML: "image: {{ .image }}\n", Variables: []TemplateVariable{{Key: "image", Required: true, Default: "nginx:1"}}})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	deployed, err := svc.CreateService(ctx, SaveServiceRequest{Name: "web-prod", ServerID: srv.ID, TemplateID: tpl.ID, RemotePath: "/opt/web"})
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
