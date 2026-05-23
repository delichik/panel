# Container Services Redesign

## Goal

Redesign Docker management as **Container Services**, a lightweight single-instance service control plane. This is a full replacement of the current service template / deployed service model, not an incremental compatibility layer.

The system remains a server panel. Overview should include Container Services health summaries, while the Container Services module becomes the primary service-management workspace.

## Core principles

- **Service is the only user-created application resource.** It is a declarative application definition, similar to a single-instance Deployment.
- **No replicas.** A Service can have exactly one active instance globally.
- **Node selection is automatic.** Users define selectors, constraints, conflicts, and preferences; the system chooses the best Node.
- **All deployment and runtime actions are tasks.** Every create, update, move, restart, stop, cleanup, and verification step appears in Task Center.
- **Visual + YAML editing stays.** The current dual-mode editing model is valuable and should become the Service spec editor.
- **UI can be redesigned freely.** Existing pages do not constrain the new Container Services experience.

## Resource model

### Service

The top-level user resource. It contains:

- name and description
- enabled state
- selector
- constraints
- anti-affinity / conflict rules
- placement preferences
- visual model
- compose YAML
- variables
- attached files
- data cleanup policy
- update policy

Users create, edit, enable, disable, and reconcile Services. Users do not create templates or per-node deployed services.

### Node

A Container Services view of an existing server.

Scheduling uses a unified node attributes object. It should include server fields and traits in one queryable model, such as:

- name
- host / IP
- OS information
- Docker / Compose capability
- resource summary
- maintenance state
- user-defined traits

Selectors and constraints operate on this unified attributes model. Server name and IP are therefore usable like traits.

### Service Instance

The current instance of a Service. Each Service has at most one instance.

Important fields:

- desired node
- current node
- active container reference
- previous container reference
- desired generation
- running generation
- status
- last reconcile task
- last error

A Service Instance is not a replica set. It represents the single active runtime placement for the Service.

### Container Runtime Unit

A runtime observation object from Docker / Compose. New and old containers may briefly coexist during updates, but only one container becomes active when reconciliation completes.

Runtime units support observability and operations:

- state and status
- image
- ports
- labels
- logs
- managed/unmanaged marker
- restart / stop / replace actions through tasks

## Scheduling model

Scheduling picks one Node for a Service.

Inputs:

- **selector:** hard filter over node attributes.
- **constraints:** hard requirements such as Docker capability, resource thresholds, or maintenance state.
- **anti-affinity / conflicts:** rules such as not colocating with specific Services, avoiding port conflicts, or avoiding path conflicts.
- **placement preferences:** soft scoring rules such as low load first, preferred region, or existing image cache.

Outputs:

- selected Node
- rejected Nodes with reasons
- score breakdown for candidates
- required action: create, update, move, replace, remove, or no-op
- task graph

A schedule preview API should expose the same reasoning before saving or reconciling.

## Update model

All Service updates, config changes, image changes, and node moves use the same fixed sequence:

1. **Create new container**
   - render compose/spec/files
   - prepare target Node directory
   - create the new runtime container
2. **Stop old container**
   - gracefully stop the current active container if one exists
3. **Start new container**
   - start the new runtime unit
   - verify health, ports, and compose status
4. **Delete old container**
   - remove the old runtime unit and temporary files
   - do not delete persistent volumes/data unless the Service data policy explicitly allows it

Failure rules:

- If creating the new container fails, leave the old container untouched.
- If stopping the old container fails, do not start the new container.
- If starting the new container fails, try to restore the old container if possible and mark the task failed or blocked.
- If deleting the old container fails after the new one is active, keep the new container active and create or mark cleanup work.

## Task model

Task Center needs parent/child tasks.

Parent task:

- `service_reconcile`
- bound to `resourceType=service` and `resourceId=serviceId`
- represents one desired-state reconciliation

Child tasks:

- `service_schedule`
- `service_create_container`
- `service_stop_old_container`
- `service_start_new_container`
- `service_delete_old_container`
- `service_verify`
- `service_remove`
- cleanup tasks when needed

Task Center should support:

- parent task expansion
- service filter
- node filter
- status filter
- failed-only filter
- real-time logs per task
- retrying failed or retryable steps
- jumping from a Service detail page to the active task graph

