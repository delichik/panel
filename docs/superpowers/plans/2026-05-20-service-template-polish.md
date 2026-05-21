# Service Template Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Compose services the primary runtime workflow, store service template Compose definitions as YAML, expand template variables, and make the UI full-screen and task-aware.

**Architecture:** `compose_yaml` remains the backend source of truth; visual editing is a frontend convenience that serializes to YAML. Backend validates Go template syntax, YAML structure, and Compose shape before saving or rendering, then injects built-in variables for servers and template files. The Docker page owns service/navigation composition while runtime resources only shows non-service Docker resources.

**Tech Stack:** Go, SQLite, Vue 3, Vuetify, Vite, Vitest.

---

### Task 1: Compose Backend Validation And Built-In Variables

**Files:**
- Modify: `internal/compose/service.go`
- Modify: `internal/compose/model.go`
- Test: `internal/compose/service_test.go`

- [ ] Add tests that saving invalid YAML fails and rendering exposes `.server`, `.servers`, `.files`, and file-path template variables.
- [ ] Implement YAML/Compose validation in `validateTemplate`.
- [ ] Add render-time built-ins for the selected server, every server, attached template files, and legacy `system_*` keys.
- [ ] Render template file paths before upload/persist while keeping binary content intact.
- [ ] Run `go test ./internal/compose`.

### Task 2: Service And Runtime Resource Separation

**Files:**
- Modify: `internal/compose/service.go`
- Modify: `web/src/features/compose/pages/ServicesPage.vue`
- Modify: `web/src/features/docker/pages/DockerRuntimePage.vue`
- Modify: `web/src/features/docker/pages/DockerPage.vue`
- Modify: `web/src/router/index.ts`

- [ ] Enrich listed services with template/server display fields if the current DTO supports them.
- [ ] Make Docker tab order Services, Runtime Resources, Service Templates.
- [ ] Remove runtime services from Runtime Resources.
- [ ] Show template badge or unmanaged text in Services.

### Task 3: Template Editor UI

**Files:**
- Modify: `web/src/features/compose/pages/ServiceTemplatesPage.vue`
- Modify: `web/src/types/api.ts`

- [ ] Rework the drawer to a narrower single-column flow.
- [ ] Expand visual Compose controls for common service fields and add multi-row editing.
- [ ] Keep YAML as the saved source and show validation errors near the affected editor.
- [ ] Move preview server above render preview and remove standalone validation display.
- [ ] Allow variables in template file paths and content.

### Task 4: Server Variables And Global Layout

**Files:**
- Modify: `web/src/features/servers/pages/ServersPage.vue`
- Modify: `web/src/layouts/AppLayout.vue`
- Modify: `web/src/styles/main.css`
- Modify: `web/src/features/overview/pages/OverviewPage.vue`
- Modify: `web/src/api/tasks.ts`

- [ ] Add server custom variables editing in the server edit dialog.
- [ ] Convert app shell to fixed full-height layout with internal content/card scrolling.
- [ ] Move page titles into page headers and make topbar show rotating running tasks.
- [ ] Improve Overview empty state with an onboarding action.

### Task 5: Verification

**Files:**
- Test: `internal/compose/service_test.go`
- Test: `web/src/**/*.test.ts`

- [ ] Run targeted Go tests.
- [ ] Run frontend typecheck/tests.
- [ ] Start the dev server and inspect the main pages in the browser if available.
