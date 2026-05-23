# Container Services Full Redesign

## Goal

Container Services is a full replacement for the current Docker Compose service template / deployed service model.

This redesign is not a compatibility layer and not an incremental migration. The old service template, deployed service, compose deployment, and legacy `/services` / `/service-templates` product model must be removed during implementation. The new product has one user-created application resource: **Container Service**.

The implementation must close the loop end to end:

1. A user creates or edits a Service.
2. The system validates the Service spec.
3. The system schedules it onto one eligible node.
4. The system renders service artifacts.
5. The system updates the node-level Compose root file.
6. Docker Compose starts or updates the runtime.
7. The system verifies runtime state.
8. Runtime status, logs, tasks, and errors are visible in the UI.

## Non-Goals

- No compatibility with `service_templates`, `deployed_services`, legacy Compose deployments, or old product routes.
- No migration flow from old template/deployed-service data.
- No full Compose project authoring by users.
- No replicas. A Service has at most one active runtime placement globally.
- No manual node pinning or manual override in the first version.
- No custom HTTP/TCP health probe system. Users should use Docker healthcheck.
- No secrets system in this redesign.
- No `.env` generation.
- No Docker Engine API requirement. The runtime adapter uses remote command execution and Docker/Compose CLI.

## Core Principles

- **Service is the only user-created application resource.**
- **Service name is stable runtime identity.** It is the Compose service key, dependency reference, remote path component, and primary UI name.
- **Docker labels are Docker labels.** The word label only means Compose/Docker runtime labels.
- **Claims are derived port occupation facts.** They are not manually managed scheduling resources except for host-network port labels.
- **Generation is Service configuration generation.** It is not a deployment attempt number.
- **DB stores desired configuration. Docker stores runtime truth. Tasks store operation history.**
- **Repeated deploy of the same current version must be idempotent.**
- **Missing runtime is recoverable.** If a Service is enabled and runtime is missing, reconcile deploys the current generation again.
- **Every mutation is a task.** Deploy, enable, disable, restart, cleanup, unmanaged runtime actions, and capability probes all produce traceable tasks.

## Removed Old Product Surface

Implementation must remove the old model instead of hiding it behind new UI:

- Old backend service template and deployed service modules.
- Old `/api/v1/services` and `/api/v1/service-templates` business APIs.
- Old frontend Services and Service Templates pages/routes/navigation.
- Old tests that only validate removed concepts.
- Old DB schema creation for service templates and deployed services.

Reusable low-level helpers may be moved into new modules, but old business concepts must not remain as active abstractions.

## Resource Model

### Container Service

A Container Service contains:

- `id`
- immutable `name`
- `enabled`
- Compose service body YAML
- per-Service variables
- per-Service files
- selector map
- generation
- spec revision/hash
- last error summary
- timestamps

There is no `display_name` in the first version. The immutable `name` is also the UI display name.

Service `name` must use a safe Docker/Compose/container-name-compatible subset:

- lowercase letters, digits, and `-`
- starts with lowercase letter or digit
- ends with lowercase letter or digit
- length 1-32
- unique across Container Services
- immutable after creation

### Node

A node is an existing managed server that may run Container Services if it passes capability checks.

Built-in scheduling requirements:

- reachable
- supported OS
- Docker available
- Docker Compose available
- Docker Compose `include` supported
- not in maintenance
- no managed port-claim conflict

User-controlled scheduling input is a simple key/value selector over server fields and user traits. There is no node pinning and no scheduling DSL in the first version.

### Runtime Unit

Runtime units are observed from Docker/Compose. They are not the source of desired state.

Runtime status values should include:

- `missing`
- `starting`
- `running`
- `healthy`
- `unhealthy`
- `exited`
- `unknown`
- `stale`

Runtime identity is recognized through Docker labels.

### Task

A task is a durable DB queued operation. A task has steps, logs, trigger metadata, operation metadata, and retry/recovery state.

There is no complex parent/child/grandchild task tree in the first version. Related tasks share `operation_id`.

## Service Spec Model

### Compose Input

The user edits a Compose **service body**, not a full `docker-compose.yaml`.

Example user input:

