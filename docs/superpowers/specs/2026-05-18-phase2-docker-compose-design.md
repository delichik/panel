# Phase 2 Docker Compose Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

Phase 2 adds Docker Compose-backed service management while preserving the Phase 1 SSH-only control model. The implementation is split into read-only runtime discovery first, then mutating service template and deployed service management. This reduces risk because Docker command parsing, remote environment differences, and UI models can stabilize before deployment actions are introduced.

## Phase Goal

Enable operators to inspect Docker status, manage reusable `service_template` definitions, create and operate deployed `service` instances, inspect Docker networks/volumes/images, clean up unused Docker resources, and migrate managed templates/services between servers.

Phase 2 is complete only when the operator loop closes through the real UI and API:

1. Select a managed server.
2. Confirm Docker and Compose capability.
3. Inspect runtime services, networks, volumes, and images.
4. Create or edit a `service_template`.
5. Add template-attached files, including binary files and text template files.
6. Configure template variables from system variables and per-server custom variables.
7. Create or sync deployed services from the template through a task-backed workflow.
8. Verify runtime status, update prompts, and task logs.
9. Export the template/service bundle and import it onto another capable server.

Read-only discovery can ship before write operations, but it must still close its own loop: capability refresh, cached status, runtime service/network/volume/image lists, status detail, unsupported state, and refresh failure handling.

## Scope

In scope:

- Docker CLI and Docker Compose CLI detection over SSH.
- Read-only runtime service, network, volume, and image views.
- Panel-managed `service_template` metadata and deployed `service` metadata.
- Template-attached binary/static file management.
- Template-attached text resources rendered through Go templates.
- Visual service template editing and YAML editing.
- Per-server user-defined variables plus system variables for template rendering.
- Label-based association between templates and deployed services.
- Docker image update detection and selected/all update workflows.
- Network, volume, and image deletion, including unused-resource cleanup.
- Compose deployment workflows through SSH and `docker compose`.
- One-click migration bundles for template metadata, service metadata, labels, variables, and attached files.

Out of scope:

- Docker Engine API integration.
- Non-Compose container orchestration.
- Registry credential management beyond what is available on the target server.
- Automatic certificate issuance; Phase 2 only stores certificate references if Phase 4 later provides them.
- Network, volume, and image create/update UI. Phase 2 only lists, deletes, and prunes unused Docker resources.

## Product Objects

`service_template`:

- A reusable definition for one or more Docker Compose services.
- Stores metadata, Compose YAML, visual editor state, variables, attached files, and template files.
- Supports visual configuration and direct YAML editing. Both modes must round-trip through the same validation path.
- Owns template-attached files:
  - Binary/static files are stored as ordinary files and uploaded unchanged.
  - Text template files are rendered with Go templates before deployment.
- Has a stable template ID used in Docker labels.

`service`:

- A deployed instance created from a `service_template` on a specific server.
- Represents already-created containers and their Compose-backed runtime state.
- Stores the linked template ID, server ID, rendered values, last applied template version, last deploy task, and runtime labels.
- Can be started, stopped, restarted, removed, updated, and synced from the template.

`network`, `volume`, and `image`:

- Read from the selected server through Docker CLI.
- Display runtime metadata and labels.
- Support delete and delete-unused/prune workflows only.
- Do not support create or edit in Phase 2.

`system variables`:

- Built-in values supplied by the panel, such as server ID, server name, service ID, service name, template ID, deployment paths, and timestamps.

`server custom variables`:

- User-defined key/value pairs stored on each managed server.
- Used during template rendering together with system variables and service-specific values.
- Missing variables are hard validation errors. Rendering must not silently substitute empty strings.

## Label Contract

Every panel-created Compose runtime project, service container, network, volume, and image reference where labels are supported must include panel labels:

- `panel.managed=true`
- `panel.service_template_id=<template-id>`
- `panel.service_template_version=<version>`
- `panel.service_id=<service-id>`
- `panel.server_id=<server-id>`

