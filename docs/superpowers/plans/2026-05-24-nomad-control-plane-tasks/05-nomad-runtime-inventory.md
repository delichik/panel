# Task 05: Nomad Runtime Inventory APIs

**Goal:** Expose read-only Nomad cluster inventory APIs for nodes, jobs, deployments, evaluations, and services.

**Architecture:** `internal/nomad.Handler` wraps the low-level client from Task 01. These endpoints are operational visibility surfaces and do not mutate Nomad state in this task.

**Tech Stack:** Go, existing HTTP routing style, Nomad HTTP API.

---

## Ownership

Primary owner: Backend runtime/read models.

Can start after Task 01.

Blocks:

- Task 07 frontend Nomad views.

## Official Docs

- `docs/nomad-api-docs/status.mdx`
- `docs/nomad-api-docs/nodes.mdx`
- `docs/nomad-api-docs/jobs.mdx`
- `docs/nomad-api-docs/deployments.mdx`
- `docs/nomad-api-docs/evaluations.mdx`
- `docs/nomad-api-docs/services.mdx`

## Files

Create:

- `internal/nomad/handler.go`
- `internal/nomad/handler_test.go`

Modify:

- `internal/nomad/client.go`
- `internal/nomad/types.go`
- `internal/app/app.go`

## API Contract

| Method | Path | Nomad Source |
| --- | --- | --- |
| `GET` | `/api/v1/nomad/status` | call a light Nomad API and return connection state |
| `GET` | `/api/v1/nomad/nodes` | `GET /v1/nodes` |
| `GET` | `/api/v1/nomad/jobs` | `GET /v1/jobs` |
| `GET` | `/api/v1/nomad/deployments` | `GET /v1/deployments` |
| `GET` | `/api/v1/nomad/evaluations` | `GET /v1/evaluations` |
| `GET` | `/api/v1/nomad/services` | `GET /v1/services` |

Do not return `nomad.token` or request headers in any response.

## Steps

- [ ] **Step 1: Write failing client tests**

Add tests in `internal/nomad/client_test.go` for `Nodes`, `Deployments`, `Evaluations`, and `Services` endpoint paths.

- [ ] **Step 2: Implement missing client methods**

Add DTOs and methods needed for inventory. Keep DTO fields minimal but include:

- node: ID, name, status, datacenter, scheduling eligibility;
- job: ID, name, type, status, namespace, datacenters;
- deployment: ID, job ID, status, status description;
- evaluation: ID, job ID, type, status;
- service: service name, tags, namespace.

- [ ] **Step 3: Write failing handler tests**

Create `internal/nomad/handler_test.go` with fake client tests for all `/api/v1/nomad/*` paths.

- [ ] **Step 4: Implement handler**

Create `internal/nomad/handler.go` with:

```go
func (h *Handler) Status(w http.ResponseWriter, r *http.Request)
func (h *Handler) Nodes(w http.ResponseWriter, r *http.Request)
func (h *Handler) Jobs(w http.ResponseWriter, r *http.Request)
func (h *Handler) Deployments(w http.ResponseWriter, r *http.Request)
func (h *Handler) Evaluations(w http.ResponseWriter, r *http.Request)
func (h *Handler) Services(w http.ResponseWriter, r *http.Request)
```

- [ ] **Step 5: Wire app routes**

Add routes in `internal/app/app.go` under authenticated `/api/v1/`.

- [ ] **Step 6: Run verification**

Run:

```bash
go test ./internal/nomad ./internal/app
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/nomad internal/app/app.go
git commit -m "feat: expose nomad runtime inventory"
```

## Handoff Notes

Give Task 07 owners sample JSON for nodes, deployments, evaluations, jobs, and services.