```yaml
image: mysql:8
restart: unless-stopped
environment:
  MYSQL_DATABASE: "{{ .variables.DB_NAME }}"
volumes:
  - "{{ .service.data_dir }}/mysql:/var/lib/mysql"
ports:
  - "3306:3306"
```

The system wraps it as:

```yaml
services:
  <serviceName>:
    <user service body>
```

Strictly disallowed in the user service body:

- top-level Compose document keys such as `services`, `include`, `volumes`, `networks`
- `container_name`
- system-reserved `panel.*` labels, except host-network `panel.claims.ports`

Allowed:

- Compose service fields supported by Docker Compose
- `depends_on` short syntax and common long syntax
- normal user labels outside `panel.*`

Advanced Compose semantics such as `profiles`, `extends`, and user-authored `include` are not part of the user input model because users can only provide the value of one Compose service map entry.

### Visual Editor

YAML is the source of truth. Visual editing is an assistant for common fields and writes back to the same service body.

If YAML contains fields the visual editor cannot fully represent, UI may show an advanced-field marker, but the YAML remains valid if system validation passes.

### Variables

Variables are per-Service and use `map[string]string`.

Rules:

- Render with Go templates.
- Use `missingkey=error`.
- No `.env` file generation.
- No typed variables.
- No secrets in this redesign.

Template context:

```yaml
service:
  name: string
  generation: number
  current_dir: string
  data_dir: string
variables:
  <key>: string
```

### Files

Files are owned by a Service.

Rules:

- File path must be relative.
- Empty path is invalid.
- Absolute path is invalid.
- `..` is invalid.
- Windows drive paths are invalid.
- Files may be template or binary.
- File content/path changes increment generation.
- Files are rendered/copied into `{current_dir}/files/{relativePath}`.
- `data/` is never written through the files API.

Compose can reference files with:

```yaml
volumes:
  - "{{ .service.current_dir }}/files/nginx.conf:/etc/nginx/nginx.conf:ro"
```

### Volumes

The system does not hard-block arbitrary Compose volume/bind mounts.

It should warn on clearly dangerous sources such as:

- `/`
- `/var/run/docker.sock`
- Docker data root
- panel control directories
- obvious system-critical directories

Warnings do not block save or deploy. Docker/Compose remains responsible for runtime success or failure. The panel only protects its own managed files API and control directories.

## Labels And Claims

### System Labels

System labels are injected by the system through Compose override output.

Required keys:

```yaml
panel.managed: "true"
panel.service.id: "<id>"
panel.service.name: "<name>"
panel.service.spec_revision: "<revision>"
panel.service.generation: "<generation>"
panel.project: "panel_managed"
panel.node.id: "<nodeId>"
```

All values are strings.

The `panel.*` namespace is reserved. Users cannot write any `panel.*` label except `panel.claims.ports` in host network mode.

### Port Claims

Claims only mark host TCP port occupation.

Non-host network:

- claims are derived from Compose `ports`
- user cannot write `panel.claims.ports`

Host network:

- user must write `panel.claims.ports`
- empty string is allowed and means no fixed host ports are claimed

Accepted syntax:

```yaml
labels:
  panel.claims.ports: "80,443"
```

List syntax is also parsed:

```yaml
labels:
  - panel.claims.ports=80,443
```

Docs and UI should write map syntax.

First version is TCP only. UDP claims are not included.

Unmanaged Docker resources do not participate in scheduling conflicts. If an unmanaged container already occupies a port and Compose fails, the task should surface the Docker error clearly.

## Generation And Artifacts

Generation is the Service configuration generation.

Generation increments only when render-affecting Service content changes:

- Compose service body
- variables
- file path/content/kind
- `depends_on` because it changes rendered service definition

Generation does not increment for:

- enable/disable
- restart
- reconcile
- runtime refresh
- selector/scheduling constraint changes
- UI-only state
- last error/task status

`spec_revision`/hash is recomputed in the same DB transaction as generation increments. The same generation must not have multiple spec hashes.

Repeated reconcile of the same generation does not create a new generation. If runtime is missing or labels do not match, the system redeploys the current generation.

### Remote Layout

Global setting:

- `containerServiceRootDir`, default `/opt/panel/container-services`
- `containerServiceComposeProject`, default `panel_managed`
- `containerServiceGenerationRetention`, default `3`

