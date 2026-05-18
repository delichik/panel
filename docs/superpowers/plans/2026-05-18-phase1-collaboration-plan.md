# Phase 1 Collaboration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Phase 1 Linux server panel as a modular monolith with clear work packages for parallel backend, frontend, QA, and documentation contributors.

**Architecture:** One Go backend serves a Vue 3 frontend and manages Debian servers over SSH. Application data and metrics data use separate SQLite databases, while long-running operations report through a shared task model.

**Tech Stack:** Go, Gin or Echo, GORM, SQLite, `golang.org/x/crypto/ssh`, Vue 3, Vite, Element Plus, Pinia, Vue Router, ECharts.

---

## Reference Documents

- Design source: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`
- Requirements breakdown: `docs/superpowers/specs/2026-05-18-phase1-requirements-breakdown.md`
- Module design: `docs/superpowers/specs/2026-05-18-phase1-module-design.md`
- Interface contracts: `docs/superpowers/specs/2026-05-18-phase1-interface-contracts.md`
- Development standards: `docs/superpowers/specs/2026-05-18-frontend-backend-development-standards.md`

## Workstream Boundaries

Backend Platform owns:

- `cmd/panel`
- `internal/app`
- `internal/config`
- `internal/storage`
- `internal/auth`
- `internal/tasks`
- `internal/scheduler`

Backend Remote owns:

- `internal/credential`
- `internal/server`
- `internal/sshx`
- `internal/linux`

Backend Operations owns:

- `internal/metrics`
- `internal/packages`

Frontend Shell owns:

- `web/src/app`
- `web/src/router`
- `web/src/api`
- `web/src/stores`
- `web/src/layouts`
- `web/src/components`

Frontend Features owns:

- `web/src/features/auth`
- `web/src/features/overview`
- `web/src/features/servers`
- `web/src/features/packages`
- `web/src/features/tasks`
- `web/src/features/settings`

QA/Docs owns:

- `docs/deployment.md`
- `docs/managed-server-prerequisites.md`
- `docs/operations.md`
- Integration test notes and manual verification checklists.

## Milestone 0: Project Foundation

### Task 0.1: Initialize Backend Skeleton

**Owner:** Backend Platform

**Files:**

- Create: `go.mod`
- Create: `cmd/panel/main.go`
- Create: `internal/app/app.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] Create the Go module and backend directories.
- [ ] Implement config defaults for listen address, app DB path, metrics DB path, data root, collection interval, and retention days.
- [ ] Add config tests covering defaults and validation failures.
- [ ] Run `go test ./...`.
- [ ] Commit as `chore: initialize backend skeleton`.

### Task 0.2: Initialize Frontend Skeleton

**Owner:** Frontend Shell

**Files:**

- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/src/main.ts`
- Create: `web/src/app/App.vue`
- Create: `web/src/router/index.ts`
- Create: `web/src/api/client.ts`

- [ ] Create the Vue 3 Vite app structure.
- [ ] Configure Element Plus, Pinia, and Vue Router.
- [ ] Implement the base API client for the response envelope.
- [ ] Add a placeholder authenticated layout and login route.
- [ ] Run frontend build and tests once tooling exists.
- [ ] Commit as `chore: initialize frontend skeleton`.

## Milestone 1: Platform Primitives

### Task 1.1: Storage and Migrations

**Owner:** Backend Platform

**Files:**

- Create: `internal/storage/store.go`
- Create: `internal/storage/migrations.go`
- Create: `internal/storage/store_test.go`

- [ ] Implement separate app and metrics SQLite connections.
- [ ] Add initial app DB tables for servers, credentials, package updates, tasks, and task logs.
- [ ] Add initial metrics DB tables for CPU, memory, disk, and network snapshots.
- [ ] Verify app and metrics schemas are created independently.
- [ ] Run `go test ./internal/storage`.
- [ ] Commit as `feat: add sqlite storage foundation`.

### Task 1.2: Auth Service and Middleware

**Owner:** Backend Platform

**Files:**

- Create: `internal/auth/service.go`
- Create: `internal/auth/handler.go`
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/service_test.go`

- [ ] Implement configured admin login.
- [ ] Implement session creation, validation, and logout.
- [ ] Add `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`, and `GET /api/v1/auth/session`.
- [ ] Protect a test route with auth middleware.
- [ ] Run auth tests.
- [ ] Commit as `feat: add panel authentication`.

### Task 1.3: Task Runner

**Owner:** Backend Platform

**Files:**

- Create: `internal/tasks/model.go`
- Create: `internal/tasks/repository.go`
- Create: `internal/tasks/service.go`
- Create: `internal/tasks/handler.go`
- Create: `internal/tasks/service_test.go`

- [ ] Implement task statuses and stages.
- [ ] Implement task creation and status transitions.
- [ ] Implement append-only logs with cursor polling.
- [ ] Add task list, detail, and log APIs.
- [ ] Run task tests.
- [ ] Commit as `feat: add task runner`.

## Milestone 2: Remote Management Foundation

### Task 2.1: Credential Service

**Owner:** Backend Remote

**Files:**

