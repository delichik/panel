# Container Services Parallel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Container Services full redesign as a backend/frontend parallel effort, removing the legacy service-template/deployed-service product surface.

**Architecture:** Backend owns DB schema, validation, scheduling, rendering, task/lock/runtime orchestration, and `/api/v1/container-services` contracts. Frontend owns the Container Services workspace, Runtime Explorer integration, Task Center operation grouping, API client types, and removal of legacy Services/Templates UI.

**Tech Stack:** Go stdlib HTTP + SQLite + existing internal services; Vue 3 + TypeScript + Vuetify + existing API utilities.

---

## Shared Spec

Both workers must follow:

- `docs/superpowers/specs/2026-05-23-container-services-redesign.md`
- `AGENTS.md`

No compatibility or migration work is required. Old code paths should be removed, not bridged.

## Work Ownership

### Backend Worker Owns

- `internal/app`
- `internal/storage`
- `internal/tasks`
- `internal/scheduler`
- `internal/docker` only as runtime adapter / Runtime Explorer base
- create new backend modules:
  - `internal/containerservice`
  - `internal/containerrender`
  - `internal/placement`
  - `internal/containerops`
- remove old backend compose business module:
  - `internal/compose`
- backend tests under `internal/**`
- config/settings fields needed by Container Services

Backend worker must not edit `web/src/**`.

### Frontend Worker Owns

- `web/src/api`
- `web/src/types`
- `web/src/router`
- `web/src/layouts`
- `web/src/features`
- frontend tests under `web/src/**`

Frontend worker must not edit `internal/**`, `cmd/**`, or Go tests.

## Backend Task

### Task B1: Backend Container Services Control Plane

**Files:**

- Create: `internal/containerservice/model.go`
- Create: `internal/containerservice/service.go`
- Create: `internal/containerservice/handler.go`
- Create: `internal/containerservice/service_test.go`
- Create: `internal/containerrender/render.go`
- Create: `internal/containerrender/render_test.go`
- Create: `internal/placement/model.go`
- Create: `internal/placement/service.go`
- Create: `internal/placement/service_test.go`
- Create: `internal/containerops/worker.go`
- Create: `internal/containerops/locks.go`
- Create: `internal/containerops/locks_test.go`
- Modify: `internal/storage/migrations.go`
- Modify: `internal/tasks/model.go`
- Modify: `internal/tasks/service.go`
- Modify: `internal/tasks/handler.go`
- Modify: `internal/docker/model.go`
- Modify: `internal/docker/service.go`
- Modify: `internal/docker/handler.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/app/app.go`
- Delete: `internal/compose/**`

**Steps:**

- [ ] Add failing tests for Service name validation, forbidden full Compose input, forbidden `container_name`, `panel.*` label rules, host/non-host `panel.claims.ports`, depends_on extraction, dependency cycle detection, generation increment rules, selector non-increment, and render context paths.
- [ ] Add failing tests for task metadata fields, `task_steps`, and `operation_locks` lease acquire/heartbeat/expiry behavior.
- [ ] Replace old schema creation with new `container_services`, `container_service_files`, `container_runtime_cache`, `task_steps`, and `operation_locks` tables. Remove old service-template/deployed-service table creation.
- [ ] Extend existing task model with `operation_id`, trigger fields, node/server binding, durable step APIs, and whole-task retry semantics.
- [ ] Implement `containerservice` CRUD with immutable `name`, generation/spec hash transactions, save-enabled reconcile enqueue, enable-preview, enable, disable-preview, disable, delete constraints, validate, render-preview, schedule-preview, reconcile, restart, runtime, and logs handlers.
- [ ] Implement `containerrender` so a user Compose service body is wrapped under `services.<name>`, variables/files render with `missingkey=error`, system labels are injected through override output, and manifests are generated for display only.
- [ ] Implement `placement` with key/value selector matching, built-in Docker/Compose/include requirements, dependency same-node validation, managed port claim conflict detection, and stable preference for existing active node then name/id.
- [ ] Implement `containerops` DB worker path for reconcile, enable/disable chains, restart, generation cleanup, node root compose refresh, and service/node lock leases. Keep remote artifact untrusted and idempotency label-based.
- [ ] Update Docker runtime adapter to support include capability probe, managed label detection, live logs, 1s container/compose cache refresh and 10s images/networks/volumes refresh, and reject destructive managed Runtime Explorer actions except restart.
- [ ] Remove legacy compose handlers, services, models, imports, scheduler references, and old `/api/v1/services` / `/api/v1/service-templates` route registration.
- [ ] Run `go test ./...` and fix failures in the backend-owned code.

