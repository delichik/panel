package containerservice

import (
	"context"
	"path/filepath"
	"testing"

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
	if err := svc.Delete(ctx, mysql.ID); err == nil {
		t.Fatal("delete should be blocked while dependents exist")
	}
}