- Create: `internal/credential/model.go`
- Create: `internal/credential/repository.go`
- Create: `internal/credential/secret_store.go`
- Create: `internal/credential/service.go`
- Create: `internal/credential/handler.go`
- Create: `internal/credential/service_test.go`

- [ ] Implement password and private-key credential metadata.
- [ ] Implement secret storage behind `SecretStore`.
- [ ] Ensure API responses never include secrets.
- [ ] Reject deletion when credentials are referenced by servers.
- [ ] Run credential tests.
- [ ] Commit as `feat: add credential management`.

### Task 2.2: SSH Executor

**Owner:** Backend Remote

**Files:**

- Create: `internal/sshx/executor.go`
- Create: `internal/sshx/types.go`
- Create: `internal/sshx/errors.go`
- Create: `internal/sshx/executor_test.go`

- [ ] Implement `RemoteExecutor`.
- [ ] Support password and private-key authentication from resolved credentials.
- [ ] Capture stdout, stderr, exit code, timeout, and duration.
- [ ] Implement passwordless sudo execution.
- [ ] Add unit tests with fakes for timeout and command-result handling.
- [ ] Commit as `feat: add ssh executor`.

### Task 2.3: Linux Distro Adapter

**Owner:** Backend Remote

**Files:**

- Create: `internal/linux/model.go`
- Create: `internal/linux/detector.go`
- Create: `internal/linux/debian.go`
- Create: `internal/linux/debian_test.go`

- [ ] Parse `/etc/os-release`.
- [ ] Mark Debian 12 and Debian 13 as supported.
- [ ] Implement Debian status and package parsing helpers.
- [ ] Implement metrics command parsing helpers.
- [ ] Run Linux adapter tests.
- [ ] Commit as `feat: add debian distro adapter`.

### Task 2.4: Server Service

**Owner:** Backend Remote

**Files:**

- Create: `internal/server/model.go`
- Create: `internal/server/repository.go`
- Create: `internal/server/service.go`
- Create: `internal/server/handler.go`
- Create: `internal/server/service_test.go`

- [ ] Implement server CRUD.
- [ ] Associate servers with credentials.
- [ ] Implement connectivity test as a task.
- [ ] Detect distro and passwordless sudo state.
- [ ] Add server APIs.
- [ ] Run server tests.
- [ ] Commit as `feat: add server inventory`.

## Milestone 3: Metrics and Dashboard

### Task 3.1: Metrics Collection

**Owner:** Backend Operations

**Files:**

- Create: `internal/metrics/model.go`
- Create: `internal/metrics/repository.go`
- Create: `internal/metrics/collector.go`
- Create: `internal/metrics/query.go`
- Create: `internal/metrics/retention.go`
- Create: `internal/metrics/service_test.go`

- [ ] Implement metrics snapshot persistence in the metrics DB.
- [ ] Implement per-server collection through `DistroAdapter`.
- [ ] Implement chart queries for `1h`, `6h`, and `24h`.
- [ ] Implement retention cleanup.
- [ ] Run metrics tests.
- [ ] Commit as `feat: add metrics collection`.

### Task 3.2: Scheduler Jobs

**Owner:** Backend Platform

**Files:**

- Create: `internal/scheduler/scheduler.go`
- Create: `internal/scheduler/jobs.go`
- Create: `internal/scheduler/scheduler_test.go`

- [ ] Implement periodic metrics collection job.
- [ ] Implement metrics cleanup job.
- [ ] Add non-overlap protection per server and job type.
- [ ] Ensure one server failure does not stop other servers.
- [ ] Run scheduler tests.
- [ ] Commit as `feat: add scheduler jobs`.

### Task 3.3: Overview API

**Owner:** Backend Operations

**Files:**

- Create: `internal/overview/service.go`
- Create: `internal/overview/handler.go`
- Create: `internal/overview/service_test.go`

- [ ] Aggregate server health, metrics freshness, and package update counts.
- [ ] Add `GET /api/v1/overview`.
- [ ] Add `GET /api/v1/servers/{serverId}/metrics`.
- [ ] Run overview tests.
- [ ] Commit as `feat: add overview api`.

### Task 3.4: Overview Frontend

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/overview/pages/OverviewPage.vue`
- Create: `web/src/features/overview/components/ServerSummaryList.vue`
- Create: `web/src/features/overview/components/MetricsCharts.vue`
- Create: `web/src/features/overview/api.ts`
- Create: `web/src/features/overview/types.ts`

- [ ] Implement overview data loading.
- [ ] Render zero-server, loading, error, and success states.
- [ ] Render CPU, memory, disk, and network charts.
- [ ] Support range switching.
- [ ] Verify in browser with populated and empty API responses.
- [ ] Commit as `feat: add overview dashboard`.

## Milestone 4: Package Updates

### Task 4.1: Package Service

**Owner:** Backend Operations

**Files:**

- Create: `internal/packages/model.go`
- Create: `internal/packages/repository.go`
- Create: `internal/packages/service.go`
- Create: `internal/packages/handler.go`
- Create: `internal/packages/service_test.go`

- [ ] Implement package cache refresh.
- [ ] Store upgradeable package list and last refresh timestamp.
- [ ] Implement selected-package upgrade task.
- [ ] Implement full-upgrade task.
- [ ] Block unsupported servers and servers without passwordless sudo.
- [ ] Run package tests.
- [ ] Commit as `feat: add package update workflows`.

### Task 4.2: Package Updates Frontend

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/packages/pages/PackageUpdatesPage.vue`
- Create: `web/src/features/packages/components/PackageTable.vue`
- Create: `web/src/features/packages/components/PackageTaskPanel.vue`
- Create: `web/src/features/packages/api.ts`
- Create: `web/src/features/packages/types.ts`