The runtime discovery layer uses labels to associate existing containers with panel metadata. When a `service_template` changes, the sync planner finds linked services by metadata and labels, computes drift, and offers a task-backed sync. Runtime resources without matching labels are shown as unmanaged and must not be mutated by template sync.

## Implementation Split

### Phase 2A: Docker Runtime Discovery

Purpose:

- Add Docker visibility without remote mutation.

Backend requirements:

- Detect `docker` and `docker compose` availability per server.
- Read Docker version, Compose version, and basic capability state.
- List runtime services/containers and Compose-backed project names where needed for status lookup.
- List Docker networks, volumes, and images.
- Detect image update availability where the remote Docker CLI can compare the running image with the latest pullable image.
- Read service/container status.
- Store capability/status cache in the application database.

Frontend requirements:

- Docker availability indicators on server detail and overview.
- Docker page with server selector and tabs/lists for services, networks, volumes, and images.
- Unsupported and not-installed states.
- Image update indicators and selected/all update actions.

Acceptance checks:

- Servers without Docker are shown as unsupported for Docker features.
- Servers with Docker show service/container, network, volume, and image state.
- Runtime refresh failures do not corrupt the last known cache.
- Image update checks behave like package updates: show available updates, allow selected update, and allow update all.

Closed-loop workflow:

- From the server detail screen, the operator can run Docker refresh.
- The refresh records capability fields: Docker installed, Docker version, Compose installed, Compose version, checked time, and last error.
- The Docker page uses the same server selector pattern as Phase 1 pages.
- Selecting a capable server lists runtime services/containers, networks, volumes, and images discovered from the server.
- Selecting a runtime service reads service/container status on demand.
- Network, volume, and image lists expose delete and delete-unused/prune actions through tasks.
- Image update checks populate an updateable image list without pulling or recreating containers until the operator chooses selected or all updates.
- If refresh fails, the UI shows the failure and keeps any previously successful cache marked as stale.

2A must not create or mutate remote services, Compose runtime projects, networks, volumes, or images. Any UI control that implies mutation belongs to 2B.

### Phase 2B: Service Template and Service Management

Purpose:

- Manage panel-owned `service_template` definitions, deployed `service` instances, deployment artifacts, sync, update, and migration.

Backend requirements:

- `service_template` CRUD.
- Deployed `service` CRUD and lifecycle operations.
- Visual template editor and YAML editor backed by one canonical Compose model.
- Binary/static files stored under `data/compose/<server-id>/<service>/static/` when service-specific and under `data/service_templates/<template-id>/static/` when template-attached.
- Text template files stored under `data/service_templates/<template-id>/templates/`.
- Rendered outputs stored under `data/compose/<server-id>/<service>/rendered/`.
- Server custom variables stored with server metadata and merged with system variables for rendering.
- Local Go template validation and rendering before upload.
- Missing template variables fail validation and block deployment or sync.
- Staged remote upload before activation.
- Task-backed deploy, sync, pull, update selected images, update all images, up, restart, stop, remove, migration export, and migration import.

Frontend requirements:

- Service Templates page.
- Services page for deployed containers/services.
- Template detail page with visual editor, YAML editor, attached files, template files, render preview, linked services, sync status, and task logs.
- Service detail page with runtime status, rendered config, linked template, variables, update prompts, lifecycle actions, and task logs.
- Networks, Volumes, and Images pages or tabs with display, delete, and delete-unused actions.
- Migration export/import UI.

Acceptance checks:

- Service deploy uploads Compose config and resources to predictable remote paths.
- Template render errors block deployment before any remote mutation.
- Deployment tasks show command stages and logs.
- Template edits mark linked services as drifted and offer task-backed sync.
- Migration export/import preserves template metadata, service metadata, binary files, template files, render values, labels, and certificate references.

Closed-loop workflow:

