# Task 04: Application Service And HTTP API

**Goal:** Implement Application CRUD and runtime operations backed by Nomad jobs.

**Architecture:** `internal/applications.Service` owns DB state transitions and calls a narrow Nomad client interface. `internal/applications.Handler` exposes REST endpoints. Nomad remains the runtime source of truth.

**Tech Stack:** Go, SQLite, existing `httpx`, existing `tasks`, `internal/appspec`, `internal/nomad`.

---

## Ownership

Primary owner: Backend product API.

Can start after:

- Task 01 client interfaces exist.
- Task 02 app spec renderer exists.
- Task 03 persistence schema exists.

Blocks:

- Task 06 frontend Applications page.
- Task 08 final cleanup.

## Official Docs

- `docs/nomad-api-docs/jobs.mdx`
- `docs/nomad-api-docs/validate.mdx`
- `docs/nomad-api-docs/allocations.mdx`
- `docs/nomad-api-docs/deployments.mdx`
- `docs/nomad-api-docs/evaluations.mdx`

## Files

Create:

- `internal/applications/service.go`
- `internal/applications/handler.go`
- `internal/applications/service_test.go`
- `internal/applications/handler_test.go`

Modify:

- `internal/app/app.go`
- `internal/tasks/model.go` if new task constants are centralized there.

## REST API Contract

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/api/v1/applications` | List Applications plus summarized runtime fields when available. |
| `POST` | `/api/v1/applications` | Create Application. If `enabled=true`, plan and deploy immediately. |
| `GET` | `/api/v1/applications/{id}` | Read one Application. |
| `PUT` | `/api/v1/applications/{id}` | Update spec, variables, enabled flag. If enabled and render-affecting content changes, plan and deploy. |
| `DELETE` | `/api/v1/applications/{id}` | Delete disabled Application. If enabled, return conflict. |
| `POST` | `/api/v1/applications/{id}/validate` | Decode, validate, render, and call Nomad validate endpoint. |
| `POST` | `/api/v1/applications/{id}/plan` | Render current spec and call Nomad job plan. |
| `POST` | `/api/v1/applications/{id}/deploy` | Render, plan, register job, and record eval ID. |
| `POST` | `/api/v1/applications/{id}/stop` | Deregister Nomad job with `purge=false` and mark disabled. |
| `POST` | `/api/v1/applications/{id}/restart` | Restart allocations or force deploy by bumping restart nonce. |
| `GET` | `/api/v1/applications/{id}/runtime` | Read job deployment, evaluations, allocations, and derived status. |
| `GET` | `/api/v1/applications/{id}/logs` | Read allocation logs for a task. |

## Service Interface

```go
type NomadClient interface {
	ValidateJob(ctx context.Context, job nomad.Job) (nomad.ValidateResponse, error)
	PlanJob(ctx context.Context, id string, job nomad.Job) (nomad.PlanResponse, error)
	RegisterJob(ctx context.Context, id string, job nomad.Job) (nomad.RegisterResponse, error)
	StopJob(ctx context.Context, id string, purge bool) (nomad.StopResponse, error)
	JobAllocations(ctx context.Context, id string) ([]nomad.AllocationListItem, error)
	JobDeployment(ctx context.Context, id string) (nomad.Deployment, error)
	JobEvaluations(ctx context.Context, id string) ([]nomad.Evaluation, error)
	RestartAllocation(ctx context.Context, allocID, task string) error
	AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error)
}
```

## Runtime Status Mapping

- `deployment.Status == "running"` and all allocations `ClientStatus == "running"` -> `running`.
- any allocation `ClientStatus == "failed"` -> `failed`.
- no allocations for enabled app -> `pending`.
- disabled app -> `stopped`.
- Nomad read error -> `unknown` plus error message.

## Steps

- [ ] **Step 1: Write failing service tests**

Create `internal/applications/service_test.go` with fake Nomad client tests for:

- create disabled app stores DB row and does not call Nomad;
- create enabled app calls validate, plan, and register;
- update disabled app increments generation only when spec hash changes;
- stop app calls `StopJob(jobID, false)` and sets `enabled=false`;
- runtime maps deployments/evaluations/allocations into `ApplicationRuntime`.

- [ ] **Step 2: Verify service test failure**

Run: `go test ./internal/applications`

Expected: FAIL because service methods are missing.

- [ ] **Step 3: Implement service**

Implement:

```go
func NewService(db *sql.DB, nomad NomadClient, tasks *tasks.Service, cfg Config) *Service
func (s *Service) List(ctx context.Context) ([]Application, error)
func (s *Service) Get(ctx context.Context, id string) (Application, error)
func (s *Service) Create(ctx context.Context, in SaveInput) (Application, error)
func (s *Service) Update(ctx context.Context, id string, in SaveInput) (Application, error)
func (s *Service) Delete(ctx context.Context, id string) error
func (s *Service) Validate(ctx context.Context, id string) (ValidationResult, error)
func (s *Service) Plan(ctx context.Context, id string) (PlanResult, error)
func (s *Service) Deploy(ctx context.Context, id string) (OperationResult, error)
func (s *Service) Stop(ctx context.Context, id string, purge bool) (OperationResult, error)
func (s *Service) Restart(ctx context.Context, id string) (OperationResult, error)
func (s *Service) Runtime(ctx context.Context, id string) (ApplicationRuntime, error)
func (s *Service) Logs(ctx context.Context, id string, in LogInput) (LogResult, error)
```

- [ ] **Step 4: Write failing handler tests**

Create `internal/applications/handler_test.go` with tests for list, create, deploy, stop, runtime, and logs handlers using a fake service.

- [ ] **Step 5: Implement handler**

Use existing handler style from nearby modules. Extract `{id}` from path with the repo's current path parsing pattern. Return `httpx.JSON` on success and `httpx.Error` on failure.

- [ ] **Step 6: Wire `internal/app/app.go`**

Construct Nomad client and Application service. Add routes from the REST API contract. Remove old Container Services route wiring from the active route switch.

- [ ] **Step 7: Run verification**

Run:

```bash
go test ./internal/applications ./internal/app
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/applications internal/app/app.go internal/tasks
git commit -m "feat: expose nomad-backed applications api"
```

## Handoff Notes

Give Task 06 owners real JSON examples for:

- `ApplicationDto`
- `ApplicationRuntimeDto`
- `ValidationResult`
- `PlanResult`
- `OperationResult`
- `LogResult`
