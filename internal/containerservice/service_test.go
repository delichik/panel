package containerservice

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func newTestService(t *testing.T) (*Service, *tasks.Service) {
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
	t.Cleanup(func() { _ = store.Close() })
	taskSvc := tasks.NewService(store.AppDB())
	return NewService(store.AppDB(), taskSvc), taskSvc
}

func TestValidateNameUsesContainerServiceSubset(t *testing.T) {
	valid := []string{"a", "app-1", "0-api", "mysql8"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("expected %q to be valid: %v", name, err)
		}
	}
	invalid := []string{"", "-app", "app-", "App", "app_name", "app.name", "a-very-long-service-name-over-32-chars"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Fatalf("expected %q to be invalid", name)
		}
	}
}

func TestParseServiceBodyValidationAndDependencies(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "full compose is forbidden", body: "services:\n  api:\n    image: nginx\n", wantErr: true},
		{name: "container_name is forbidden", body: "image: nginx\ncontainer_name: api\n", wantErr: true},
		{name: "panel labels are reserved", body: "image: nginx\nlabels:\n  panel.managed: 'true'\n", wantErr: true},
		{name: "host mode must declare claims", body: "image: nginx\nnetwork_mode: host\n", wantErr: true},
		{name: "host mode allows empty claims", body: "image: nginx\nnetwork_mode: host\nlabels:\n  panel.claims.ports: ''\n"},
		{name: "non-host mode forbids explicit claims", body: "image: nginx\nlabels:\n  panel.claims.ports: '80'\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseServiceBody(tt.body)
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	body, err := ParseServiceBody("image: nginx\ndepends_on:\n  mysql:\n    condition: service_started\n  redis:\n    condition: service_healthy\nports:\n  - '8080:80'\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := body.Dependencies; len(got) != 2 || got[0] != "mysql" || got[1] != "redis" {
		t.Fatalf("unexpected dependencies: %#v", got)
	}
	if got := body.PortClaims; len(got) != 1 || got[0] != 8080 {
		t.Fatalf("unexpected port claims: %#v", got)
	}
}