- The operator creates a `service_template` using the visual editor or YAML editor.
- The operator attaches binary files and text template files to the template.
- The operator defines required variables and chooses values from system variables, server custom variables, and service-specific values.
- The operator creates a deployed `service` from a template by choosing server, service name, remote base path, and render values.
- The service detail page shows configuration, resources, templates, render preview, deployment actions, last deployment task, linked template version, drift/sync state, update prompts, and current runtime status.
- Static resources are uploaded into the local data root first, then copied to the remote staging path during deployment.
- Template resources are validated and rendered locally before any remote upload.
- Deployment creates a task, logs validation, rendering, staging upload, activation, `docker compose pull`, `docker compose up`, and final status refresh.
- Template changes create a new template version and mark linked services as needing sync.
- Sync uses labels and metadata to target only linked services and writes the new template version after a successful task.
- Stop, restart, remove, image update, migration export, and migration import use the same task/log model.
- Migration import validates the target server capability, template/service name collision, remote path safety, label compatibility, variables, and bundle version before writing local metadata or remote files.

The UI cannot present a service as deployed or synced until the task finishes and the runtime status refresh succeeds or reports a clear post-deploy failure.

## Design Closure Rules

Phase 2 work must keep four layers aligned:

- Requirements: every capability in this document has an operator-facing workflow and a failure state.
- Interfaces: every workflow maps to a stable API endpoint or shared task endpoint.
- Implementation: remote Docker/Compose behavior stays behind `internal/docker`; panel-owned template/service metadata and filesystem artifacts stay behind `internal/compose`.
- Verification: every workflow has a manual acceptance step in `docs/phase2-acceptance-gate.md` and at least one focused backend or frontend test where practical.

Do not mark a milestone complete when only one layer exists. Examples that are not complete:

- Backend API exists but no UI calls it.
- UI page renders sample data or local-only state.
- Deployment command runs but does not emit task stages and logs.
- Migration exports metadata but omits files, template files, render values, labels, or certificate references.
- Docker status refresh overwrites the last known successful state with an empty failure result.
- Template edits are saved but linked services are not marked drifted.

## Data and State Closure

Application database owns metadata:

- Docker capability cache per server.
- Runtime project cache summaries, when persisted.
- Panel-owned service templates.
- Deployed service metadata.
- Resource manifests and template manifests.
- Last deployment task and last observed runtime status references.
- Migration bundle metadata when retained.
- Image update cache and last checked timestamps.
- Server custom variables.

Filesystem data root owns artifacts:

- `data/service_templates/<template-id>/static/`
- `data/service_templates/<template-id>/templates/`
- `data/compose/<server-id>/<service>/static/`
- `data/compose/<server-id>/<service>/rendered/`
- `data/compose/<server-id>/<service>/bundles/`

Remote servers own deployed runtime files:

- Active project directory under the configured remote base path.
- Staging directory under the same remote base path or a panel-owned staging child.
- Runtime containers, networks, volumes, and images managed by Docker Compose and associated through labels where possible.

Deletion rules must be explicit. Deleting template or service metadata must not silently remove remote containers, networks, volumes, images, or files. Remote removal and resource pruning must be separate task-backed actions with confirmation in the UI.

## Failure and Recovery Closure

Every task-backed workflow must leave the operator with a next action:

- Validation failure: show the invalid field or resource and perform no remote mutation.
- Missing variable failure: show the template file, variable name, and value source that failed resolution.
- Staging upload failure: leave active remote files untouched and log the failed path.
- Activation failure: log whether active files were changed and whether manual cleanup is required.
- Compose command failure: preserve task logs and refresh runtime status if possible.
- Image update failure: keep the previous container state visible, show which image failed, and avoid marking the service as updated.
- Resource delete/prune failure: show the resource ID/name and whether Docker reported it as in use.
- Migration import failure: do not create partial local metadata unless the bundle has passed validation; log cleanup requirements if remote staging was touched.

