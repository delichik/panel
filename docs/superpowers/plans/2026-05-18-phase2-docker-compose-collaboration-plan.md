# Phase 2 Docker Compose Collaboration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker runtime discovery, panel-managed Compose projects, resource/template deployment, and migration.

**Architecture:** Docker and Compose operations run through SSH using `RemoteExecutor`. `internal/docker` owns runtime CLI behavior, `internal/compose` owns panel project metadata and workflows, and every mutating operation uses `TaskRunner`.

**Tech Stack:** Go, SQLite, SSH, Docker CLI, Docker Compose CLI, Go templates, Vue 3, Element Plus, Pinia.

---

## Milestone 2A.1: Docker Capability and Status

**Owner:** Backend Operations

**Files:**

- Create: `internal/docker/model.go`
- Create: `internal/docker/runtime.go`
- Create: `internal/docker/service.go`
- Create: `internal/docker/handler.go`
- Modify: `internal/server/model.go`

- [ ] Implement Docker and Compose CLI detection.
- [ ] Store per-server Docker capability cache.
- [ ] Implement project and service/container status reads.
- [ ] Add Docker capability/status APIs.
- [ ] Add tests for Docker output parsing and unsupported states.

## Milestone 2A.2: Docker Read-Only UI

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/docker/pages/DockerPage.vue`
- Create: `web/src/features/docker/components/DockerCapabilityPanel.vue`
- Create: `web/src/features/docker/components/ComposeRuntimeStatus.vue`
- Create: `web/src/features/docker/api.ts`
- Create: `web/src/features/docker/types.ts`

- [ ] Show Docker availability by server.
- [ ] Show runtime Compose projects and service/container status.
- [ ] Handle unsupported, empty, loading, error, and success states.

## Milestone 2B.1: Compose Project and Resource Model

**Owner:** Backend Operations

**Files:**

- Create: `internal/compose/model.go`
- Create: `internal/compose/repository.go`
- Create: `internal/compose/project_service.go`
- Create: `internal/compose/resource_service.go`
- Create: `internal/templatex/renderer.go`

- [ ] Implement Compose project CRUD.
- [ ] Implement static resource metadata and storage.
- [ ] Implement template resource metadata and rendering.
- [ ] Validate data-root path safety.
- [ ] Add repository and service tests.

## Milestone 2B.2: Compose Project UI

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/compose/pages/ComposeProjectsPage.vue`
- Create: `web/src/features/compose/pages/ComposeProjectDetailPage.vue`
- Create: `web/src/features/compose/components/ComposeProjectForm.vue`
- Create: `web/src/features/compose/components/ComposeResourceList.vue`
- Create: `web/src/features/compose/components/TemplateEditor.vue`
- Create: `web/src/features/compose/api.ts`
- Create: `web/src/features/compose/types.ts`

- [ ] Implement project list and detail pages.
- [ ] Implement static resource management UI.
- [ ] Implement template editor and render preview.
- [ ] Show validation errors before deployment.

## Milestone 2B.3: Deployment and Migration

**Owner:** Backend Operations and Frontend Features

**Files:**

- Create: `internal/compose/deployment_service.go`
- Create: `internal/compose/migration_service.go`
- Modify: `web/src/features/compose/*`
- Create: `docs/compose-migration.md`

- [ ] Implement staged upload.
- [ ] Implement deploy, pull, up, restart, stop, and remove task workflows.
- [ ] Implement migration export and import.
- [ ] Add deployment and migration task panels.
- [ ] Verify export/import across two servers.

## Done Criteria

- [ ] Docker discovery works for supported and unsupported servers.
- [ ] Compose projects can be created, rendered, deployed, restarted, stopped, and migrated.
- [ ] Static and dynamic resources are stored under the configured data root.
- [ ] Every mutating operation is task-backed and log-visible.