## Data model

### `container_services`

Stores Service desired state.

Suggested fields:

- `id`
- `name`
- `description`
- `enabled`
- `selector_json`
- `constraints_json`
- `anti_affinity_json`
- `placement_preference_json`
- `compose_yaml`
- `visual_state_json`
- `variables_json`
- `files_manifest_json`
- `data_policy_json`
- `update_policy_json`
- `spec_hash`
- `generation`
- `created_at`
- `updated_at`

Rules:

- Increment `generation` on spec changes.
- Recalculate `spec_hash` on spec changes.
- If saved with `enabled=true`, enqueue a `service_reconcile` task.

### `container_service_files`

Stores Service-owned files.

Suggested fields:

- `id`
- `service_id`
- `path`
- `kind`: `template` or `binary`
- `content_type`
- `size`
- `sha256`
- `created_at`
- `updated_at`

Files belong directly to Services.

### `container_service_instances`

Stores the single current Service instance.

Suggested fields:

- `id`
- `service_id`
- `desired_node_id`
- `current_node_id`
- `active_container_ref`
- `previous_container_ref`
- `desired_generation`
- `running_generation`
- `status`
- `last_reconcile_task_id`
- `last_error`
- `created_at`
- `updated_at`

Constraint:

- `UNIQUE(service_id)`

### `container_runtime_units`

Stores runtime observations.

Suggested fields:

- `id`
- `service_id`
- `instance_id`
- `node_id`
- `runtime_container_id`
- `name`
- `image`
- `state`
- `status`
- `ports_json`
- `labels_json`
- `is_active`
- `created_at`
- `observed_at`

### Tasks

The task schema should support:

- `parent_id`
- `resource_type`
- `resource_id`
- `node_id`
- `step`
- `metadata_json`
- `percentage`

The Task Center concept remains, but the schema and API should be rebuilt around operation trees.

## API model

Use `/api/container-services` as the unified API namespace.

### Service APIs

- `GET /container-services`
- `POST /container-services`
- `GET /container-services/{id}`
- `PUT /container-services/{id}`
- `DELETE /container-services/{id}`
- `POST /container-services/{id}/enable`
- `POST /container-services/{id}/disable`
- `POST /container-services/{id}/reconcile`
- `POST /container-services/{id}/render-preview`
- `POST /container-services/{id}/validate`
- `POST /container-services/{id}/schedule-preview`
- `GET /container-services/{id}/instances`
- `GET /container-services/{id}/runtime-units`
- `GET /container-services/{id}/tasks`

### File APIs

- `GET /container-services/{id}/files`
- `POST /container-services/{id}/files/template`
- `POST /container-services/{id}/files/binary`
- `PUT /container-services/{id}/files/{fileId}`
- `DELETE /container-services/{id}/files/{fileId}`

### Node and scheduling APIs

- `GET /container-services/nodes`
- `GET /container-services/nodes/{nodeId}/services`
- `POST /container-services/{id}/schedule-preview`

### Runtime action APIs

All runtime actions create tasks:

- `POST /container-services/{id}/restart`
- `POST /container-services/{id}/stop`
- `POST /container-services/{id}/replace`
- `POST /container-services/{id}/remove-runtime`

## Backend modules

### `internal/containerservice`

The main control-plane module.

Responsibilities:

- Service CRUD
- spec validation
- visual/YAML render
- Service files
- spec hash and generation
- instance state
- reconcile request creation

This replaces the current compose template/deployed-service business model.

### `internal/placement`

The scheduler/placement module.

Responsibilities:

- build node attributes
- selector matching
- constraints filtering
- anti-affinity and conflict checks
- placement scoring
- schedule preview output

### `internal/runtime/docker`

The Docker runtime adapter.

Responsibilities:

- Docker and Compose capability detection
- container/compose operations
- runtime discovery
- start/stop/delete/pull/log/status operations
- runtime unit cache refresh

### `internal/operations`

Executes task graphs.

Responsibilities:

- create container
- stop old container
- start new container
- delete old container
- verify runtime state
- remove runtime
- cleanup failed leftovers

Business modules create operation graphs; this module performs the steps and writes task logs.