Automatic rollback is required only where the implementation can prove the previous active path is intact. Otherwise the task log must say that rollback was not attempted and why.

## UI Closure

Phase 2 UI screens must expose the same state machine as the backend:

- Not selected: no server or project selected.
- Unsupported: selected server cannot run Docker/Compose workflows.
- Ready: capability exists and current action is available.
- Validating: local project, resource, or template checks are running.
- Running: a task-backed operation is active.
- Failed: the last action failed and the user can open logs or validation details.
- Stale: cached Docker/runtime data exists, but the latest refresh failed or is older than the configured threshold.

The Docker runtime page, template detail page, and service detail page should reuse Phase 1 patterns for server selection, task log panels, loading states, and error banners. Phase 2 should add controls only when the corresponding real API exists.

Primary navigation for Phase 2:

- Service Templates: reusable definitions, visual/YAML editing, attached files, template files, variables, linked services, and sync prompts.
- Services: deployed instances, runtime status, lifecycle actions, linked template, drift state, image update prompts, and task logs.
- Networks: list, delete, and delete unused.
- Volumes: list, delete, and delete unused.
- Images: list, delete, delete unused, check updates, selected update, and update all.

## Backend Modules

`internal/docker`:

- Owns Docker CLI detection, runtime status, networks, volumes, images, pruning, and image update checks.
- Exposes `ContainerRuntime`.
- Calls `RemoteExecutor`.

`internal/compose`:

- Owns service template metadata, deployed service metadata, labels, resources, deployment, sync, update, and migration.
- Calls `ContainerRuntime`, `TemplateRenderer`, `TaskRunner`, and `RemoteExecutor`.

`internal/templatex`:

- Owns Go template validation and rendering.
- Dynamic resources are text-only.

`internal/tasks`:

- Records every mutating Docker/Compose workflow.

`internal/scheduler`:

- Refreshes Docker capability/status cache when enabled.

## Interfaces

```go
type ContainerRuntime interface {
    Detect(ctx context.Context, exec RemoteExecutor, target Target) (DockerCapability, error)
    ListComposeProjects(ctx context.Context, exec RemoteExecutor, target Target) ([]ComposeRuntimeProject, error)
    ListServices(ctx context.Context, exec RemoteExecutor, target Target) ([]RuntimeService, error)
    ListNetworks(ctx context.Context, exec RemoteExecutor, target Target) ([]RuntimeNetwork, error)
    ListVolumes(ctx context.Context, exec RemoteExecutor, target Target) ([]RuntimeVolume, error)
    ListImages(ctx context.Context, exec RemoteExecutor, target Target) ([]RuntimeImage, error)
    CheckImageUpdates(ctx context.Context, exec RemoteExecutor, target Target, images []RuntimeImage, log LogSink) ([]ImageUpdate, error)
    ReadComposeStatus(ctx context.Context, exec RemoteExecutor, target Target, project string) (ComposeStatus, error)
    DeleteNetwork(ctx context.Context, exec RemoteExecutor, target Target, networkID string, log LogSink) error
    DeleteVolume(ctx context.Context, exec RemoteExecutor, target Target, volumeID string, log LogSink) error
    DeleteImage(ctx context.Context, exec RemoteExecutor, target Target, imageID string, log LogSink) error
    PruneNetworks(ctx context.Context, exec RemoteExecutor, target Target, log LogSink) error
    PruneVolumes(ctx context.Context, exec RemoteExecutor, target Target, log LogSink) error
    PruneImages(ctx context.Context, exec RemoteExecutor, target Target, log LogSink) error
    Pull(ctx context.Context, exec RemoteExecutor, target Target, project ComposeDeployment, log LogSink) error
    Up(ctx context.Context, exec RemoteExecutor, target Target, project ComposeDeployment, log LogSink) error
    Restart(ctx context.Context, exec RemoteExecutor, target Target, project ComposeDeployment, services []string, log LogSink) error
    Down(ctx context.Context, exec RemoteExecutor, target Target, project ComposeDeployment, log LogSink) error
}

type TemplateRenderer interface {
    Validate(ctx context.Context, source string, variables map[string]TemplateVariable) error
    Render(ctx context.Context, source string, values map[string]any) (string, error)
}
```

