# Nomad Control Plane Implementation Plan

**Goal:** Replace the current self-built Docker Compose orchestration design with a Nomad-backed application control plane.

**Architecture:** Panel remains a Go modular monolith with a Vue frontend, but Nomad becomes the only scheduler and runtime control plane. Panel stores product-level desired state, renders Nomad JSON jobs, calls the Nomad HTTP API, and reads runtime state from jobs, deployments, evaluations, nodes, allocations, services, and logs.

**Tech Stack:** Go standard library HTTP client, SQLite, Vue 3, Vite, Vuetify, Pinia, Nomad HTTP API v2.0.x from `docs/nomad-api-docs`.

---

## Multi-Person Task Split

This plan has been split into task files under:

- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/00-coordination.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/01-nomad-config-client.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/02-appspec-renderer.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/03-application-persistence.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/04-application-service-api.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/05-nomad-runtime-inventory.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/06-frontend-applications.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/07-frontend-nomad-views.md`
- `docs/superpowers/plans/2026-05-24-nomad-control-plane-tasks/08-removal-verification-docs.md`

Use `00-coordination.md` as the collaboration index. Each task file includes ownership, dependencies, related files, Nomad official docs, API/interface contracts, detailed steps, verification, and handoff notes.

## Local Official Docs

Use these local official Nomad API docs while implementing:

- `docs/nomad-api-docs/jobs.mdx`: `/v1/jobs`, `/v1/job/:job_id`, `/v1/job/:job_id/plan`, `/v1/job/:job_id/allocations`, `/v1/job/:job_id/deployment`, `/v1/job/:job_id/evaluations`, `/v1/job/:job_id/summary`, `/v1/job/:job_id/scale`, `/v1/job/:job_id/services`, `DELETE /v1/job/:job_id`.
- `docs/nomad-api-docs/json-jobs.mdx`: Nomad JSON job, task group, task, resources, restart, update, constraint, service, check, template, volume syntax.
- `docs/nomad-api-docs/validate.mdx`: `POST /v1/validate/job`.
- `docs/nomad-api-docs/allocations.mdx`: `/v1/allocations`, `/v1/allocation/:alloc_id`, `/v1/client/allocation/:alloc_id/restart`, allocation services and checks.
- `docs/nomad-api-docs/deployments.mdx`: `/v1/deployments`, `/v1/deployment/:deployment_id`, deployment allocations and deployment controls.
- `docs/nomad-api-docs/evaluations.mdx`: `/v1/evaluations`, `/v1/evaluation/:eval_id`, evaluation allocations.
- `docs/nomad-api-docs/nodes.mdx`: `/v1/nodes`, `/v1/node/:node_id`, node allocations, drain, eligibility.
- `docs/nomad-api-docs/services.mdx`: `/v1/services`, `/v1/service/:service_name`.
- `docs/nomad-api-docs/README.md`: upstream source and commit for the local documentation copy.

## File Structure

Create:

- `internal/nomad/config.go`: Nomad address, token, namespace, region, datacenter settings derived from `config.Config` and runtime settings.
- `internal/nomad/client.go`: low-level HTTP client with Nomad headers, query parameters, JSON encoding, and error decoding.
- `internal/nomad/types.go`: minimal request and response DTOs used by panel.
- `internal/nomad/client_test.go`: low-level client tests with `httptest.Server`.
- `internal/appspec/model.go`: panel-owned application spec DTOs and validation types.
- `internal/appspec/validate.go`: deterministic validation for names, images, ports, resources, constraints, services, checks, volumes, and files.
- `internal/appspec/render.go`: application spec to Nomad JSON job renderer.
- `internal/appspec/render_test.go`: renderer tests.
- `internal/applications/model.go`: persisted Application, revision, runtime, and operation DTOs.
- `internal/applications/service.go`: Application CRUD, validation, plan, deploy, stop, restart, runtime, and logs orchestration.
- `internal/applications/handler.go`: `/api/v1/applications` HTTP handler.
- `internal/applications/service_test.go`: service tests using fake Nomad client.
- `internal/applications/handler_test.go`: API routing tests.
- `web/src/api/applications.ts`: frontend API client.
- `web/src/features/applications/pages/ApplicationsPage.vue`: primary application workspace.
- `web/src/features/applications/components/ApplicationEditor.vue`: spec editor, validation, plan, deploy controls.
- `web/src/features/applications/components/ApplicationDetail.vue`: runtime, deployments, allocations, events, logs.
- `web/src/features/nomad/pages/NomadNodesPage.vue`: Nomad nodes and allocation view.
- `web/src/features/deployments/pages/DeploymentsPage.vue`: deployment/evaluation timeline.
- `web/src/features/applications/*.test.ts`: focused frontend tests.

Modify:

- `config.example.json`: add Nomad settings.
- `internal/config/config.go`: parse and expose Nomad settings.
- `internal/storage/migrations.go`: add application tables and remove old container orchestration tables from fresh schema.
- `internal/app/app.go`: wire Nomad and Applications services, remove Docker Compose orchestration service wiring.
- `internal/scheduler/scheduler.go`: remove container reconcile worker and add Nomad cache refresh jobs.
- `web/src/router/index.ts`: replace Container Services and Runtime Explorer routes with Applications, Nomad Nodes, and Deployments routes.
- `web/src/layouts/AppLayout.vue`: update navigation labels.
- `web/src/types/api.ts`: replace container-service DTOs with application and Nomad DTOs.

Delete:

- `internal/containerops/`
- `internal/containerrender/`
- `internal/containerservice/`
- `internal/placement/`
- Docker Compose mutation paths in `internal/docker/` that exist only for the old orchestration design.
- `web/src/features/container-services/`
- old container service API client and tests in `web/src/api/containerServices.ts` and `web/src/api/containerServices.test.ts`.

Preserve:

- `internal/tasks/`: keep task history and logs, but task execution becomes thin wrappers around Nomad API calls.
- `internal/server/`, `internal/sshx/`, `internal/packages/`, `internal/metrics/`: keep because they support existing Linux server management outside the container orchestration surface.

Delete:

- `internal/docker/`: remove the Docker runtime explorer and Compose mutation backend from the active product. Nomad allocations become the runtime explorer surface.

---

### Task 1: Store Nomad Configuration

**Files:**
- Modify: `config.example.json`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing config tests**

Add tests that cover explicit Nomad config and defaults:

```go
func TestLoadNomadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
		"listenAddress": "127.0.0.1:8080",
		"adminUsername": "admin",
		"sessionSecret": "secret",
		"dataRoot": "data",
		"appDatabase": "data/db/app.db",
		"metricsDatabase": "data/db/metrics.db",
		"nomad": {
			"address": "https://nomad.service:4646",
			"token": "root-token",
			"namespace": "apps",
			"region": "global",
			"datacenter": "dc1"
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Nomad.Address != "https://nomad.service:4646" {
		t.Fatalf("address = %q", cfg.Nomad.Address)
	}
	if cfg.Nomad.Token != "root-token" {
		t.Fatalf("token = %q", cfg.Nomad.Token)
	}
	if cfg.Nomad.Namespace != "apps" {
		t.Fatalf("namespace = %q", cfg.Nomad.Namespace)
	}
	if cfg.Nomad.Region != "global" {
		t.Fatalf("region = %q", cfg.Nomad.Region)
	}
	if cfg.Nomad.Datacenter != "dc1" {
		t.Fatalf("datacenter = %q", cfg.Nomad.Datacenter)
	}
}

func TestNomadConfigDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Nomad.Address != "http://127.0.0.1:4646" {
		t.Fatalf("address = %q", cfg.Nomad.Address)
	}
	if cfg.Nomad.Namespace != "default" {
		t.Fatalf("namespace = %q", cfg.Nomad.Namespace)
	}
	if cfg.Nomad.Region != "global" {
		t.Fatalf("region = %q", cfg.Nomad.Region)
	}
	if cfg.Nomad.Datacenter != "dc1" {
		t.Fatalf("datacenter = %q", cfg.Nomad.Datacenter)
	}
}
```

- [ ] **Step 2: Run config tests and verify failure**

Run: `go test ./internal/config`

Expected: FAIL with missing `Nomad` field or missing default assertions.

- [ ] **Step 3: Add Nomad config types and defaults**

Add this shape to `internal/config/config.go`:

```go
type NomadConfig struct {
	Address    string `json:"address"`
	Token      string `json:"token"`
	Namespace  string `json:"namespace"`
	Region     string `json:"region"`
	Datacenter string `json:"datacenter"`
}

type Config struct {
	ListenAddress                    string      `json:"listenAddress"`
	AdminUsername                    string      `json:"adminUsername"`
	AdminPasswordHash                string      `json:"adminPasswordHash"`
	SessionSecret                    string      `json:"sessionSecret"`
	DataRoot                         string      `json:"dataRoot"`
	AppDatabase                      string      `json:"appDatabase"`
	MetricsDatabase                  string      `json:"metricsDatabase"`
	MetricsRetentionDays             int         `json:"metricsRetentionDays"`
	MetricsCollectionIntervalSeconds int         `json:"metricsCollectionIntervalSeconds"`
	RemoteCommandTimeoutSeconds      int         `json:"remoteCommandTimeoutSeconds"`
	Nomad                            NomadConfig `json:"nomad"`
}
```

Ensure defaults include:

```go
Nomad: NomadConfig{
	Address:    "http://127.0.0.1:4646",
	Namespace:  "default",
	Region:     "global",
	Datacenter: "dc1",
},
```

Normalize empty loaded values back to those defaults after JSON decoding.

- [ ] **Step 4: Update config example**

Add this block to `config.example.json`:

```json
  "nomad": {
    "address": "http://127.0.0.1:4646",
    "token": "",
    "namespace": "default",
    "region": "global",
    "datacenter": "dc1"
  }
```

- [ ] **Step 5: Run config tests and commit**

Run: `go test ./internal/config`

Expected: PASS.

Commit:

```bash
git add config.example.json internal/config/config.go internal/config/config_test.go
git commit -m "feat: add nomad configuration"
```

### Task 2: Build Nomad HTTP Client

**Files:**
- Create: `internal/nomad/config.go`
- Create: `internal/nomad/client.go`
- Create: `internal/nomad/types.go`
- Create: `internal/nomad/client_test.go`

- [ ] **Step 1: Write failing client tests**

Create `internal/nomad/client_test.go`:

```go
package nomad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAddsHeadersAndNamespace(t *testing.T) {
	var gotPath, gotToken, gotNamespace string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Nomad-Token")
		gotNamespace = r.URL.Query().Get("namespace")
		_ = json.NewEncoder(w).Encode([]JobListItem{{ID: "web"}})
	}))
	defer srv.Close()

	client := NewClient(Config{Address: srv.URL, Token: "secret", Namespace: "apps"}, srv.Client())
	jobs, err := client.ListJobs(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/jobs" || gotToken != "secret" || gotNamespace != "apps" {
		t.Fatalf("path=%q token=%q namespace=%q", gotPath, gotToken, gotNamespace)
	}
	if len(jobs) != 1 || jobs[0].ID != "web" {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestClientDecodesNomadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "job not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(Config{Address: srv.URL, Namespace: "default"}, srv.Client())
	_, err := client.ReadJob(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "nomad GET /v1/job/missing failed: 404 job not found" {
		t.Fatalf("error = %q", err.Error())
	}
}
```

- [ ] **Step 2: Run Nomad client tests and verify failure**

Run: `go test ./internal/nomad`

Expected: FAIL because package and types do not exist.

- [ ] **Step 3: Implement minimal client config and types**

Create `internal/nomad/config.go`:

```go
package nomad

type Config struct {
	Address    string
	Token      string
	Namespace  string
	Region     string
	Datacenter string
}
```

Create `internal/nomad/types.go` with the DTOs used by the first slice:

```go
package nomad

type JobListItem struct {
	ID          string `json:"ID"`
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Status      string `json:"Status"`
	Namespace   string `json:"Namespace"`
	Datacenters []string `json:"Datacenters"`
	SubmitTime  int64  `json:"SubmitTime"`
}

type Job struct {
	ID          string            `json:"ID"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	Namespace   string            `json:"Namespace,omitempty"`
	Region      string            `json:"Region,omitempty"`
	Datacenters []string          `json:"Datacenters"`
	Meta        map[string]string `json:"Meta,omitempty"`
	TaskGroups  []TaskGroup       `json:"TaskGroups"`
}

type TaskGroup struct {
	Name     string            `json:"Name"`
	Count    int               `json:"Count"`
	Networks []NetworkResource `json:"Networks,omitempty"`
	Services []Service         `json:"Services,omitempty"`
	Tasks    []Task            `json:"Tasks"`
}

type Task struct {
	Name      string            `json:"Name"`
	Driver    string            `json:"Driver"`
	Config    map[string]any     `json:"Config"`
	Env       map[string]string  `json:"Env,omitempty"`
	Resources *Resources        `json:"Resources,omitempty"`
	Services  []Service         `json:"Services,omitempty"`
	Meta      map[string]string `json:"Meta,omitempty"`
}

type Resources struct {
	CPU      int `json:"CPU"`
	MemoryMB int `json:"MemoryMB"`
}

type NetworkResource struct {
	Mode         string        `json:"Mode,omitempty"`
	DynamicPorts []PortMapping `json:"DynamicPorts,omitempty"`
	ReservedPorts []PortMapping `json:"ReservedPorts,omitempty"`
}

type PortMapping struct {
	Label string `json:"Label"`
	Value int   `json:"Value,omitempty"`
	To    int   `json:"To,omitempty"`
}

type Service struct {
	Name      string            `json:"Name"`
	PortLabel string           `json:"PortLabel,omitempty"`
	Tags      []string          `json:"Tags,omitempty"`
	Meta      map[string]string `json:"Meta,omitempty"`
	Checks    []Check           `json:"Checks,omitempty"`
}

type Check struct {
	Name     string `json:"Name"`
	Type     string `json:"Type"`
	PortLabel string `json:"PortLabel,omitempty"`
	Path     string `json:"Path,omitempty"`
	Interval int64  `json:"Interval"`
	Timeout  int64  `json:"Timeout"`
}
```

- [ ] **Step 4: Implement HTTP methods**

Create `internal/nomad/client.go` with:

```go
package nomad

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	cfg.Address = strings.TrimRight(cfg.Address, "/")
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	return &Client{cfg: cfg, http: httpClient}
}

func (c *Client) ListJobs(ctx context.Context, prefix string) ([]JobListItem, error) {
	var out []JobListItem
	q := url.Values{}
	if prefix != "" {
		q.Set("prefix", prefix)
	}
	return out, c.do(ctx, http.MethodGet, "/v1/jobs", q, nil, &out)
}

func (c *Client) ReadJob(ctx context.Context, id string) (Job, error) {
	var out Job
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id), nil, nil, &out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if query == nil {
		query = url.Values{}
	}
	if c.cfg.Namespace != "" {
		query.Set("namespace", c.cfg.Namespace)
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	u := c.cfg.Address + path
	if encoded := query.Encode(); encoded != "" {
		u += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("X-Nomad-Token", c.cfg.Token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("nomad %s %s failed: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	return json.Unmarshal(payload, out)
}
```

- [ ] **Step 5: Run Nomad client tests and commit**

Run: `go test ./internal/nomad`

Expected: PASS.

Commit:

```bash
git add internal/nomad
git commit -m "feat: add nomad api client"
```

### Task 3: Add Application Spec Validation and Nomad Job Rendering

**Files:**
- Create: `internal/appspec/model.go`
- Create: `internal/appspec/validate.go`
- Create: `internal/appspec/render.go`
- Create: `internal/appspec/render_test.go`

- [ ] **Step 1: Write renderer tests**

Create `internal/appspec/render_test.go`:

```go
package appspec

import "testing"

func TestRenderDockerServiceJob(t *testing.T) {
	spec := Spec{
		Name: "web",
		Image: "nginx:1.27",
		Count: 2,
		Env: map[string]string{"MODE": "prod"},
		Ports: []Port{{Label: "http", To: 80, Static: 8080}},
		Resources: Resources{CPU: 200, MemoryMB: 128},
		Checks: []Check{{Name: "http", Type: "http", Port: "http", Path: "/", IntervalSeconds: 10, TimeoutSeconds: 2}},
	}
	job, err := Render(RenderInput{
		AppID: "app_1",
		Generation: 3,
		SpecHash: "hash",
		Namespace: "apps",
		Region: "global",
		Datacenter: "dc1",
		Spec: spec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "panel-web" || job.Namespace != "apps" || job.Region != "global" {
		t.Fatalf("job identity = %#v", job)
	}
	if len(job.TaskGroups) != 1 || job.TaskGroups[0].Count != 2 {
		t.Fatalf("groups = %#v", job.TaskGroups)
	}
	task := job.TaskGroups[0].Tasks[0]
	if task.Driver != "docker" || task.Config["image"] != "nginx:1.27" {
		t.Fatalf("task = %#v", task)
	}
	if task.Env["MODE"] != "prod" {
		t.Fatalf("env = %#v", task.Env)
	}
	if job.Meta["panel.app.id"] != "app_1" || job.Meta["panel.generation"] != "3" {
		t.Fatalf("meta = %#v", job.Meta)
	}
}

func TestValidateRejectsInvalidNameAndPort(t *testing.T) {
	issues := Validate(Spec{Name: "Bad_Name", Image: "", Ports: []Port{{Label: "http", Static: -1}}})
	if len(issues) != 3 {
		t.Fatalf("issues = %#v", issues)
	}
}
```

- [ ] **Step 2: Run renderer tests and verify failure**

Run: `go test ./internal/appspec`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Define spec model**

Create `internal/appspec/model.go`:

```go
package appspec

type Spec struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Count       int               `json:"count"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Ports       []Port            `json:"ports,omitempty"`
	Resources   Resources         `json:"resources"`
	Constraints []Constraint      `json:"constraints,omitempty"`
	Services    []Service         `json:"services,omitempty"`
	Checks      []Check           `json:"checks,omitempty"`
	Volumes     []Volume          `json:"volumes,omitempty"`
}

type Port struct {
	Label  string `json:"label"`
	To     int    `json:"to"`
	Static int    `json:"static,omitempty"`
}

type Resources struct {
	CPU      int `json:"cpu"`
	MemoryMB int `json:"memoryMb"`
}

type Constraint struct {
	Attribute string `json:"attribute"`
	Operator  string `json:"operator"`
	Value     string `json:"value"`
}

type Service struct {
	Name string   `json:"name"`
	Port string   `json:"port"`
	Tags []string `json:"tags,omitempty"`
}

type Check struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Port            string `json:"port"`
	Path            string `json:"path,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds"`
	TimeoutSeconds  int    `json:"timeoutSeconds"`
}

type Volume struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly"`
}

type Issue struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}
```

- [ ] **Step 4: Implement validation**

Create `internal/appspec/validate.go`:

```go
package appspec

import "regexp"

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func Validate(spec Spec) []Issue {
	issues := []Issue{}
	if !namePattern.MatchString(spec.Name) {
		issues = append(issues, Issue{Path: "name", Message: "name must use lowercase letters, digits, and dashes, start with a letter or digit, and be at most 32 characters", Severity: "error"})
	}
	if spec.Image == "" {
		issues = append(issues, Issue{Path: "image", Message: "image is required", Severity: "error"})
	}
	if spec.Count < 0 {
		issues = append(issues, Issue{Path: "count", Message: "count must be zero or greater", Severity: "error"})
	}
	for i, port := range spec.Ports {
		if !namePattern.MatchString(port.Label) {
			issues = append(issues, Issue{Path: pathf("ports", i, "label"), Message: "port label must use the application name format", Severity: "error"})
		}
		if port.To <= 0 || port.To > 65535 || port.Static < 0 || port.Static > 65535 {
			issues = append(issues, Issue{Path: pathf("ports", i, "static"), Message: "port values must be between 1 and 65535", Severity: "error"})
		}
	}
	if spec.Resources.CPU < 0 || spec.Resources.MemoryMB < 0 {
		issues = append(issues, Issue{Path: "resources", Message: "cpu and memory must be zero or greater", Severity: "error"})
	}
	return issues
}

func pathf(prefix string, index int, field string) string {
	return prefix + "[" + strconv.Itoa(index) + "]." + field
}
```

Add the missing import:

```go
import (
	"regexp"
	"strconv"
)
```

- [ ] **Step 5: Implement renderer**

Create `internal/appspec/render.go`:

```go
package appspec

import (
	"fmt"
	"strconv"

	"panel/internal/nomad"
)

type RenderInput struct {
	AppID      string
	Generation int
	SpecHash   string
	Namespace  string
	Region     string
	Datacenter string
	Spec       Spec
}

func Render(in RenderInput) (nomad.Job, error) {
	if issues := Validate(in.Spec); len(issues) > 0 {
		return nomad.Job{}, fmt.Errorf(issues[0].Message)
	}
	count := in.Spec.Count
	if count == 0 {
		count = 1
	}
	taskConfig := map[string]any{"image": in.Spec.Image}
	if len(in.Spec.Command) > 0 {
		taskConfig["command"] = in.Spec.Command[0]
	}
	if len(in.Spec.Args) > 0 {
		taskConfig["args"] = in.Spec.Args
	}
	group := nomad.TaskGroup{
		Name:  in.Spec.Name,
		Count: count,
		Networks: []nomad.NetworkResource{renderNetwork(in.Spec.Ports)},
		Tasks: []nomad.Task{{
			Name: in.Spec.Name,
			Driver: "docker",
			Config: taskConfig,
			Env: in.Spec.Env,
			Resources: &nomad.Resources{CPU: in.Spec.Resources.CPU, MemoryMB: in.Spec.Resources.MemoryMB},
			Services: renderServices(in.Spec),
			Meta: map[string]string{
				"panel.app.id": in.AppID,
				"panel.generation": strconv.Itoa(in.Generation),
				"panel.spec_hash": in.SpecHash,
			},
		}},
	}
	return nomad.Job{
		ID: "panel-" + in.Spec.Name,
		Name: in.Spec.Name,
		Type: "service",
		Namespace: in.Namespace,
		Region: in.Region,
		Datacenters: []string{in.Datacenter},
		Meta: map[string]string{
			"panel.app.id": in.AppID,
			"panel.app.name": in.Spec.Name,
			"panel.generation": strconv.Itoa(in.Generation),
			"panel.spec_hash": in.SpecHash,
		},
		TaskGroups: []nomad.TaskGroup{group},
	}, nil
}
```

Add helper functions in the same file:

```go
func renderNetwork(ports []Port) nomad.NetworkResource {
	out := nomad.NetworkResource{Mode: "bridge"}
	for _, port := range ports {
		mapping := nomad.PortMapping{Label: port.Label, To: port.To}
		if port.Static > 0 {
			mapping.Value = port.Static
			out.ReservedPorts = append(out.ReservedPorts, mapping)
		} else {
			out.DynamicPorts = append(out.DynamicPorts, mapping)
		}
	}
	return out
}

func renderServices(spec Spec) []nomad.Service {
	services := []nomad.Service{}
	if len(spec.Services) == 0 && len(spec.Ports) > 0 {
		services = append(services, nomad.Service{Name: spec.Name, PortLabel: spec.Ports[0].Label, Checks: renderChecks(spec.Checks)})
		return services
	}
	for _, svc := range spec.Services {
		services = append(services, nomad.Service{Name: svc.Name, PortLabel: svc.Port, Tags: svc.Tags, Checks: renderChecks(spec.Checks)})
	}
	return services
}

func renderChecks(checks []Check) []nomad.Check {
	out := make([]nomad.Check, 0, len(checks))
	for _, check := range checks {
		out = append(out, nomad.Check{
			Name: check.Name,
			Type: check.Type,
			PortLabel: check.Port,
			Path: check.Path,
			Interval: int64(check.IntervalSeconds) * 1_000_000_000,
			Timeout: int64(check.TimeoutSeconds) * 1_000_000_000,
		})
	}
	return out
}
```

- [ ] **Step 6: Run app spec tests and commit**

Run: `go test ./internal/appspec`

Expected: PASS.

Commit:

```bash
git add internal/appspec internal/nomad/types.go
git commit -m "feat: render application specs as nomad jobs"
```

### Task 4: Extend Nomad Client for Plan, Deploy, Stop, Runtime, and Logs

**Files:**
- Modify: `internal/nomad/client.go`
- Modify: `internal/nomad/types.go`
- Modify: `internal/nomad/client_test.go`

- [ ] **Step 1: Write client method tests**

Add tests that assert endpoint paths:

```go
func TestPlanRegisterStopAndAllocations(t *testing.T) {
	seen := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/job/panel-web/plan":
			_ = json.NewEncoder(w).Encode(PlanResponse{Diff: "diff"})
		case "/v1/job/panel-web":
			_ = json.NewEncoder(w).Encode(RegisterResponse{EvalID: "eval-1"})
		case "/v1/job/panel-web/allocations":
			_ = json.NewEncoder(w).Encode([]AllocationListItem{{ID: "alloc-1", JobID: "panel-web"}})
		default:
			if r.Method == http.MethodDelete && r.URL.Path == "/v1/job/panel-web" {
				_ = json.NewEncoder(w).Encode(StopResponse{EvalID: "eval-stop"})
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewClient(Config{Address: srv.URL, Namespace: "default"}, srv.Client())
	job := Job{ID: "panel-web", Name: "web", Type: "service", Datacenters: []string{"dc1"}}
	if _, err := client.PlanJob(context.Background(), "panel-web", job); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RegisterJob(context.Background(), "panel-web", job); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StopJob(context.Background(), "panel-web", false); err != nil {
		t.Fatal(err)
	}
	if _, err := client.JobAllocations(context.Background(), "panel-web"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /v1/job/panel-web/plan", "POST /v1/job/panel-web", "DELETE /v1/job/panel-web", "GET /v1/job/panel-web/allocations"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("seen = %#v", seen)
	}
}
```

- [ ] **Step 2: Run client tests and verify failure**

Run: `go test ./internal/nomad`

Expected: FAIL with undefined methods and types.

- [ ] **Step 3: Add DTOs**

Add to `internal/nomad/types.go`:

```go
type PlanRequest struct {
	Job Job `json:"Job"`
	Diff bool `json:"Diff"`
}

type PlanResponse struct {
	Annotations map[string]any `json:"Annotations,omitempty"`
	Diff        any            `json:"Diff,omitempty"`
	FailedTGAllocs map[string]any `json:"FailedTGAllocs,omitempty"`
	JobModifyIndex uint64     `json:"JobModifyIndex,omitempty"`
	CreatedEvals []Evaluation `json:"CreatedEvals,omitempty"`
}

type RegisterRequest struct {
	Job Job `json:"Job"`
}

type RegisterResponse struct {
	EvalID          string `json:"EvalID"`
	EvalCreateIndex uint64 `json:"EvalCreateIndex"`
	JobModifyIndex uint64 `json:"JobModifyIndex"`
}

type StopResponse struct {
	EvalID string `json:"EvalID"`
}

type Evaluation struct {
	ID     string `json:"ID"`
	Status string `json:"Status"`
	Type   string `json:"Type"`
	JobID  string `json:"JobID"`
}

type Deployment struct {
	ID     string `json:"ID"`
	JobID  string `json:"JobID"`
	Status string `json:"Status"`
	StatusDescription string `json:"StatusDescription"`
}

type AllocationListItem struct {
	ID          string `json:"ID"`
	EvalID      string `json:"EvalID"`
	Name        string `json:"Name"`
	NodeID      string `json:"NodeID"`
	JobID       string `json:"JobID"`
	TaskGroup   string `json:"TaskGroup"`
	ClientStatus string `json:"ClientStatus"`
	DesiredStatus string `json:"DesiredStatus"`
	TaskStates  map[string]TaskState `json:"TaskStates"`
}

type TaskState struct {
	State string `json:"State"`
	Failed bool `json:"Failed"`
	Events []TaskEvent `json:"Events"`
}

type TaskEvent struct {
	Type string `json:"Type"`
	Time int64 `json:"Time"`
	Message string `json:"Message"`
	DisplayMessage string `json:"DisplayMessage"`
}

type NodeListItem struct {
	ID string `json:"ID"`
	Name string `json:"Name"`
	Status string `json:"Status"`
	Eligibility string `json:"SchedulingEligibility"`
	Datacenter string `json:"Datacenter"`
}
```

- [ ] **Step 4: Add client methods**

Add to `internal/nomad/client.go`:

```go
func (c *Client) PlanJob(ctx context.Context, id string, job Job) (PlanResponse, error) {
	var out PlanResponse
	return out, c.do(ctx, http.MethodPost, "/v1/job/"+url.PathEscape(id)+"/plan", nil, PlanRequest{Job: job, Diff: true}, &out)
}

func (c *Client) RegisterJob(ctx context.Context, id string, job Job) (RegisterResponse, error) {
	var out RegisterResponse
	return out, c.do(ctx, http.MethodPost, "/v1/job/"+url.PathEscape(id), nil, RegisterRequest{Job: job}, &out)
}

func (c *Client) StopJob(ctx context.Context, id string, purge bool) (StopResponse, error) {
	q := url.Values{}
	if purge {
		q.Set("purge", "true")
	}
	var out StopResponse
	return out, c.do(ctx, http.MethodDelete, "/v1/job/"+url.PathEscape(id), q, nil, &out)
}

func (c *Client) JobAllocations(ctx context.Context, id string) ([]AllocationListItem, error) {
	var out []AllocationListItem
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/allocations", nil, nil, &out)
}

func (c *Client) JobDeployment(ctx context.Context, id string) (Deployment, error) {
	var out Deployment
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/deployment", nil, nil, &out)
}

func (c *Client) JobEvaluations(ctx context.Context, id string) ([]Evaluation, error) {
	var out []Evaluation
	return out, c.do(ctx, http.MethodGet, "/v1/job/"+url.PathEscape(id)+"/evaluations", nil, nil, &out)
}

func (c *Client) Nodes(ctx context.Context) ([]NodeListItem, error) {
	var out []NodeListItem
	return out, c.do(ctx, http.MethodGet, "/v1/nodes", nil, nil, &out)
}
```

- [ ] **Step 5: Run Nomad client tests and commit**

Run: `go test ./internal/nomad`

Expected: PASS.

Commit:

```bash
git add internal/nomad
git commit -m "feat: support nomad job operations"
```

### Task 5: Add Application Persistence

**Files:**
- Modify: `internal/storage/migrations.go`
- Create: `internal/applications/model.go`
- Test: `internal/storage/store_test.go`

- [ ] **Step 1: Write migration test**

Add a test that fresh DB contains the new tables and does not create old container orchestration tables:

```go
func TestMigrateCreatesApplicationTables(t *testing.T) {
	cfg := testConfig(t)
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"applications", "application_files", "application_revisions"} {
		if !tableExists(t, store.AppDB(), table) {
			t.Fatalf("missing table %s", table)
		}
	}
	for _, table := range []string{"container_services", "container_service_placements", "operation_locks"} {
		if tableExists(t, store.AppDB(), table) {
			t.Fatalf("old table still exists in fresh schema: %s", table)
		}
	}
}
```

- [ ] **Step 2: Run storage tests and verify failure**

Run: `go test ./internal/storage`

Expected: FAIL because application tables are missing and old tables still exist.

- [ ] **Step 3: Define application model**

Create `internal/applications/model.go`:

```go
package applications

import "time"

const (
	ResourceTypeApplication = "application"
	TaskTypeApplicationDeploy = "application_deploy"
	TaskTypeApplicationStop = "application_stop"
	TaskTypeApplicationRestart = "application_restart"
)

type Application struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	SpecYAML    string            `json:"specYaml"`
	Variables   map[string]string `json:"variables"`
	Generation  int               `json:"generation"`
	SpecHash    string            `json:"specHash"`
	JobID       string            `json:"jobId"`
	Namespace   string            `json:"namespace"`
	LastEvalID  string            `json:"lastEvalId,omitempty"`
	LastDeploymentID string       `json:"lastDeploymentId,omitempty"`
	LastError   string            `json:"lastError,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}
```

- [ ] **Step 4: Replace fresh schema**

In `internal/storage/migrations.go`, remove the `docker_capabilities`, `docker_runtime_cache`, `container_runtime_cache`, `operation_locks`, `container_services`, `container_service_files`, and `container_service_placements` create statements from the fresh schema. Add:

```sql
CREATE TABLE IF NOT EXISTS applications (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 0,
	spec_yaml TEXT NOT NULL,
	variables_json TEXT NOT NULL DEFAULT '{}',
	generation INTEGER NOT NULL DEFAULT 1,
	spec_hash TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL,
	namespace TEXT NOT NULL DEFAULT 'default',
	last_eval_id TEXT NOT NULL DEFAULT '',
	last_deployment_id TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(name)
)
```

Add:

```sql
CREATE TABLE IF NOT EXISTS application_files (
	id TEXT PRIMARY KEY,
	application_id TEXT NOT NULL,
	path TEXT NOT NULL,
	kind TEXT NOT NULL CHECK(kind IN ('binary','template')),
	content_type TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	sha256 TEXT NOT NULL DEFAULT '',
	content BLOB,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(application_id, path),
	FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
)
```

Add:

```sql
CREATE TABLE IF NOT EXISTS application_revisions (
	id TEXT PRIMARY KEY,
	application_id TEXT NOT NULL,
	generation INTEGER NOT NULL,
	spec_hash TEXT NOT NULL,
	spec_yaml TEXT NOT NULL,
	job_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE(application_id, generation),
	FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
)
```

- [ ] **Step 5: Run storage tests and commit**

Run: `go test ./internal/storage`

Expected: PASS.

Commit:

```bash
git add internal/storage/migrations.go internal/storage/store_test.go internal/applications/model.go
git commit -m "feat: add application persistence schema"
```

### Task 6: Implement Application Service

**Files:**
- Create: `internal/applications/service.go`
- Create: `internal/applications/service_test.go`
- Modify: `internal/nomad/types.go`

- [ ] **Step 1: Write service tests with fake Nomad client**

Create tests for create, deploy, stop, and runtime mapping:

```go
func TestCreateApplicationStoresDisabledSpec(t *testing.T) {
	db := testDB(t)
	nomad := &fakeNomad{}
	svc := NewService(db, nomad, applicationsConfig())
	app, err := svc.Create(context.Background(), SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx:1.27\n"})
	if err != nil {
		t.Fatal(err)
	}
	if app.Name != "web" || app.Enabled || app.Generation != 1 || app.JobID != "panel-web" {
		t.Fatalf("app = %#v", app)
	}
	if nomad.registerCalls != 0 {
		t.Fatalf("register calls = %d", nomad.registerCalls)
	}
}

func TestDeployEnabledApplicationRegistersNomadJob(t *testing.T) {
	db := testDB(t)
	nomad := &fakeNomad{registerResponse: nomadapi.RegisterResponse{EvalID: "eval-1"}}
	svc := NewService(db, nomad, applicationsConfig())
	app, err := svc.Create(context.Background(), SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx:1.27\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled || app.LastEvalID != "eval-1" {
		t.Fatalf("app = %#v", app)
	}
	if nomad.planCalls != 1 || nomad.registerCalls != 1 {
		t.Fatalf("plan=%d register=%d", nomad.planCalls, nomad.registerCalls)
	}
}
```

- [ ] **Step 2: Run service tests and verify failure**

Run: `go test ./internal/applications`

Expected: FAIL because the service does not exist.

- [ ] **Step 3: Define service dependencies and inputs**

In `internal/applications/service.go`, define:

```go
type NomadClient interface {
	PlanJob(ctx context.Context, id string, job nomad.Job) (nomad.PlanResponse, error)
	RegisterJob(ctx context.Context, id string, job nomad.Job) (nomad.RegisterResponse, error)
	StopJob(ctx context.Context, id string, purge bool) (nomad.StopResponse, error)
	JobAllocations(ctx context.Context, id string) ([]nomad.AllocationListItem, error)
	JobDeployment(ctx context.Context, id string) (nomad.Deployment, error)
	JobEvaluations(ctx context.Context, id string) ([]nomad.Evaluation, error)
}

type Config struct {
	Namespace  string
	Region     string
	Datacenter string
}

type Service struct {
	db *sql.DB
	nomad NomadClient
	cfg Config
}

type SaveInput struct {
	Name string `json:"name"`
	Enabled bool `json:"enabled"`
	SpecYAML string `json:"specYaml"`
	Variables map[string]string `json:"variables"`
}
```

- [ ] **Step 4: Implement create and deploy flow**

Implement these methods:

```go
func NewService(db *sql.DB, nomadClient NomadClient, cfg Config) *Service
func (s *Service) Create(ctx context.Context, in SaveInput) (Application, error)
func (s *Service) Deploy(ctx context.Context, id string) (Application, error)
func (s *Service) Stop(ctx context.Context, id string, purge bool) (Application, error)
func (s *Service) Runtime(ctx context.Context, id string) (Runtime, error)
```

Use `gopkg.in/yaml.v3` to decode `SpecYAML` into `appspec.Spec`. Compute `spec_hash` as SHA-256 over normalized spec YAML and variables JSON. Set `job_id` to `"panel-"+spec.Name`. Store a row in `application_revisions` for each new generation with the rendered Nomad job JSON.

Deploy sequence:

1. Load app.
2. Decode and render spec.
3. Call `PlanJob`.
4. Call `RegisterJob`.
5. Save `last_eval_id`.
6. Return updated app.

Stop sequence:

1. Load app.
2. Call `StopJob(job_id, purge)`.
3. Set `enabled=false`.
4. Save `last_eval_id`.

- [ ] **Step 5: Run application service tests and commit**

Run: `go test ./internal/applications`

Expected: PASS.

Commit:

```bash
git add internal/applications internal/nomad/types.go go.mod go.sum
git commit -m "feat: manage applications through nomad"
```

### Task 7: Add Application HTTP API

**Files:**
- Create: `internal/applications/handler.go`
- Create: `internal/applications/handler_test.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Write handler tests**

Cover routes:

```go
func TestApplicationRoutes(t *testing.T) {
	svc := &fakeApplicationService{}
	h := NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run handler tests and verify failure**

Run: `go test ./internal/applications`

Expected: FAIL with missing handler.

- [ ] **Step 3: Implement handler methods**

Create `internal/applications/handler.go` with methods:

```go
func (h *Handler) List(w http.ResponseWriter, r *http.Request)
func (h *Handler) Create(w http.ResponseWriter, r *http.Request)
func (h *Handler) Get(w http.ResponseWriter, r *http.Request)
func (h *Handler) Update(w http.ResponseWriter, r *http.Request)
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request)
func (h *Handler) Validate(w http.ResponseWriter, r *http.Request)
func (h *Handler) Plan(w http.ResponseWriter, r *http.Request)
func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request)
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request)
func (h *Handler) Restart(w http.ResponseWriter, r *http.Request)
func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request)
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request)
```

API paths:

```text
GET    /api/v1/applications
POST   /api/v1/applications
GET    /api/v1/applications/{id}
PUT    /api/v1/applications/{id}
DELETE /api/v1/applications/{id}
POST   /api/v1/applications/{id}/validate
POST   /api/v1/applications/{id}/plan
POST   /api/v1/applications/{id}/deploy
POST   /api/v1/applications/{id}/stop
POST   /api/v1/applications/{id}/restart
GET    /api/v1/applications/{id}/runtime
GET    /api/v1/applications/{id}/logs
```

- [ ] **Step 4: Wire app routes**

In `internal/app/app.go`, construct:

```go
nomadClient := nomad.NewClient(nomad.Config{
	Address: cfg.Nomad.Address,
	Token: cfg.Nomad.Token,
	Namespace: cfg.Nomad.Namespace,
	Region: cfg.Nomad.Region,
	Datacenter: cfg.Nomad.Datacenter,
}, nil)
applicationSvc := applications.NewService(store.AppDB(), nomadClient, applications.Config{
	Namespace: cfg.Nomad.Namespace,
	Region: cfg.Nomad.Region,
	Datacenter: cfg.Nomad.Datacenter,
})
```

Remove construction of `containerops.NewWorker`, `containerservice.NewService`, and Docker mutation routing for old Container Services.

- [ ] **Step 5: Run backend tests and commit**

Run: `go test ./internal/applications ./internal/app`

Expected: PASS.

Commit:

```bash
git add internal/applications internal/app/app.go
git commit -m "feat: expose application api"
```

### Task 8: Replace Frontend Container Services With Applications

**Files:**
- Create: `web/src/api/applications.ts`
- Create: `web/src/features/applications/pages/ApplicationsPage.vue`
- Create: `web/src/features/applications/components/ApplicationEditor.vue`
- Create: `web/src/features/applications/components/ApplicationDetail.vue`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/layouts/AppLayout.vue`
- Modify: `web/src/types/api.ts`
- Delete: `web/src/features/container-services/`
- Delete: `web/src/api/containerServices.ts`

- [ ] **Step 1: Write API client tests**

Create tests equivalent to existing API tests:

```ts
import { describe, expect, it, vi } from 'vitest';
import { applicationsApi } from './applications';

describe('applicationsApi', () => {
  it('lists applications', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{ id: 'app_1', name: 'web', enabled: true }],
      headers: new Headers({ 'content-type': 'application/json' }),
    });
    globalThis.fetch = fetchMock as typeof fetch;
    const apps = await applicationsApi.list();
    expect(apps[0].name).toBe('web');
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/applications', expect.any(Object));
  });
});
```

- [ ] **Step 2: Run frontend tests and verify failure**

Run: `cd web; npm test -- applications`

Expected: FAIL because the API client does not exist.

- [ ] **Step 3: Implement API client**

Create methods:

```ts
export const applicationsApi = {
  list: () => get<ApplicationDto[]>('/api/v1/applications'),
  create: (body: ApplicationSaveDto) => post<ApplicationDto>('/api/v1/applications', body),
  update: (id: string, body: ApplicationSaveDto) => put<ApplicationDto>(`/api/v1/applications/${id}`, body),
  remove: (id: string) => del<void>(`/api/v1/applications/${id}`),
  validate: (id: string) => post<ApplicationValidationDto>(`/api/v1/applications/${id}/validate`, {}),
  plan: (id: string) => post<ApplicationPlanDto>(`/api/v1/applications/${id}/plan`, {}),
  deploy: (id: string) => post<ApplicationOperationDto>(`/api/v1/applications/${id}/deploy`, {}),
  stop: (id: string) => post<ApplicationOperationDto>(`/api/v1/applications/${id}/stop`, {}),
  restart: (id: string) => post<ApplicationOperationDto>(`/api/v1/applications/${id}/restart`, {}),
  runtime: (id: string) => get<ApplicationRuntimeDto>(`/api/v1/applications/${id}/runtime`),
  logs: (id: string, tail = 200) => get<ApplicationLogsDto>(`/api/v1/applications/${id}/logs?tail=${tail}`),
};
```

- [ ] **Step 4: Implement pages and routes**

Use an operational layout:

- Left side: applications table with name, enabled, job ID, deployment status, alloc count, last eval.
- Right side: selected application detail with deployment, allocations, events, and logs.
- Editor dialog: name, enabled toggle, YAML spec editor, validate, plan, deploy.

Update routes:

```ts
{ path: 'applications', name: 'applications', component: ApplicationsPage, meta: { title: 'Applications' } },
{ path: 'nomad/nodes', name: 'nomad-nodes', component: NomadNodesPage, meta: { title: 'Nomad Nodes' } },
{ path: 'deployments', name: 'deployments', component: DeploymentsPage, meta: { title: 'Deployments' } },
```

Redirect old paths:

```ts
{ path: 'container-services', redirect: '/applications' },
{ path: 'runtime-explorer', redirect: '/nomad/nodes' },
```

- [ ] **Step 5: Run frontend tests and build**

Run: `cd web; npm test -- applications`

Expected: PASS.

Run: `cd web; npm run build`

Expected: PASS.

Commit:

```bash
git add web/src
git rm -r web/src/features/container-services web/src/api/containerServices.ts
git commit -m "feat: replace container services ui with applications"
```

### Task 9: Add Nomad Nodes and Deployment Views

**Files:**
- Create: `internal/nomad/handler.go`
- Modify: `internal/app/app.go`
- Create: `web/src/api/nomad.ts`
- Create: `web/src/features/nomad/pages/NomadNodesPage.vue`
- Create: `web/src/features/deployments/pages/DeploymentsPage.vue`

- [ ] **Step 1: Write API tests**

Test endpoints:

```text
GET /api/v1/nomad/status
GET /api/v1/nomad/nodes
GET /api/v1/nomad/jobs
GET /api/v1/nomad/deployments
GET /api/v1/nomad/evaluations
```

Expected handler behavior:

- `status` calls Nomad agent self/status or jobs list and returns `{ connected: true }` when the request succeeds.
- `nodes` calls `/v1/nodes`.
- `jobs` calls `/v1/jobs`.
- `deployments` calls `/v1/deployments`.
- `evaluations` calls `/v1/evaluations`.

- [ ] **Step 2: Run API tests and verify failure**

Run: `go test ./internal/nomad ./internal/app`

Expected: FAIL because handler endpoints are missing.

- [ ] **Step 3: Implement Nomad read-only handler**

Create a handler that wraps read-only methods. Use `httpx.JSON` for success and `httpx.Error` for failures. Do not expose ACL token values in responses.

- [ ] **Step 4: Implement frontend views**

`NomadNodesPage.vue` shows:

- node name
- datacenter
- status
- eligibility
- allocation count
- node detail link with current allocations

`DeploymentsPage.vue` shows:

- deployment ID
- job ID
- status
- status description
- latest evaluation ID

- [ ] **Step 5: Run tests and commit**

Run:

```bash
go test ./internal/nomad ./internal/app
cd web
npm test -- nomad deployments
npm run build
```

Expected: PASS for all commands.

Commit:

```bash
git add internal/nomad internal/app/app.go web/src
git commit -m "feat: add nomad inventory views"
```

### Task 10: Remove Old Compose Orchestration Code

**Files:**
- Delete: `internal/containerops/`
- Delete: `internal/containerrender/`
- Delete: `internal/containerservice/`
- Delete: `internal/placement/`
- Modify: `internal/docker/`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/app/app.go`
- Modify: `web/src/router/index.ts`

- [ ] **Step 1: Remove old packages**

Delete packages only after Tasks 1-9 pass:

```bash
git rm -r internal/containerops internal/containerrender internal/containerservice internal/placement
```

- [ ] **Step 2: Remove old imports and scheduler references**

Use `rg "containerops|containerrender|containerservice|placement|Container Service|container-services"` to find remaining references. Remove each reference by either replacing it with Applications/Nomad behavior or deleting old route/UI text.

- [ ] **Step 3: Run backend tests and fix compile errors**

Run: `go test ./...`

Expected first run: FAIL only with compile errors pointing to old imports. Remove every old reference until the command passes.

- [ ] **Step 4: Run frontend tests and fix route/API references**

Run:

```bash
cd web
npm test
npm run build
```

Expected first run: FAIL only with references to old container service modules. Remove every old reference until both commands pass.

- [ ] **Step 5: Commit cleanup**

Commit:

```bash
git add -A
git commit -m "refactor: remove compose orchestration layer"
```

### Task 11: End-to-End Verification

**Files:**
- Modify: `docs/servers.md`
- Create: `docs/nomad-operations.md`

- [ ] **Step 1: Add operations documentation**

Create `docs/nomad-operations.md` with:

```markdown
# Nomad Operations

Panel expects an existing Nomad cluster reachable from the panel backend.

Required config:

- `nomad.address`: Nomad HTTP API address.
- `nomad.token`: ACL token when ACLs are enabled.
- `nomad.namespace`: default namespace for panel-created jobs.
- `nomad.region`: target region.
- `nomad.datacenter`: target datacenter for rendered jobs.

Panel-created jobs use IDs prefixed with `panel-` and include these meta keys:

- `panel.app.id`
- `panel.app.name`
- `panel.generation`
- `panel.spec_hash`

Runtime state comes from Nomad jobs, deployments, evaluations, allocations, services, checks, and allocation logs.
```

- [ ] **Step 2: Run full backend verification**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 3: Run full frontend verification**

Run:

```bash
cd web
npm test
npm run build
```

Expected: PASS.

- [ ] **Step 4: Run reference scan**

Run:

```bash
rg "container-services|Container Services|docker compose|compose include|operation_locks|container_service_placements|panel.claims.ports"
```

Expected: no active product or runtime references. Because old design docs are deleted on this branch, any remaining doc match must be intentionally quoted as a removal target in the Nomad task split.

- [ ] **Step 5: Commit docs and verification fixes**

Commit:

```bash
git add docs internal web
git commit -m "docs: document nomad operations"
```

---

## Self-Review

Spec coverage:

- Nomad as the only scheduler and runtime control plane: Tasks 2, 4, 6, 7, 9, 10.
- Panel-owned Application model: Tasks 3, 5, 6, 8.
- Job plan/register/stop lifecycle: Tasks 4, 6, 7.
- Runtime status from deployments/evaluations/allocations: Tasks 4, 6, 9.
- Removal of current complex Compose design: Tasks 5, 8, 10, 11.
- Official docs available locally: this plan references `docs/nomad-api-docs`.

Placeholder scan:

- The plan intentionally avoids deferred compatibility, migration paths, and old Compose behavior.
- Each implementation task has concrete files, commands, and expected verification results.

Type consistency:

- `Application` uses `jobId`, `namespace`, `generation`, `specHash`, `lastEvalId`, and `lastDeploymentId` consistently across backend and frontend tasks.
- Nomad job renderer uses `panel-<app name>` consistently for `job_id`.
- Runtime data flows through Nomad DTOs rather than Docker/Compose DTOs.
