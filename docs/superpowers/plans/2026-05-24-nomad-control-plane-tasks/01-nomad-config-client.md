# Task 01: Nomad Config And HTTP Client

**Goal:** Add first-class Nomad configuration and a tested low-level Nomad HTTP client.

**Architecture:** `internal/config` owns user configuration. `internal/nomad` owns HTTP transport, Nomad headers, namespace query parameters, request/response JSON, and typed DTOs used by higher layers.

**Tech Stack:** Go standard library `net/http`, `httptest`, JSON, SQLite unaffected.

---

## Ownership

Primary owner: Backend platform.

Can start immediately.

Blocks:

- `02-appspec-renderer.md` for shared Nomad DTOs.
- `04-application-service-api.md` for job plan/register/stop.
- `05-nomad-runtime-inventory.md` for read-only Nomad APIs.

## Official Docs

- `docs/nomad-api-docs/jobs.mdx`
- `docs/nomad-api-docs/validate.mdx`
- `docs/nomad-api-docs/nodes.mdx`
- `docs/nomad-api-docs/deployments.mdx`
- `docs/nomad-api-docs/evaluations.mdx`
- `docs/nomad-api-docs/services.mdx`
- `docs/nomad-api-docs/allocations.mdx`
- `docs/nomad-api-docs/client.mdx`

## Files

Modify:

- `config.example.json`
- `internal/config/config.go`
- `internal/config/config_test.go`

Create:

- `internal/nomad/config.go`
- `internal/nomad/client.go`
- `internal/nomad/types.go`
- `internal/nomad/client_test.go`

## Public Interface

```go
package nomad

type Config struct {
	Address    string
	Token      string
	Namespace  string
	Region     string
	Datacenter string
}

type Client struct {
	// fields are private
}

func NewClient(cfg Config, httpClient *http.Client) *Client
func (c *Client) ListJobs(ctx context.Context, prefix string) ([]JobListItem, error)
func (c *Client) ReadJob(ctx context.Context, id string) (Job, error)
func (c *Client) ValidateJob(ctx context.Context, job Job) (ValidateResponse, error)
func (c *Client) PlanJob(ctx context.Context, id string, job Job) (PlanResponse, error)
func (c *Client) RegisterJob(ctx context.Context, id string, job Job) (RegisterResponse, error)
func (c *Client) StopJob(ctx context.Context, id string, purge bool) (StopResponse, error)
func (c *Client) JobAllocations(ctx context.Context, id string) ([]AllocationListItem, error)
func (c *Client) JobDeployment(ctx context.Context, id string) (Deployment, error)
func (c *Client) JobEvaluations(ctx context.Context, id string) ([]Evaluation, error)
func (c *Client) Nodes(ctx context.Context) ([]NodeListItem, error)
func (c *Client) Deployments(ctx context.Context) ([]Deployment, error)
func (c *Client) Evaluations(ctx context.Context) ([]Evaluation, error)
func (c *Client) Services(ctx context.Context) ([]ServiceRegistration, error)
func (c *Client) RestartAllocation(ctx context.Context, allocID, task string) error
func (c *Client) AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error)
```

## Nomad Endpoint Mapping

| Client Method | Nomad API | Official Doc |
| --- | --- | --- |
| `ListJobs` | `GET /v1/jobs` | `jobs.mdx` |
| `ReadJob` | `GET /v1/job/:job_id` | `jobs.mdx` |
| `ValidateJob` | `POST /v1/validate/job` | `validate.mdx` |
| `PlanJob` | `POST /v1/job/:job_id/plan` | `jobs.mdx` |
| `RegisterJob` | `POST /v1/job/:job_id` | `jobs.mdx` |
| `StopJob` | `DELETE /v1/job/:job_id` | `jobs.mdx` |
| `JobAllocations` | `GET /v1/job/:job_id/allocations` | `jobs.mdx` |
| `JobDeployment` | `GET /v1/job/:job_id/deployment` | `jobs.mdx` |
| `JobEvaluations` | `GET /v1/job/:job_id/evaluations` | `jobs.mdx` |
| `Nodes` | `GET /v1/nodes` | `nodes.mdx` |
| `Deployments` | `GET /v1/deployments` | `deployments.mdx` |
| `Evaluations` | `GET /v1/evaluations` | `evaluations.mdx` |
| `Services` | `GET /v1/services` | `services.mdx` |
| `RestartAllocation` | `POST /v1/client/allocation/:alloc_id/restart` | `allocations.mdx` |

## Steps

- [ ] **Step 1: Add failing config tests**

Add tests in `internal/config/config_test.go` for explicit Nomad config and default Nomad config:

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
	if cfg.Nomad.Address != "https://nomad.service:4646" || cfg.Nomad.Token != "root-token" || cfg.Nomad.Namespace != "apps" || cfg.Nomad.Region != "global" || cfg.Nomad.Datacenter != "dc1" {
		t.Fatalf("nomad config = %#v", cfg.Nomad)
	}
}
```

- [ ] **Step 2: Verify config test failure**

Run: `go test ./internal/config`

Expected: FAIL with missing `Nomad` field or assertions failing.

- [ ] **Step 3: Implement config fields**

Add `NomadConfig` to `internal/config/config.go`, add a `Nomad NomadConfig` field to `Config`, set defaults, and normalize empty decoded values back to defaults.

- [ ] **Step 4: Update config example**

Add this JSON block to `config.example.json`:

```json
"nomad": {
  "address": "http://127.0.0.1:4646",
  "token": "",
  "namespace": "default",
  "region": "global",
  "datacenter": "dc1"
}
```

- [ ] **Step 5: Add failing Nomad client tests**

Create `internal/nomad/client_test.go` with tests for:

- `X-Nomad-Token` header.
- `namespace` query parameter.
- endpoint path construction.
- non-2xx Nomad error decoding.
- plan/register/stop endpoint paths.

- [ ] **Step 6: Implement Nomad client**

Create `internal/nomad/config.go`, `internal/nomad/client.go`, and `internal/nomad/types.go`. Keep `do` private and use typed public methods only.

- [ ] **Step 7: Run verification**

Run:

```bash
go test ./internal/config ./internal/nomad
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add config.example.json internal/config internal/nomad
git commit -m "feat: add nomad configuration and client"
```

## Handoff Notes

After this task lands, tell Task 02 and Task 04 owners the final names of `nomad.Job`, `nomad.TaskGroup`, `nomad.Task`, `nomad.PlanResponse`, `nomad.RegisterResponse`, `nomad.AllocationListItem`, `nomad.Deployment`, and `nomad.Evaluation`.