## API Groups

Docker runtime:

- `GET /api/v1/servers/{serverId}/docker/capability`
- `POST /api/v1/servers/{serverId}/docker/refresh`
- `GET /api/v1/servers/{serverId}/docker/projects`
- `GET /api/v1/servers/{serverId}/docker/projects/{projectName}/status`
- `GET /api/v1/servers/{serverId}/docker/services`
- `GET /api/v1/servers/{serverId}/docker/networks`
- `DELETE /api/v1/servers/{serverId}/docker/networks/{networkId}`
- `POST /api/v1/servers/{serverId}/docker/networks/prune`
- `GET /api/v1/servers/{serverId}/docker/volumes`
- `DELETE /api/v1/servers/{serverId}/docker/volumes/{volumeId}`
- `POST /api/v1/servers/{serverId}/docker/volumes/prune`
- `GET /api/v1/servers/{serverId}/docker/images`
- `POST /api/v1/servers/{serverId}/docker/images/check-updates`
- `POST /api/v1/servers/{serverId}/docker/images/update-selected`
- `POST /api/v1/servers/{serverId}/docker/images/update-all`
- `DELETE /api/v1/servers/{serverId}/docker/images/{imageId}`
- `POST /api/v1/servers/{serverId}/docker/images/prune`

Service templates and services:

- `GET /api/v1/service-templates`
- `POST /api/v1/service-templates`
- `GET /api/v1/service-templates/{templateId}`
- `PUT /api/v1/service-templates/{templateId}`
- `DELETE /api/v1/service-templates/{templateId}`
- `POST /api/v1/service-templates/{templateId}/validate`
- `POST /api/v1/service-templates/{templateId}/render-preview`
- `GET /api/v1/service-templates/{templateId}/services`
- `GET /api/v1/services`
- `POST /api/v1/services`
- `GET /api/v1/services/{serviceId}`
- `PUT /api/v1/services/{serviceId}`
- `DELETE /api/v1/services/{serviceId}`
- `POST /api/v1/services/{serviceId}/render`
- `POST /api/v1/services/{serviceId}/deploy`
- `POST /api/v1/services/{serviceId}/sync`
- `POST /api/v1/services/{serviceId}/restart`
- `POST /api/v1/services/{serviceId}/stop`
- `POST /api/v1/services/{serviceId}/remove`
- `POST /api/v1/services/{serviceId}/update-images`

Resources:

- `GET /api/v1/service-templates/{templateId}/files`
- `POST /api/v1/service-templates/{templateId}/files/binary`
- `POST /api/v1/service-templates/{templateId}/files/template`
- `PUT /api/v1/service-templates/{templateId}/files/{fileId}`
- `DELETE /api/v1/service-templates/{templateId}/files/{fileId}`
- `GET /api/v1/servers/{serverId}/variables`
- `PUT /api/v1/servers/{serverId}/variables`

Migration:

- `POST /api/v1/services/{serviceId}/migration/export`
- `POST /api/v1/service-templates/{templateId}/migration/export`
- `POST /api/v1/compose/migration/import`

## Difficulty and Risk Notes

High-risk areas:

- Remote path handling.
- Template variable correctness.
- Label drift between panel metadata and runtime Docker objects.
- Binary file storage and upload integrity.
- Image update detection differences across registries.
- Partial deployment failure.
- Migration compatibility over time.
- Docker CLI output differences.

Controls:

- Validate locally before remote changes.
- Upload into staging paths before activation.
- Use task stages for every remote operation.
- Version migration bundles.
- Keep Docker runtime commands out of template/service metadata services.
