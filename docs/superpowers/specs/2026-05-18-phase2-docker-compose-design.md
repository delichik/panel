# Phase 2 Docker Compose Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

Phase 2 adds Docker Compose management while preserving the Phase 1 SSH-only control model. The implementation is split into read-only runtime discovery first, then mutating Compose project management. This reduces risk because Docker command parsing, remote environment differences, and UI models can stabilize before deployment actions are introduced.

## Phase Goal

Enable operators to inspect Docker/Compose status, manage panel-owned Compose projects, deploy static and dynamic resources, and migrate projects between managed servers.

## Scope

In scope:

- Docker CLI and Docker Compose CLI detection over SSH.
- Read-only runtime project and container/service status views.
- Panel-managed Compose project metadata.
- Static resource management.
- Dynamic text resources rendered through Go templates.
- Compose deployment workflows through SSH and `docker compose`.
- One-click migration bundles for project metadata and attached resources.

Out of scope:

- Docker Engine API integration.
- Non-Compose container orchestration.
- Registry credential management beyond what is available on the target server.
- Automatic certificate issuance; Phase 2 only stores certificate references if Phase 4 later provides them.

## Implementation Split

### Phase 2A: Docker Runtime Discovery

Purpose:

- Add Docker visibility without remote mutation.

Backend requirements:

- Detect `docker` and `docker compose` availability per server.
- Read Docker version, Compose version, and basic capability state.
- List runtime Compose projects.
- Read project service/container status.
- Store capability/status cache in the application database.

Frontend requirements:

- Docker availability indicators on server detail and overview.
- Docker page with server selector, project list, and project status.
- Unsupported and not-installed states.

Acceptance checks:

- Servers without Docker are shown as unsupported for Docker features.
- Servers with Docker show project and service/container state.
- Runtime refresh failures do not corrupt the last known cache.

### Phase 2B: Compose Project Management

Purpose:

- Manage panel-owned projects and deployment artifacts.

Backend requirements:

- Compose project CRUD.
- Static resources stored under `data/compose/<server-id>/<project>/static/`.
- Template resources stored under `data/compose/<server-id>/<project>/templates/`.
- Rendered outputs stored under `data/compose/<server-id>/<project>/rendered/`.
- Local Go template validation and rendering before upload.
- Staged remote upload before activation.
- Task-backed deploy, pull, up, restart, stop, remove, migration export, and migration import.

Frontend requirements:

- Compose Projects page.
- Project detail page with configuration, resources, templates, render preview, deployment actions, and task logs.
- Migration export/import UI.

Acceptance checks:

- Project deploy uploads Compose config and resources to predictable remote paths.
- Template render errors block deployment before any remote mutation.
- Deployment tasks show command stages and logs.
- Migration export/import preserves project metadata, resources, templates, render values, and certificate references.

## Backend Modules

`internal/docker`:

- Owns Docker CLI detection and runtime status.
- Exposes `ContainerRuntime`.
- Calls `RemoteExecutor`.

`internal/compose`:

- Owns panel-managed Compose project metadata.
- Owns resources, deployment, and migration.
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
    ReadComposeStatus(ctx context.Context, exec RemoteExecutor, target Target, project string) (ComposeStatus, error)
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

Compose projects:

- `GET /api/v1/compose/projects`
- `POST /api/v1/compose/projects`
- `GET /api/v1/compose/projects/{projectId}`
- `PUT /api/v1/compose/projects/{projectId}`
- `DELETE /api/v1/compose/projects/{projectId}`
- `POST /api/v1/compose/projects/{projectId}/render`
- `POST /api/v1/compose/projects/{projectId}/deploy`
- `POST /api/v1/compose/projects/{projectId}/restart`
- `POST /api/v1/compose/projects/{projectId}/stop`

Resources:

- `GET /api/v1/compose/projects/{projectId}/resources`
- `POST /api/v1/compose/projects/{projectId}/resources/static`
- `POST /api/v1/compose/projects/{projectId}/resources/template`
- `PUT /api/v1/compose/projects/{projectId}/resources/{resourceId}`
- `DELETE /api/v1/compose/projects/{projectId}/resources/{resourceId}`

Migration:

- `POST /api/v1/compose/projects/{projectId}/migration/export`
- `POST /api/v1/compose/migration/import`

## Difficulty and Risk Notes

High-risk areas:

- Remote path handling.
- Template variable correctness.
- Partial deployment failure.
- Migration compatibility over time.
- Docker CLI output differences.

Controls:

- Validate locally before remote changes.
- Upload into staging paths before activation.
- Use task stages for every remote operation.
- Version migration bundles.
- Keep Docker runtime commands out of Compose project metadata services.