Per node:

```text
/opt/panel/container-services/
  root.compose.yaml
  <serviceName>/
    current/
      compose.yaml
      panel.override.yaml
      manifest.json
      files/
    generations/
      1/
      2/
      3/
    data/
```

`data/` is the built-in persistent data layer shared by all generations and exposed as `.service.data_dir`.

The target node artifact is not a trusted source of truth. DB spec is the source of truth. Reconcile may delete and recreate the current generation artifact from DB spec.

Manifest is for troubleshooting, cleanup, and display. It is not used to decide whether the current version is already deployed.

Idempotent deploy decisions use Docker labels.

## Compose Root And Include

Each node has one root Compose file:

```text
{containerServiceRootDir}/root.compose.yaml
```

It includes current artifacts for enabled Services on that node.

Root project name is globally configurable and defaults to `panel_managed`.

Root compose maintenance:

- promote/write current artifact updates the include list
- disable removes Service from include
- delete removes Service from include
- move removes old node include and adds new node include
- before any Compose command, regenerate/validate root compose from DB/runtime artifact state

Docker Compose `include` support is mandatory. Capability check must use a real probe:

1. Create a temporary remote directory.
2. Write a minimal child compose file.
3. Write a root compose with `include`.
4. Run `docker compose -f root.yaml config`.
5. Record success/failure and stderr.
6. Clean the temporary directory.

Version string checks are not enough.

## Dependencies

Dependencies are read from Compose `depends_on` in the user service body.

Supported:

```yaml
depends_on:
  - mysql
  - redis
```

Supported:

```yaml
depends_on:
  mysql:
    condition: service_healthy
  redis:
    condition: service_started
```

The system reads only dependency Service names for validation, scheduling, previews, and graph operations. Runtime semantics such as `condition` are handled by Compose.

Validation rules:

- dependency name must exist as a Container Service
- no self dependency
- no dependency cycle
- dependency names must be valid Service names

If a Service is saved as disabled, dependencies may also be disabled or not deployed. Save still requires static dependency validity.

If a Service is saved/enabled as enabled, dependency enable behavior follows the normal enable preview and confirmation flow.

Deployment/reconcile rules:

- The dependency must be schedulable on the same target node.
- The dependency must have current artifact included/available on that node, otherwise Compose would not find the service definition.
- The system blocks static Compose-not-found situations before running Compose.
- The system does not check whether dependency runtime is running or healthy.
- If dependency runtime is not running, Compose handles it.
- The system does not secretly render/upload dependency artifacts inside the dependent's task.

If A can schedule to node1 but dependency B cannot schedule to node1, A's deploy task fails with a clear missing-dependency/scheduling reason. The user must adjust B.

## Scheduling

Scheduling happens during reconcile. Schedule preview is advisory and does not reserve or persist a node.

Inputs:

- Service selector map
- server fields
- user traits
- built-in requirements
- dependency co-scheduling requirements
- managed port claims

No resource threshold scheduler is required in the first version.

No manual node pinning is supported.

Stable preference:

1. prefer existing active node when still eligible
2. then stable sort by node name/id

Selector changes trigger reconcile/move behavior but do not increment generation.

## Save, Enable, Disable, Delete

### Save

Save performs static validation.

If `enabled=false`:

- save spec
- increment generation only if render-affecting content changed
- do not deploy

If `enabled=true`:

- save spec
- increment generation only if render-affecting content changed
- if dependencies require enabling, return enable-preview style impact and require user confirmation
- after confirmation or if no dependency enable is needed, enqueue reconcile

An enabled Service with spec changes must automatically create a reconcile task. Saving is not a manual draft/deploy split for enabled Services.

### Enable

Enable has preview and confirm phases.

Preview returns:

- target Service
- disabled direct/indirect dependencies that must be enabled
- dependency order
- expected tasks
- validation errors

Confirm:

- sets all affected Services `enabled=true`
- creates tasks using one `operation_id`
- reconciles in dependency-first order

The user does not choose a subset. Enabling A means enabling the dependency chain needed by A.

### Disable

Disable has preview and confirm phases.

Preview returns:

- target Service
- direct/indirect dependents that will also be disabled
- disable order
- expected runtime removal tasks

