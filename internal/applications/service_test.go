package applications

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/nomad"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestCreateDisabledAppStoresRowAndDoesNotCallNomad(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID == "" || app.Name != "web" || app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	if app.Generation != 1 || app.JobID != "panel-web" || app.Namespace != "apps" {
		t.Fatalf("app persistence fields = %#v", app)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("nomad calls = %#v", fake.calls)
	}
}

func TestCreateEnabledAppValidatesPlansAndRegisters(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	fake.registerResponse = nomad.RegisterResponse{EvalID: "eval-1"}

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled || app.LastEvalID != "eval-1" {
		t.Fatalf("app = %#v", app)
	}
	want := []string{"validate:panel-web", "plan:panel-web", "register:panel-web"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestUpdateDisabledAppIncrementsGenerationOnlyWhenSpecHashChanges(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if same.Generation != 1 {
		t.Fatalf("same generation = %d", same.Generation)
	}
	changed, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx:1.27\n"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Generation != 2 {
		t.Fatalf("changed generation = %d", changed.Generation)
	}
}

func TestStopAppCallsNomadAndDisablesApp(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Stop(ctx, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Enabled {
		t.Fatalf("app should be disabled: %#v", stopped)
	}
	if got := fake.calls[len(fake.calls)-1]; got != "stop:panel-web:false" {
		t.Fatalf("last call = %q", got)
	}
}

func TestRuntimeMapsNomadState(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	fake.deployment = nomad.Deployment{ID: "dep-1", JobID: "panel-web", Status: "running"}
	fake.allocations = []nomad.AllocationListItem{{ID: "alloc-1", JobID: "panel-web", ClientStatus: "running"}}
	fake.evaluations = []nomad.Evaluation{{ID: "eval-1", JobID: "panel-web", Status: "complete"}}

	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.JobStatus != "running" || runtime.Deployment == nil || runtime.Deployment.ID != "dep-1" {
		t.Fatalf("runtime = %#v", runtime)
	}
	if len(runtime.Allocations) != 1 || len(runtime.Evaluations) != 1 {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func newTestService(t *testing.T) (*Service, *fakeNomadClient, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.Nomad.Namespace = "apps"
	cfg.Nomad.Region = "global"
	cfg.Nomad.Datacenter = "dc1"
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNomadClient{}
	svc := NewService(store.AppDB(), fake, tasks.NewService(store.AppDB()), Config{
		Namespace:  cfg.Nomad.Namespace,
		Region:     cfg.Nomad.Region,
		Datacenter: cfg.Nomad.Datacenter,
	})
	return svc, fake, func() { _ = store.Close() }
}

type fakeNomadClient struct {
	calls            []string
	registerResponse nomad.RegisterResponse
	deployment       nomad.Deployment
	allocations      []nomad.AllocationListItem
	evaluations      []nomad.Evaluation
}

func (f *fakeNomadClient) ValidateJob(ctx context.Context, job nomad.Job) (nomad.ValidateResponse, error) {
	f.calls = append(f.calls, "validate:"+job.ID)
	return nomad.ValidateResponse{DriverConfigValidated: true}, nil
}

func (f *fakeNomadClient) PlanJob(ctx context.Context, id string, job nomad.Job) (nomad.PlanResponse, error) {
	f.calls = append(f.calls, "plan:"+id)
	return nomad.PlanResponse{}, nil
}

func (f *fakeNomadClient) RegisterJob(ctx context.Context, id string, job nomad.Job) (nomad.RegisterResponse, error) {
	f.calls = append(f.calls, "register:"+id)
	return f.registerResponse, nil
}

func (f *fakeNomadClient) StopJob(ctx context.Context, id string, purge bool) (nomad.StopResponse, error) {
	f.calls = append(f.calls, "stop:"+id+":"+boolString(purge))
	return nomad.StopResponse{}, nil
}

func (f *fakeNomadClient) JobAllocations(ctx context.Context, id string) ([]nomad.AllocationListItem, error) {
	return f.allocations, nil
}

func (f *fakeNomadClient) JobDeployment(ctx context.Context, id string) (nomad.Deployment, error) {
	return f.deployment, nil
}

func (f *fakeNomadClient) JobEvaluations(ctx context.Context, id string) ([]nomad.Evaluation, error) {
	return f.evaluations, nil
}

func (f *fakeNomadClient) RestartAllocation(ctx context.Context, allocID, task string) error {
	f.calls = append(f.calls, "restart:"+allocID+":"+task)
	return nil
}

func (f *fakeNomadClient) AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error) {
	f.calls = append(f.calls, "logs:"+allocID+":"+task+":"+logType)
	return "hello", nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
