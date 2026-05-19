# Phase 2 Docker Compose Collaboration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker runtime discovery, service templates, deployed services, template-attached files, resource cleanup, image update workflows, and migration.

**Architecture:** Docker and Compose operations run through SSH using `RemoteExecutor`. `internal/docker` owns runtime CLI behavior for services, networks, volumes, images, pruning, and update checks. `internal/compose` owns service templates, deployed services, labels, rendering, deployment, sync, updates, and migration. Every mutating operation uses `TaskRunner`.

**Tech Stack:** Go, SQLite, SSH, Docker CLI, Docker Compose CLI, Go templates, Vue 3, Element Plus, Pinia.

---

## Milestone 2A.1: Docker Capability and Status

**Owner:** Backend Operations

**Closed loop:** API can refresh Docker capability for a real managed server, cache the result, list runtime services/containers, networks, volumes, and images, read selected service status, and preserve the last successful cache when refresh fails.

**Files:**

- Create: `internal/docker/model.go`
- Create: `internal/docker/runtime.go`
- Create: `internal/docker/service.go`
- Create: `internal/docker/handler.go`
- Modify: `internal/server/model.go`

- [ ] Implement Docker and Compose CLI detection.
- [ ] Store per-server Docker capability cache.
- [ ] Implement service/container, network, volume, and image status reads.
- [ ] Implement image update detection model.
- [ ] Add Docker capability/status APIs.
- [ ] Add tests for Docker output parsing and unsupported states.
- [ ] Update `docs/phase2-acceptance-gate.md` with any implementation-specific verification notes.

## Milestone 2A.2: Docker Read-Only UI

**Owner:** Frontend Features

**Closed loop:** Operator can move from server selection to capability refresh, services, networks, volumes, images, update prompts, and visible unsupported/error/empty states without mock data.

**Files:**

- Create: `web/src/features/docker/pages/DockerPage.vue`
- Create: `web/src/features/docker/components/DockerCapabilityPanel.vue`
- Create: `web/src/features/docker/components/ComposeRuntimeStatus.vue`
- Create: `web/src/features/docker/api.ts`
- Create: `web/src/features/docker/types.ts`

- [ ] Show Docker availability by server.
- [ ] Show runtime services/containers, networks, volumes, and images.
- [ ] Show image update availability, selected update, and update all actions.
- [ ] Add delete and delete-unused actions for networks, volumes, and images.
- [ ] Handle unsupported, empty, loading, error, and success states.
- [ ] Link Docker refresh/status failures to task or API error details in the UI.

## Milestone 2B.1: Service Template, Service, File, and Variable Model

**Owner:** Backend Operations

**Closed loop:** Service templates, deployed service metadata, binary files, text template files, system variables, server custom variables, render values, and rendered outputs can be created, validated, listed, updated, deleted, and round-tripped from disk/database without touching a remote server.

**Files:**

- Create: `internal/compose/model.go`
- Create: `internal/compose/repository.go`
- Create: `internal/compose/template_service.go`
- Create: `internal/compose/service_service.go`
- Create: `internal/compose/resource_service.go`
- Create: `internal/templatex/renderer.go`
- Modify: `internal/server/model.go`

- [ ] Implement `service_template` CRUD.
- [ ] Implement deployed `service` CRUD.
- [ ] Implement binary file metadata and storage.
- [ ] Implement text template file metadata and rendering.
- [ ] Implement system variable and server custom variable resolution.
- [ ] Treat missing variables as hard validation errors.
- [ ] Version templates and mark linked services drifted after template changes.
- [ ] Validate data-root path safety.
- [ ] Add repository and service tests.
- [ ] Add tests for template/service/file name sanitization and path traversal rejection.
- [ ] Add tests proving template render failures do not write rendered outputs.

## Milestone 2B.2: Service Template and Service UI

**Owner:** Frontend Features

**Closed loop:** Operator can create a service template, switch between visual and YAML editing, manage binary/template files, preview render output, configure variables, see validation errors, create services, and reach deploy/sync actions only when the local state is valid.

**Files:**

- Create: `web/src/features/compose/pages/ServiceTemplatesPage.vue`
- Create: `web/src/features/compose/pages/ServiceTemplateDetailPage.vue`
- Create: `web/src/features/compose/pages/ServicesPage.vue`
- Create: `web/src/features/compose/pages/ServiceDetailPage.vue`
- Create: `web/src/features/compose/components/ServiceTemplateForm.vue`
- Create: `web/src/features/compose/components/ServiceTemplateVisualEditor.vue`
- Create: `web/src/features/compose/components/ServiceTemplateYamlEditor.vue`
- Create: `web/src/features/compose/components/TemplateFileList.vue`
- Create: `web/src/features/compose/components/ServerVariableEditor.vue`
- Create: `web/src/features/compose/components/TemplateEditor.vue`
- Create: `web/src/features/compose/api.ts`
- Create: `web/src/features/compose/types.ts`

- [ ] Implement service template list and detail pages.
- [ ] Implement deployed service list and detail pages.
- [ ] Implement visual and YAML template editing.
- [ ] Implement binary file management UI.
- [ ] Implement text template file editor and render preview.
- [ ] Implement server custom variable UI.
- [ ] Show linked services, template versions, and drift/sync state.
- [ ] Show validation errors before deployment.
- [ ] Display local artifact state: binary files, template files, rendered preview, and missing required values.
- [ ] Keep deploy controls disabled or guarded while validation errors exist.

## Milestone 2B.3: Deployment, Sync, Updates, Cleanup, and Migration

**Owner:** Backend Operations and Frontend Features

**Closed loop:** Operator can deploy a panel-owned service, inspect task logs and runtime status, sync after template changes, update selected/all images, clean up Docker resources, stop/restart/remove through tasks, export a bundle, import it to another server, and deploy the imported service.

**Files:**

- Create: `internal/compose/deployment_service.go`
- Create: `internal/compose/migration_service.go`
- Modify: `web/src/features/compose/*`
- Create: `docs/compose-migration.md`

- [ ] Implement staged upload.
- [ ] Implement deploy, sync, pull, image update, up, restart, stop, and remove task workflows.
- [ ] Implement network, volume, and image delete/prune task workflows.
- [ ] Implement migration export and import.
- [ ] Add deployment and migration task panels.
- [ ] Verify export/import across two servers.
- [ ] Define remote active and staging path layout in docs.
- [ ] Log validation, rendering, staging upload, activation, Compose command, update, cleanup, and status refresh stages.
- [ ] Document rollback behavior and manual cleanup paths for partial failures.

## Done Criteria

- [ ] Docker discovery works for supported and unsupported servers.
- [ ] Service templates can be created, rendered, versioned, synced, and migrated.
- [ ] Services can be created, deployed, updated, restarted, stopped, removed, and migrated.
- [ ] Binary files and dynamic text template files are stored under the configured data root.
- [ ] Networks, volumes, and images can be listed, deleted, and pruned through tasks.
- [ ] Image updates show selected/all update prompts and execute through tasks.
- [ ] Every mutating operation is task-backed and log-visible.
- [ ] `docs/phase2-acceptance-gate.md` passes manually against real managed servers.
- [ ] `task backend:test`, `task web:test`, and `task web:build` pass.