Confirm:

- disables dependents first
- disables dependency last
- sets each affected Service `enabled=false`
- removes runtime containers
- removes root include entries
- keeps spec, current artifact, generations, and data

Disable is propagation, not warning/blocking.

### Delete

Delete rules:

- Service must already be disabled.
- Service cannot be deleted while any other Service depends on it.
- Delete does not propagate.
- Delete does not rewrite other Services.
- Delete removes Service spec and managed artifacts according to implementation policy, but never removes unrelated unmanaged Docker resources.

## Reconcile And Idempotency

Reconcile uses the current DB spec and current generation.

Idempotency check:

- inspect runtime through Docker/Compose
- if container/service exists and system labels match current Service name, generation, and spec revision, task succeeds as no-op
- if runtime is missing, deploy current generation
- if labels mismatch, recreate/update
- if same-name unmanaged container exists, fail with a clear conflict

Do not trust remote artifact for idempotency.

Same current generation can be rendered and uploaded again. The system may clear the current generation artifact directory before writing, excluding `data/`.

Typical reconcile steps:

1. acquire Service lock
2. schedule
3. acquire Node lock
4. validate dependencies
5. inspect labels/idempotency
6. render current generation from DB spec
7. upload/write artifact
8. write current copy
9. refresh root compose
10. run `docker compose -p <project> -f root.compose.yaml up -d <serviceName>`
11. verify runtime
12. cleanup old generations
13. release locks

If Compose or verification fails, task fails and reports stderr/status. There is no hidden rollback requirement in the first version.

## Health Verification

After `docker compose up -d <serviceName>` succeeds, verify:

- `docker compose ps --format json <serviceName>` can find the service/container
- container is running
- if Docker healthcheck exists, wait until healthy
- unhealthy means failed
- starting waits until timeout
- no healthcheck means running passes

Global setting:

- `containerServiceHealthTimeoutSeconds`, default `60`

## Logs And Runtime Refresh

Task logs:

- command summary
- stdout
- stderr
- failure error

Container logs:

- not stored in DB
- fetched live when user opens logs
- default tail 200
- refresh supported
- first version does not require continuous streaming

Runtime cache:

- containers / Compose service state refreshed every 1 second
- interval is fixed and not configurable
- images / networks / volumes refreshed every 10th cycle, approximately every 10 seconds
- refresh failures mark cache stale/error
- desired state is not changed by refresh failures

## Task Model

Use durable DB queue tasks.

Required task fields:

- `id`
- `operation_id`
- `type`
- `resource_type`
- `resource_id`
- `node_id`
- `trigger_type`
- `trigger_resource_type`
- `trigger_resource_id`
- `trigger_task_id`
- `triggered_by`
- `status`
- `stage`
- `percentage`
- `summary`
- `error`
- retry fields
- timestamps

Trigger examples:

- `user`
- `scheduler`
- `service_enable`
- `service_disable`
- `retry`
- `runtime_explorer`

`task_steps` table:

- `id`
- `task_id`
- `step`
- `status`
- `percentage`
- `metadata_json`
- `started_at`
- `finished_at`
- `error`

Retry first version retries whole task. Step retry can be added later.

Worker recovery:

- running tasks are durable in DB
- worker can recover retryable tasks after restart
- stale locks can expire and be reclaimed

## Locks

Use DB lease locks.

Table:

```sql
operation_locks (
  scope TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  owner_task_id TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  heartbeat_at TEXT NOT NULL,
  PRIMARY KEY(scope, resource_id)
)
```

Scopes:

- `service`
- `node`

Rules:

- same Service cannot have concurrent active Container Services tasks
- same Node cannot concurrently modify root compose, Docker runtime, or generation cleanup
- enable/disable chains acquire Service locks in sorted order
- Node lock is acquired around node operations
- locks heartbeat and expire
- completed/failed/cancelled tasks release locks

## Runtime Explorer

Runtime Explorer is part of the new system, not a legacy Compose entry.

It shows:

- containers
- networks
- volumes
- images
- Docker/Compose capability
- managed/unmanaged marker

Managed runtime is identified by system Docker labels.

Managed resources:

- destructive stop/delete/remove actions are not allowed here
- UI says managed resources should be operated from Container Service
- restart is allowed and creates a `service_restart_runtime` task
- restart uses Service/Node locks
- restart does not change spec, generation, or enabled

Unmanaged resources:

- limited stop/delete/prune actions are allowed
- actions create tasks with `resource_type=docker_runtime`
- unmanaged resources are not used for scheduling conflict checks

## API Design

Use `/api/v1/container-services` as the Container Services namespace.

### Service APIs

- `GET /api/v1/container-services`
- `POST /api/v1/container-services`
- `GET /api/v1/container-services/{id}`
- `PUT /api/v1/container-services/{id}`
- `DELETE /api/v1/container-services/{id}`
- `POST /api/v1/container-services/{id}/validate`
- `POST /api/v1/container-services/{id}/render-preview`
- `POST /api/v1/container-services/{id}/schedule-preview`
- `POST /api/v1/container-services/{id}/reconcile`
- `POST /api/v1/container-services/{id}/restart`

### Enable / Disable APIs

- `POST /api/v1/container-services/{id}/enable-preview`
- `POST /api/v1/container-services/{id}/enable`
- `POST /api/v1/container-services/{id}/disable-preview`
- `POST /api/v1/container-services/{id}/disable`

Enable/disable confirm responses return created tasks and shared `operation_id`.

### File APIs

- `GET /api/v1/container-services/{id}/files`
- `POST /api/v1/container-services/{id}/files`
- `PUT /api/v1/container-services/{id}/files/{fileId}`
- `DELETE /api/v1/container-services/{id}/files/{fileId}`

### Runtime APIs

- `GET /api/v1/container-services/{id}/runtime`
- `GET /api/v1/container-services/{id}/logs?tail=200`
- `GET /api/v1/runtime-explorer/nodes/{nodeId}`
- `POST /api/v1/runtime-explorer/nodes/{nodeId}/containers/{containerId}/restart`
- `POST /api/v1/runtime-explorer/nodes/{nodeId}/containers/{containerId}/stop`
- `DELETE /api/v1/runtime-explorer/nodes/{nodeId}/containers/{containerId}`
- `POST /api/v1/runtime-explorer/nodes/{nodeId}/prune`

Runtime Explorer must reject destructive managed-resource operations except restart.

### Task APIs

- `GET /api/v1/tasks`
- `GET /api/v1/tasks/{id}`
- `GET /api/v1/tasks/{id}/steps`
- `GET /api/v1/tasks?operation_id=<id>`
- `POST /api/v1/tasks/{id}/retry`

## Data Model

### `container_services`

- `id`
- `name`
- `enabled`
- `compose_service_yaml`
- `variables_json`
- `selector_json`
- `generation`
- `spec_revision`
- `spec_hash`
- `last_error`
- `created_at`
- `updated_at`

`name` is unique and immutable.

### `container_service_files`

- `id`
- `service_id`
- `path`
- `kind`
- `content_type`
- `size`
- `sha256`
- `content` or storage reference
- `created_at`
- `updated_at`

### `container_runtime_cache`

- `id`
- `node_id`
- `service_id`
- `container_id`
- `name`
- `image`
- `state`
- `status`
- `health`
- `ports_json`
- `labels_json`
- `managed`
- `observed_at`
- `stale`
- `error`

### `tasks`

Use the task fields listed in Task Model.

### `task_steps`

Use the step fields listed in Task Model.

### `operation_locks`

Use the lock schema listed in Locks.

## Backend Modules

### `internal/containerservice`

Responsibilities:

- Service CRUD
- name validation
- service body validation
- dependency graph extraction
- generation/spec hash management
- enable/disable/delete orchestration
- render preview
- file validation and storage

### `internal/containerrender`

Responsibilities:

- Go template render
- service body wrapping
- system label injection
- compose/override artifact generation
- manifest generation
- file rendering/copy planning

### `internal/placement`

Responsibilities:

- build node attributes
- selector matching
- Docker/Compose/include capability filtering
- dependency co-scheduling validation
- managed port claim conflict detection
- stable node choice
- schedule preview reasoning

### `internal/runtime/docker`

Responsibilities:

- remote Docker/Compose CLI commands
- capability probes
- compose up/restart/down/rm
- `docker compose ps` parsing
- live logs
- runtime cache refresh
- managed/unmanaged discovery through labels