func TestDependencyValidationDetectsMissingSelfAndCycle(t *testing.T) {
	graph := map[string][]string{
		"api":   {"mysql"},
		"mysql": {"redis"},
		"redis": {},
	}
	if err := ValidateDependencyGraph("api", graph); err != nil {
		t.Fatalf("expected valid graph: %v", err)
	}
	graph["api"] = []string{"api"}
	if err := ValidateDependencyGraph("api", graph); err == nil {
		t.Fatal("expected self dependency error")
	}
	graph["api"] = []string{"mysql"}
	graph["redis"] = []string{"api"}
	if err := ValidateDependencyGraph("api", graph); err == nil {
		t.Fatal("expected cycle error")
	}
	graph["redis"] = []string{"missing"}
	if err := ValidateDependencyGraph("api", graph); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func TestSaveGenerationAndEnabledReconcileBehavior(t *testing.T) {
	svc, taskSvc := newTestService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, SaveRequest{
		Name:               "api",
		Enabled:            false,
		ComposeServiceYAML: "image: nginx\n",
		Variables:          map[string]string{"A": "1"},
		Selector:           map[string]string{"role": "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Generation != 1 {
		t.Fatalf("expected generation 1, got %d", created.Generation)
	}

	selectorOnly, err := svc.Update(ctx, created.ID, SaveRequest{
		Name:               "ignored-new-name",
		Enabled:            false,
		ComposeServiceYAML: "image: nginx\n",
		Variables:          map[string]string{"A": "1"},
		Selector:           map[string]string{"role": "edge"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selectorOnly.Name != "api" || selectorOnly.Generation != 1 {
		t.Fatalf("selector-only update must keep immutable name and generation: %#v", selectorOnly)
	}

	enabled, err := svc.Update(ctx, created.ID, SaveRequest{
		Enabled:            true,
		ComposeServiceYAML: "image: nginx:alpine\n",
		Variables:          map[string]string{"A": "1"},
		Selector:           map[string]string{"role": "edge"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if enabled.Generation != 2 || enabled.LastTaskID == "" {
		t.Fatalf("enabled spec update should increment and enqueue reconcile: %#v", enabled)
	}
	task, err := taskSvc.Get(ctx, enabled.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.OperationID == "" || task.Type != TaskTypeReconcile || task.TriggerType != TriggerUser {
		t.Fatalf("unexpected reconcile task metadata: %#v", task)
	}
}

func TestEnableDisablePreviewAndDeleteConstraints(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	mysql, err := svc.Create(ctx, SaveRequest{Name: "mysql", ComposeServiceYAML: "image: mysql:8\n"})
	if err != nil {
		t.Fatal(err)
	}
	api, err := svc.Create(ctx, SaveRequest{Name: "api", ComposeServiceYAML: "image: nginx\ndepends_on:\n  - mysql\n"})
	if err != nil {
		t.Fatal(err)
	}
	enable, err := svc.EnablePreview(ctx, api.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(enable.Services) != 2 || enable.Services[0].ID != mysql.ID || enable.Services[1].ID != api.ID {
		t.Fatalf("enable order must be dependency first: %#v", enable.Services)
	}
	disable, err := svc.DisablePreview(ctx, mysql.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(disable.Services) != 2 || disable.Services[0].ID != api.ID || disable.Services[1].ID != mysql.ID {
		t.Fatalf("disable order must be dependent first: %#v", disable.Services)
	}
	if _, err := svc.Delete(ctx, mysql.ID); err == nil {
		t.Fatal("delete should be blocked while dependents exist")
	}
}

func TestRenderSchedulePreviewAndFileCRUD(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	addDockerNode(t, svc, "node-1", "Node 1", map[string]string{"role": "web"})
	created, err := svc.Create(ctx, SaveRequest{
		Name:               "api",
		ComposeServiceYAML: "image: nginx\nvolumes:\n  - \"{{ .service.current_dir }}/files/nginx.conf:/etc/nginx/nginx.conf:ro\"\n",
		Variables:          map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := svc.CreateFile(ctx, created.ID, FileInput{Path: "nginx.conf", Kind: "template", Content: "server_name {{ .service.name }};"})
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != "nginx.conf" || file.Size == 0 || file.SHA256 == "" {
		t.Fatalf("unexpected created file: %#v", file)
	}
	updated, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != created.Generation+1 {
		t.Fatalf("file changes must increment generation, got %d from %d", updated.Generation, created.Generation)
	}
	if updated.SpecHash == created.SpecHash || updated.SpecRevision == created.SpecRevision {
		t.Fatal("file changes must recompute spec hash and revision")
	}
	files, err := svc.ListFiles(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Content != "server_name {{ .service.name }};" {
		t.Fatalf("unexpected files: %#v", files)
	}
	rendered, err := svc.RenderPreview(ctx, created.ID, SaveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.ComposeYAML, "services:") || !strings.Contains(rendered.OverrideYAML, "panel.service.name") || !strings.Contains(rendered.ManifestJSON, "currentDir") {
		t.Fatalf("render preview did not include expected artifacts: %#v", rendered)
	}
	schedule, err := svc.SchedulePreview(ctx, created.ID, SaveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(schedule.Candidates) == 0 {
		t.Fatal("expected at least one schedule preview candidate")
	}
	if err := svc.DeleteFile(ctx, created.ID, file.ID); err != nil {
		t.Fatal(err)
	}
	files, err = svc.ListFiles(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected file deletion, got %#v", files)
	}
}

func TestEnabledFileChangeRecomputesSpecAndQueuesReconcileOnlyWhenChanged(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, SaveRequest{Name: "api", Enabled: true, ComposeServiceYAML: "image: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	initialTask := created.LastTaskID
	file, err := svc.CreateFile(ctx, created.ID, FileInput{Path: "nginx.conf", Kind: "template", Content: "one"})
	if err != nil {
		t.Fatal(err)
	}
	afterCreate, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterCreate.Generation != created.Generation+1 || afterCreate.LastTaskID == "" || afterCreate.LastTaskID == initialTask {
		t.Fatalf("enabled file create should bump generation and queue reconcile: %#v", afterCreate)
	}
	queuedAfterCreate := afterCreate.LastTaskID
	if _, err := svc.UpdateFile(ctx, created.ID, file.ID, FileInput{Path: "nginx.conf", Kind: "template", Content: "one"}); err != nil {
		t.Fatal(err)
	}
	afterNoop, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterNoop.Generation != afterCreate.Generation || afterNoop.LastTaskID != queuedAfterCreate {
		t.Fatalf("same file content should not create a new generation/task: %#v", afterNoop)
	}
}

func TestRuntimeStatusUsesDockerCacheLabels(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, SaveRequest{Name: "api", ComposeServiceYAML: "image: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	addDockerNode(t, svc, "node-1", "Node 1", nil)
	now := ts(time.Now().UTC())
	payload, err := json.Marshal([]map[string]any{{
		"id":      "container-1",
		"name":    "api",
		"image":   "nginx",
		"state":   "running",
		"status":  "Up 3 seconds",
		"managed": true,
		"labels": map[string]string{
			"panel.managed":               "true",
			"panel.service.id":            created.ID,
			"panel.service.name":          "api",
			"panel.service.generation":    "1",
			"panel.service.spec_revision": created.SpecRevision,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO docker_runtime_cache(server_id,resource,payload,refreshed_at) VALUES(?,?,?,?)`, "node-1", "services", string(payload), now); err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.Runtime(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Status != "running" || runtime.ServiceName != "api" || runtime.NodeID != "node-1" || runtime.NodeName != "Node 1" {
		t.Fatalf("unexpected runtime status: %#v", runtime)
	}
	if runtime.ObservedGeneration == nil || *runtime.ObservedGeneration != 1 || runtime.ObservedSpecRevision != created.SpecRevision {
		t.Fatalf("runtime labels were not parsed: %#v", runtime)
	}
	if runtime.ContainerID != "container-1" || !runtime.Managed {
		t.Fatalf("runtime ownership not derived: %#v", runtime)
	}
}

func addDockerNode(t *testing.T, svc *Service, id, name string, traits map[string]string) {
	t.Helper()
	traitsJSON := "{}"
	if len(traits) > 0 {
		data, err := json.Marshal(traits)
		if err != nil {
			t.Fatal(err)
		}
		traitsJSON = string(data)
	}
	now := ts(time.Now().UTC())
	_, err := svc.db.Exec(`INSERT INTO credentials(id,name,type,username,password_secret,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id+"-cred", name+" credential", "password", "root", "secret", now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.db.Exec(`INSERT INTO servers(id,name,host,port,credential_id,traits,os_supported,reachable,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, name, "127.0.0.1", 22, id+"-cred", traitsJSON, 1, 1, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.db.Exec(`INSERT INTO docker_capabilities(server_id,docker_installed,docker_version,compose_installed,compose_version,include_supported,supported,last_checked_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, 1, "25.0.0", 1, "2.27.0", 1, 1, now)
	if err != nil {
		t.Fatal(err)
	}
}