**Acceptance Criteria:**

- `go test ./...` passes.
- No active backend route remains for `/api/v1/services` or `/api/v1/service-templates`.
- New `/api/v1/container-services` API exists and matches the spec.
- Old `internal/compose` business module is removed.
- Runtime idempotency is based on labels, not remote manifest.

## Frontend Task

### Task F1: Frontend Container Services Workspace

**Files:**

- Create: `web/src/api/containerServices.ts`
- Create: `web/src/api/runtimeExplorer.ts`
- Create: `web/src/features/container-services/pages/ContainerServicesPage.vue`
- Create: `web/src/features/container-services/components/ServiceEditor.vue`
- Create: `web/src/features/container-services/components/ServiceDetail.vue`
- Create: `web/src/features/container-services/components/DependencyImpactDialog.vue`
- Create: `web/src/features/container-services/components/RuntimeStatusPanel.vue`
- Create: `web/src/features/runtime-explorer/pages/RuntimeExplorerPage.vue`
- Modify: `web/src/types/api.ts`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/layouts/AppLayout.vue`
- Modify: `web/src/features/tasks/pages/TaskCenterPage.vue`
- Delete: `web/src/api/compose.ts`
- Delete: `web/src/features/compose/**`
- Replace/retire: old Docker runtime page route if it conflicts with new Runtime Explorer

**Steps:**

- [ ] Add API client types for Container Services, files, validation, render preview, schedule preview, enable/disable previews, runtime/logs, Runtime Explorer resources, task operation grouping, and task steps.
- [ ] Replace navigation: remove legacy Services and Service Templates entries; add Container Services and Runtime Explorer.
- [ ] Replace routes: `/container-services`, `/runtime-explorer`, `/tasks`; remove `/services` and `/service-templates`; redirect old `/docker` only if needed to the new Runtime Explorer.
- [ ] Build Container Services page as the primary tool surface: dense service list, enabled/runtime/generation state, node, last task/error, quick edit/reconcile/restart/enable/disable/delete actions.
- [ ] Build Service Editor with YAML source-of-truth, visual common-field helper, variables/files panels, selector map editor, validation/render/schedule preview, and dependency impact confirmation.
- [ ] Build Service Detail with current DB generation/hash, observed runtime label generation/hash, dependency graph, live logs tail, recent tasks, and actions.
- [ ] Build enable/disable impact dialogs: enable shows dependency chain that will be enabled; disable shows dependent chain that will be disabled.
- [ ] Build Runtime Explorer page with containers/networks/volumes/images/capability, managed/unmanaged markers, managed destructive action blocking, managed restart, and unmanaged limited actions.
- [ ] Update Task Center to group/filter by `operation_id`, show trigger fields, and display task steps timeline plus task logs.
- [ ] Remove old compose API/tests/pages/components and fix imports.
- [ ] Run frontend tests/build (`npm test` and `npm run build` from `web`) and fix failures in frontend-owned code.

**Acceptance Criteria:**

- No legacy Services/Templates UI route or nav entry remains.
- Frontend uses `/api/v1/container-services` and Runtime Explorer APIs, not old compose APIs.
- Managed Runtime Explorer destructive actions are blocked except restart.
- Enabled save flow shows reconcile task or dependency confirmation.
- `npm test` and `npm run build` pass.