### `internal/containerops`

Responsibilities:

- DB queue workers for Container Services tasks
- reconcile execution
- enable/disable chain execution
- cleanup generation execution
- lock acquisition and heartbeat
- worker recovery

### `internal/tasks`

Responsibilities:

- durable task creation
- task steps
- logs
- retry metadata
- operation grouping
- task APIs

## Frontend Information Architecture

Navigation:

- Overview: Container Services summary.
- Container Services: primary Service workspace.
- Runtime Explorer: Docker observation and unmanaged limited actions.
- Task Center: operation and task timeline.
- Servers/Nodes: node details, traits, Docker capability.

### Container Services Page

Main page should be operational, not a landing page.

It needs:

- Service list
- enabled state
- runtime state
- node
- generation/spec status from runtime labels
- last task
- last error
- quick actions: edit, reconcile, restart, enable, disable, delete when allowed

### Service Editor

Sections:

- name on create only
- enabled toggle/save behavior
- YAML editor for Compose service body
- visual editor for common fields
- variables
- files
- selector
- validation results
- render preview
- schedule preview
- dependency impact preview

Saving an enabled Service with changes should show the created reconcile task. If dependencies need enabling, UI shows the enable impact confirmation.

### Service Detail

Show:

- current DB generation/spec hash
- runtime observed labels/generation/hash
- enabled state
- runtime status
- node
- dependency graph
- live logs
- recent tasks
- actions

### Task Center

Show:

- task list grouped by `operation_id`
- trigger information
- resource and node
- status/stage/percentage
- task steps timeline
- stdout/stderr logs
- retry whole task

### Runtime Explorer

Show Docker resources with managed/unmanaged marker. Managed destructive actions are replaced with a message and link to the owning Service; restart is allowed.

## Strong Constraints

- No old template/deployed-service compatibility.
- Users only provide one Compose service map value.
- `name` is immutable and no `display_name`.
- `container_name` is forbidden.
- `panel.*` labels are forbidden except host `panel.claims.ports`.
- Host network must explicitly provide `panel.claims.ports`, empty string allowed.
- Non-host network must not provide `panel.claims.ports`.
- Dependencies must reference existing Service names.
- Dependency cycles are invalid.
- Delete requires disabled state and no dependents.
- Disable propagates to dependents after preview/confirmation.
- Enable propagates to dependencies after preview/confirmation.
- Same Service active Container Services task is locked.
- Same Node runtime/root-compose mutation is locked.
- Docker Compose `include` support is mandatory and probed by execution.
- Remote artifact is not trusted for deployment idempotency.
- Runtime idempotency is based on Docker labels.
- `missing` runtime is recoverable by reconcile.

## Verification Plan

Backend tests:

- Service name validation
- service body parser rejects full compose and `container_name`
- label namespace validation
- host/non-host claims validation
- port extraction from `ports`
- dependency extraction short/long syntax
- dependency missing/self/cycle validation
- generation increments only on render-affecting changes
- selector changes do not increment generation
- save enabled creates reconcile task
- save disabled does not deploy
- enable-preview dependency ordering
- disable-preview dependent propagation
- delete blocked by dependents
- schedule rejects unsatisfied dependency co-location
- include capability probe command handling
- idempotency by Docker labels
- missing runtime reconcile path
- unmanaged same-name conflict
- task steps persistence
- service/node lock lease behavior
- runtime refresh cache intervals

Frontend tests:

- Service list status rendering
- editor validation display
- YAML/visual roundtrip for common fields
- dependency impact confirmation
- save enabled task display
- disable propagation preview
- runtime labels/generation mismatch display
- managed/unmanaged Runtime Explorer actions
- task operation grouping and steps timeline

Manual verification:

- create disabled Service
- enable Service and deploy
- update enabled Service and observe automatic reconcile
- repeat reconcile current generation and observe no-op when labels match
- delete runtime manually and reconcile missing state
- create host-network Service with empty port claims
- create dependency chain and enable top Service
- disable a base Service and confirm dependent propagation
- attempt delete while depended on and see block
- attempt deploy with same-name unmanaged container and see conflict
- inspect live logs from Service detail