### `internal/tasks`

Task Center backend.

Responsibilities:

- parent/child task trees
- resource binding
- node binding
- step metadata
- retry state
- real-time logs

## UI design

### Navigation

- **Overview** includes Container Services summary cards.
- **Container Services** is the Service-first main workspace.
- **Task Center** shows all operation tasks.
- **Servers / Nodes** keeps server management and adds service/runtime context.

### Container Services page

Main dashboard sections:

- health summary: total, running, updating, failed, disabled, blocked
- Service list with enabled state, selected Node, active container, generation, status, last task, last error
- quick actions: Edit, Reconcile, Restart, Disable
- detail drawer or detail page with runtime and task context

### Service editor

The editor keeps Visual + YAML mode and becomes the Service Spec Editor.

Sections:

- basic information
- enabled state
- visual service model
- YAML compose model
- variables
- files
- selector builder
- constraints
- anti-affinity / conflicts
- placement preferences
- data policy
- update policy

A fixed preview area should show:

- render preview
- schedule preview
- selected Node
- candidate Nodes
- rejection reasons
- score breakdown
- pending impact
- task graph that will be created

Saving an enabled Service immediately creates a reconcile task. The UI should show task progress instead of asking the user whether to deploy.

### Service detail

Show desired state against current state:

- desired spec summary
- selected Node
- current instance
- active container
- previous container during switching/updating
- current task graph
- runtime logs
- recent events
- actions: Reconcile now, Restart, Replace, Disable, Remove runtime

### Task Center

Upgrade the flat task table into an operation timeline:

- parent/child task tree
- service and node columns
- step progress
- logs per child task
- retry failed step
- filters for service, node, type, status, failed-only

### Runtime Explorer

The old Docker runtime pages can become a Runtime Explorer:

- containers
- networks
- volumes
- images
- managed/unmanaged markers
- links back to owning Service for managed runtime units

Runtime Explorer is for observation and troubleshooting, not the main creation workflow.

## Automation rules

### Save Service

1. Save spec.
2. Increment generation if desired state changed.
3. Calculate spec hash.
4. If enabled, create `service_reconcile` task graph.

### Reconcile Service

1. Read desired Service spec.
2. Build node attributes.
3. Filter by selector and constraints.
4. Apply anti-affinity and conflicts.
5. Score candidates by placement preferences.
6. Choose one Node.
7. Compare desired state with current instance.
8. Create, update, move, replace, remove, or no-op.
9. Execute through task children.
10. Update instance and runtime unit state.

### Intelligent guidance

The system should explain decisions:

- why a Node was selected
- why Nodes were rejected
- which constraint failed
- which port/path/service caused a conflict
- whether a failure is retryable
- what action the user can take next

Capability problems, port conflicts, and resource shortages should become visible task or schedule-preview information instead of hidden errors.

## Error handling

- No schedulable Node: instance and parent task become `blocked`, with rejected reasons.
- New container creation failure: old container remains active.
- Old container stop failure: new container is not started.
- New container start failure: attempt to restore old container if possible.
- Old container deletion failure after successful switch: new container remains active, cleanup is tracked.
- Disabled Service: runtime is removed according to policy, Service spec remains.

## Verification plan

### Backend tests

- Service CRUD
- visual/YAML roundtrip
- file CRUD
- selector matching
- node attributes construction
- constraints filtering
- anti-affinity conflict detection
- placement scoring
- schedule preview rejection reasons
- generation and spec hash drift detection
- task graph creation
- fixed update sequence order
- failure recovery behavior
- parent/child task APIs
- Docker runtime adapter commands

### Frontend tests

- Service list status rendering
- Visual/YAML editor behavior
- schedule preview rendering
- save triggers reconcile task display
- task tree rendering
- failed task retry UI
- Service desired/current diff
- Runtime Explorer managed/unmanaged markers

### Manual verification

- Creating an enabled Service automatically deploys it.
- Updating image/config uses create new → stop old → start new → delete old.
- Selector changes move the Service to a new Node.
- Port conflicts block scheduling and show the conflict reason.
- Disabling a Service removes runtime but keeps the spec.
- All deployment and runtime actions appear in Task Center.