- [ ] Implement server selector.
- [ ] Display package update list.
- [ ] Support manual refresh.
- [ ] Support selected and full upgrade actions.
- [ ] Poll task status and logs after actions.
- [ ] Verify in browser with mocked and real backend responses.
- [ ] Commit as `feat: add package updates page`.

## Milestone 5: Remaining Frontend Pages

### Task 5.1: Auth and App Shell

**Owner:** Frontend Shell

**Files:**

- Create: `web/src/features/auth/pages/LoginPage.vue`
- Create: `web/src/stores/auth.ts`
- Modify: `web/src/router/index.ts`
- Modify: `web/src/api/client.ts`

- [ ] Implement login page.
- [ ] Implement auth store and session refresh.
- [ ] Add protected route guards.
- [ ] Add logout action.
- [ ] Verify refresh and redirect behavior.
- [ ] Commit as `feat: add frontend auth flow`.

### Task 5.2: Servers Page

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/servers/pages/ServersPage.vue`
- Create: `web/src/features/servers/components/ServerForm.vue`
- Create: `web/src/features/servers/components/CredentialForm.vue`
- Create: `web/src/features/servers/components/ConnectivityResult.vue`
- Create: `web/src/features/servers/api.ts`
- Create: `web/src/features/servers/types.ts`

- [ ] Implement server list and forms.
- [ ] Implement credential creation and selection.
- [ ] Trigger connectivity tests.
- [ ] Show distro and sudo state.
- [ ] Verify delete conflict handling.
- [ ] Commit as `feat: add servers page`.

### Task 5.3: Task Center Page

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/tasks/pages/TaskCenterPage.vue`
- Create: `web/src/features/tasks/components/TaskList.vue`
- Create: `web/src/features/tasks/components/TaskDetail.vue`
- Create: `web/src/features/tasks/api.ts`
- Create: `web/src/features/tasks/types.ts`

- [ ] Implement task list filters.
- [ ] Implement task detail.
- [ ] Implement log polling by cursor.
- [ ] Render failed, running, completed, and empty states.
- [ ] Commit as `feat: add task center`.

### Task 5.4: Settings Page

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/settings/pages/SettingsPage.vue`
- Create: `web/src/features/settings/api.ts`
- Create: `web/src/features/settings/types.ts`

- [ ] Implement read-only runtime settings page.
- [ ] Ensure sensitive values are not displayed.
- [ ] Commit as `feat: add settings page`.

## Milestone 6: Documentation and Verification

### Task 6.1: Deployment Guide

**Owner:** QA/Docs

**Files:**

- Create: `docs/deployment.md`
- Create: `docs/managed-server-prerequisites.md`
- Create: `docs/operations.md`

- [ ] Document panel configuration.
- [ ] Document database and data root paths.
- [ ] Document Debian 12/13 managed-server prerequisites.
- [ ] Document passwordless sudo requirement.
- [ ] Document metrics retention and cleanup behavior.
- [ ] Commit as `docs: add deployment and operations guides`.

### Task 6.2: End-to-End Verification

**Owner:** QA/Docs with all workstreams

**Files:**

- Create: `docs/phase1-verification-checklist.md`

- [ ] Verify login and logout.
- [ ] Verify server creation with Debian 12 and Debian 13 test servers.
- [ ] Verify password and private-key credentials.
- [ ] Verify connectivity test task logs.
- [ ] Verify metrics collection and dashboard charts.
- [ ] Verify package refresh.
- [ ] Verify selected package upgrade.
- [ ] Verify full upgrade.
- [ ] Verify task center status and logs.
- [ ] Verify metrics cleanup with shortened retention in a test config.
- [ ] Commit as `docs: add phase1 verification checklist`.

## Integration Policy

- Merge by milestone order when possible.
- Backend interface changes must update contract docs and frontend DTOs in the same pull request.
- Long-running operation changes must include task lifecycle tests.
- Frontend pages must be manually checked in browser before merge.
- Integration tests that require live servers must be opt-in and documented.

## Done Criteria

Phase 1 is complete when:

- All in-scope pages are available through the authenticated UI.
- Debian 12 and Debian 13 servers can be managed over SSH.
- Metrics are stored in the metrics DB and cleaned by retention.
- Package refresh and upgrade actions run as tasks with logs.
- App DB and metrics DB are separate.
- Deployment and managed-server prerequisite docs are written.
- Manual verification checklist passes against the documented test environment.
