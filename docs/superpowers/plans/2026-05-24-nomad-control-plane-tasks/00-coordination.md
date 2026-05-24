# Nomad Control Plane Task Split

**Goal:** Coordinate multi-person development for replacing Panel's self-built Docker Compose orchestration layer with a Nomad-backed application control plane.

**Architecture:** Nomad owns scheduling, placement, deployments, evaluations, allocations, services, checks, and logs. Panel owns product-level Application desired state, renders Nomad JSON jobs, calls Nomad HTTP APIs, stores task history, and displays runtime state from Nomad.

**Tech Stack:** Go, SQLite, Vue 3, Vite, Vuetify, Pinia, Nomad HTTP API v2.0.x copied under `docs/nomad-api-docs`.

---

## Source Material

- Parent plan: `docs/superpowers/plans/2026-05-24-nomad-control-plane.md`
- Official Nomad API docs: `docs/nomad-api-docs/`
- Official docs source marker: `docs/nomad-api-docs/README.md`
- Current backend wiring: `internal/app/app.go`
- Current DB schema: `internal/storage/migrations.go`
- Current frontend routes: `web/src/router/index.ts`

## Team Task Files

| Task | Owner Track | Can Start After | Blocks |
| --- | --- | --- | --- |
| `01-nomad-config-client.md` | Backend platform | Immediately | All backend Nomad integration |
| `02-appspec-renderer.md` | Backend domain | Task 01 DTO names agreed | Tasks 04, 06 |
| `03-application-persistence.md` | Backend storage | Immediately | Task 04 |
| `04-application-service-api.md` | Backend product API | Tasks 01, 02, 03 | Frontend task 06 full integration |
| `05-nomad-runtime-inventory.md` | Backend runtime/read models | Task 01 | Frontend task 07 |
| `06-frontend-applications.md` | Frontend application workspace | API contracts in Task 04 | Task 08 final verification |
| `07-frontend-nomad-views.md` | Frontend Nomad operations views | API contracts in Task 05 | Task 08 final verification |
| `08-removal-verification-docs.md` | Integration/cleanup | Tasks 01-07 merged | Final branch readiness |

## Integration Rules

- Use `on-nomad` as the integration branch.
- Each task should be developed on its own branch from `on-nomad`, preferably `codex/nomad-task-N-short-name`.
- Each task must commit only files listed in its task file unless coordination updates are required.
- Do not keep compatibility shims for old Container Services, Compose include, port claims, root compose files, operation locks, or Docker label idempotency.
- Test and debug intermediate files must go under `tmp/`, following `AGENTS.md`.
- Preserve existing Linux server, credentials, package, metrics, auth, task center, and settings functionality unless a task explicitly touches it.

## Shared Naming Contract

Panel resource names:

- Product resource: `Application`
- API namespace: `/api/v1/applications`
- Nomad job ID: `panel-<application-name>`
- Nomad namespace: config value `nomad.namespace`, default `default`
- Nomad datacenter: config value `nomad.datacenter`, default `dc1`

Nomad job meta keys:

- `panel.app.id`
- `panel.app.name`
- `panel.generation`
- `panel.spec_hash`

Task types:

- `application_deploy`
- `application_stop`
- `application_restart`
- `application_delete`
- `nomad_refresh`

## Shared Backend DTO Contract

```go
type Application struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	SpecYAML         string            `json:"specYaml"`
	Variables        map[string]string `json:"variables"`
	Generation       int               `json:"generation"`
	SpecHash         string            `json:"specHash"`
	JobID            string            `json:"jobId"`
	Namespace        string            `json:"namespace"`
	LastEvalID       string            `json:"lastEvalId,omitempty"`
	LastDeploymentID string            `json:"lastDeploymentId,omitempty"`
	LastError        string            `json:"lastError,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}
```

```go
type ApplicationRuntime struct {
	ApplicationID string                     `json:"applicationId"`
	JobID         string                     `json:"jobId"`
	JobStatus     string                     `json:"jobStatus"`
	Deployment    *nomad.Deployment         `json:"deployment,omitempty"`
	Evaluations   []nomad.Evaluation        `json:"evaluations"`
	Allocations   []nomad.AllocationListItem `json:"allocations"`
	Services      []nomad.ServiceRegistration `json:"services,omitempty"`
	ObservedAt    time.Time                  `json:"observedAt"`
}
```

## Shared Frontend DTO Contract

```ts
export interface ApplicationDto {
  id: string;
  name: string;
  enabled: boolean;
  specYaml: string;
  variables: Record<string, string>;
  generation: number;
  specHash: string;
  jobId: string;
  namespace: string;
  lastEvalId?: string;
  lastDeploymentId?: string;
  lastError?: string;
  runtimeStatus?: string;
  allocationCount?: number;
  createdAt: string;
  updatedAt: string;
}
```

```ts
export interface ApplicationSaveDto {
  name: string;
  enabled: boolean;
  specYaml: string;
  variables: Record<string, string>;
}
```

```ts
export interface ApplicationRuntimeDto {
  applicationId: string;
  jobId: string;
  jobStatus: string;
  deployment?: NomadDeploymentDto;
  evaluations: NomadEvaluationDto[];
  allocations: NomadAllocationDto[];
  services?: NomadServiceRegistrationDto[];
  observedAt: string;
}
```

## Shared API Contract

Applications:

- `GET /api/v1/applications`
- `POST /api/v1/applications`
- `GET /api/v1/applications/{id}`
- `PUT /api/v1/applications/{id}`
- `DELETE /api/v1/applications/{id}`
- `POST /api/v1/applications/{id}/validate`
- `POST /api/v1/applications/{id}/plan`
- `POST /api/v1/applications/{id}/deploy`
- `POST /api/v1/applications/{id}/stop`
- `POST /api/v1/applications/{id}/restart`
- `GET /api/v1/applications/{id}/runtime`
- `GET /api/v1/applications/{id}/logs?allocId=<id>&task=<task>&tail=200`

Nomad inventory:

- `GET /api/v1/nomad/status`
- `GET /api/v1/nomad/nodes`
- `GET /api/v1/nomad/jobs`
- `GET /api/v1/nomad/deployments`
- `GET /api/v1/nomad/evaluations`
- `GET /api/v1/nomad/services`

## Shared Nomad Official Docs Map

- Config/client basics: `docs/nomad-api-docs/index.mdx`, `docs/nomad-api-docs/status.mdx`, `docs/nomad-api-docs/agent.mdx`
- Job CRUD, plan, allocations, deployments, evaluations: `docs/nomad-api-docs/jobs.mdx`
- JSON job syntax: `docs/nomad-api-docs/json-jobs.mdx`
- Validation: `docs/nomad-api-docs/validate.mdx`
- Allocation restart, checks, logs-related endpoints: `docs/nomad-api-docs/allocations.mdx`, `docs/nomad-api-docs/client.mdx`
- Deployments: `docs/nomad-api-docs/deployments.mdx`
- Evaluations: `docs/nomad-api-docs/evaluations.mdx`
- Nodes: `docs/nomad-api-docs/nodes.mdx`
- Services: `docs/nomad-api-docs/services.mdx`

## Cross-Team Merge Order

1. Task 01
2. Task 02 and Task 03 in either order after Task 01 DTOs settle
3. Task 04
4. Task 05
5. Task 06 and Task 07 in either order after their backend APIs are available
6. Task 08

## Definition Of Done For Every Task

- The task file checklist is complete.
- The task-specific tests pass.
- Any changed APIs are documented in the task file and reflected in shared DTOs when needed.
- `git status --short` contains only intended files before commit.
- The commit message follows the task file's recommended message.
