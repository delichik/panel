# All Phases Collaboration Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the full Linux server panel roadmap across MVP, Docker Compose, DNS, and SSL certificate phases with clear module ownership and low rework risk.

**Architecture:** The system remains a modular monolith: one Go backend, one Vue 3 frontend, separate SQLite databases for app state and metrics, and filesystem-backed artifacts for keys, Compose resources, migration bundles, and certificates. All remote server work flows through SSH abstractions and all long-running operations flow through shared task/log infrastructure.

**Tech Stack:** Go, Gin or Echo, GORM, SQLite, `golang.org/x/crypto/ssh`, Vue 3, Vite, Element Plus, Pinia, Vue Router, ECharts, Go templates, Docker CLI, Docker Compose CLI, Cloudflare API, ACME/Let's Encrypt.

---

## Reference Documents

- Product design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`
- All-phase requirements: `docs/superpowers/specs/2026-05-18-all-phases-requirements-breakdown.md`
- All-phase modules: `docs/superpowers/specs/2026-05-18-all-phases-module-design.md`
- All-phase interfaces: `docs/superpowers/specs/2026-05-18-all-phases-interface-contracts.md`
- Development standards: `docs/superpowers/specs/2026-05-18-frontend-backend-development-standards.md`

## Workstream Ownership

Backend Platform:

- `cmd/panel`, `internal/app`, `internal/config`, `internal/storage`, `internal/auth`, `internal/tasks`, `internal/scheduler`.

Backend Remote:

- `internal/server`, `internal/credential`, `internal/sshx`, `internal/linux`.

Backend Operations:

- `internal/metrics`, `internal/packages`, `internal/docker`, `internal/compose`, `internal/templatex`.

Backend Providers:

- `internal/provider`, `internal/dns`, `internal/certs`.

Frontend Shell:

- `web/src/app`, `web/src/router`, `web/src/api`, `web/src/stores`, `web/src/layouts`, shared components.

Frontend Features:

- `web/src/features/auth`, `overview`, `servers`, `packages`, `tasks`, `docker`, `compose`, `dns`, `certificates`, `settings`.

QA/Docs:

- Deployment, prerequisites, operations, provider setup, migration testing, and all verification checklists.

## Difficulty-Aware Sequencing

The roadmap intentionally does read-only discovery before mutating workflows:

- Docker discovery before Compose deployment.
- DNS CRUD before DNS-backed certificate validation.
- Certificate issuance before renewal and server sync.
- Compose certificate references after certificates and Compose projects both exist.

The roadmap also front-loads infrastructure that every later phase needs:

- Task logs.
- Scheduler.
- SSH executor.
- Filesystem artifact layout.
- Provider credential handling.
- Stable API envelope and typed frontend API clients.

## Phase 1: MVP Foundation

### Milestone 1.1: Platform Skeleton

**Owner:** Backend Platform and Frontend Shell

**Files:**

- Create: `go.mod`
- Create: `cmd/panel/main.go`
- Create: `internal/app/app.go`
- Create: `internal/config/config.go`
- Create: `internal/storage/store.go`
- Create: `web/package.json`
- Create: `web/src/main.ts`
- Create: `web/src/api/client.ts`

- [ ] Initialize Go backend.
- [ ] Initialize Vue frontend.
- [ ] Implement config defaults and validation.
- [ ] Implement API envelope handling.
- [ ] Add app and metrics database connection setup.
- [ ] Verify backend test command and frontend build command work.

### Milestone 1.2: Auth, Tasks, and Scheduler

**Owner:** Backend Platform and Frontend Shell

**Files:**

- Create: `internal/auth/*`
- Create: `internal/tasks/*`
- Create: `internal/scheduler/*`
- Create: `web/src/features/auth/*`
- Create: `web/src/features/tasks/*`
- Create: `web/src/components/tasks/*`

- [ ] Implement single-admin login and session middleware.
- [ ] Implement task model, log model, polling APIs, and shared frontend task components.
- [ ] Implement scheduler lifecycle with enableable jobs.
- [ ] Verify login, logout, protected route, and task log polling.

### Milestone 1.3: Server, Credentials, SSH, Debian Adapter

**Owner:** Backend Remote and Frontend Features

**Files:**

- Create: `internal/server/*`
- Create: `internal/credential/*`
- Create: `internal/sshx/*`
- Create: `internal/linux/*`
- Create: `web/src/features/servers/*`

- [ ] Implement server CRUD.
- [ ] Implement password and private-key credentials.
- [ ] Implement `RemoteExecutor`.
- [ ] Implement Debian 12/13 detection and prerequisite checks.
- [ ] Implement connectivity test as a task.
- [ ] Verify Debian 12 and Debian 13 test servers can be added and tested.

### Milestone 1.4: Metrics, Overview, Packages

**Owner:** Backend Operations and Frontend Features

**Files:**

- Create: `internal/metrics/*`
- Create: `internal/packages/*`
- Create: `internal/overview/*`
- Create: `web/src/features/overview/*`
- Create: `web/src/features/packages/*`

- [ ] Implement metrics collection and retention in the metrics DB.
- [ ] Implement overview aggregation and charts.
- [ ] Implement package refresh, selected upgrade, and full upgrade.
- [ ] Verify package operations use tasks and logs.
- [ ] Verify metrics cleanup with short retention in test config.

### Phase 1 Done Criteria

- [ ] Login, servers, overview, packages, task center, and settings are usable.
- [ ] App DB and metrics DB are separate.
- [ ] All remote operations use `RemoteExecutor`.
- [ ] All Debian behavior uses `DistroAdapter`.
- [ ] Documentation covers deployment and managed server prerequisites.

## Phase 2A: Docker Runtime Discovery

### Milestone 2.1: Docker Capability Model

**Owner:** Backend Operations

**Files:**

- Create: `internal/docker/model.go`
- Create: `internal/docker/runtime.go`
- Create: `internal/docker/service.go`
- Create: `internal/docker/handler.go`
- Modify: `internal/server/model.go`

- [ ] Implement Docker and Compose CLI detection through SSH.
- [ ] Store Docker capability cache per server.
- [ ] Add Docker capability API.
- [ ] Add scheduler job for Docker status refresh.
- [ ] Verify servers without Docker are handled as unsupported.

### Milestone 2.2: Read-Only Docker Frontend

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/docker/*`
- Modify: `web/src/features/overview/*`
- Modify: `web/src/features/servers/*`

- [ ] Add Docker availability display on server detail and overview.
- [ ] Add Docker projects/status page.
- [ ] Add loading, empty, unsupported, and error states.
- [ ] Verify Docker pages do not expose mutating actions yet.

## Phase 2B: Compose Management and Migration

### Milestone 2.3: Compose Project Metadata and Resources

**Owner:** Backend Operations and Frontend Features

**Files:**

- Create: `internal/compose/model.go`
- Create: `internal/compose/repository.go`
- Create: `internal/compose/project_service.go`
- Create: `internal/compose/resource_service.go`
- Create: `internal/templatex/*`
- Create: `web/src/features/compose/*`

- [ ] Implement Compose project CRUD.
- [ ] Implement static resource metadata and file storage.
- [ ] Implement dynamic template resource metadata.
- [ ] Implement local template validation and rendering.
- [ ] Verify dynamic resources are text-only and render before deployment.

### Milestone 2.4: Compose Deployment Workflows

**Owner:** Backend Operations

**Files:**

- Create: `internal/compose/deployment_service.go`
- Modify: `internal/docker/runtime.go`
- Modify: `web/src/features/compose/*`

- [ ] Implement staged upload of compose files and resources.
- [ ] Implement deploy, pull, up, restart, stop, and remove task workflows.
- [ ] Log every command stage through `TaskRunner`.
- [ ] Verify failed deployment leaves diagnostic logs and does not hide remote command errors.

### Milestone 2.5: One-Click Migration

**Owner:** Backend Operations and QA/Docs

**Files:**

- Create: `internal/compose/migration_service.go`
- Create: `docs/compose-migration.md`
- Modify: `web/src/features/compose/*`

- [ ] Implement migration bundle export.
- [ ] Implement migration bundle import validation.
- [ ] Include project metadata, static resources, template sources, render inputs, rendered outputs when required, and certificate references.
- [ ] Verify export/import across two managed servers.

## Phase 3A: DNS Provider and Cloudflare CRUD

### Milestone 3.1: Provider Credentials

**Owner:** Backend Providers and Frontend Features

**Files:**

- Create: `internal/provider/*`
- Create: `web/src/features/dns/components/ProviderCredentialForm.vue`

- [ ] Implement provider credential metadata and write-only secret storage.
- [ ] Implement redacted provider credential APIs.
- [ ] Verify provider token values are never returned by APIs.

### Milestone 3.2: DNS Provider Framework and Cloudflare

**Owner:** Backend Providers

**Files:**

- Create: `internal/dns/model.go`
- Create: `internal/dns/provider.go`
- Create: `internal/dns/cloudflare.go`
- Create: `internal/dns/service.go`
- Create: `internal/dns/handler.go`

- [ ] Implement `DNSProvider`.
- [ ] Implement Cloudflare zones and record CRUD.
- [ ] Implement provider error mapping.
- [ ] Implement DNS cache refresh task.
- [ ] Verify list, create, update, and delete against a test zone.

### Milestone 3.3: DNS Frontend and Domain References

**Owner:** Frontend Features and Backend Providers

**Files:**

- Create: `web/src/features/dns/*`
- Create: `internal/dns/domain_reference_service.go`

- [ ] Implement DNS provider, zone, and record pages.
- [ ] Implement domain references independent of provider record IDs.
- [ ] Implement reusable domain selector component.
- [ ] Verify domain references survive provider refresh.

## Phase 4A: Certificate Issuance

### Milestone 4.1: Certificate Metadata and Storage

**Owner:** Backend Providers

**Files:**

- Create: `internal/certs/model.go`
- Create: `internal/certs/repository.go`
- Create: `internal/certs/storage.go`
- Create: `web/src/features/certificates/*`

- [ ] Implement certificate metadata schema.
- [ ] Implement certificate filesystem storage under `data/certs`.
- [ ] Implement certificate list/detail APIs with redacted file material.
- [ ] Verify private keys are never returned by APIs.

### Milestone 4.2: Let's Encrypt Issuance

**Owner:** Backend Providers and Frontend Features

**Files:**

- Create: `internal/certs/provider.go`
- Create: `internal/certs/letsencrypt.go`
- Create: `internal/certs/issue_service.go`
- Modify: `web/src/features/certificates/*`

- [ ] Implement `CertificateProvider`.
- [ ] Implement Let's Encrypt issuance flow.
- [ ] Integrate DNS validation through provider-neutral DNS contracts when automation is used.
- [ ] Emit task stages for challenge creation, validation wait, issuance, storage, and verification.
- [ ] Verify issuance for a managed domain.

## Phase 4B: Renewal, Sync, and Compose References

### Milestone 4.3: Renewal Scheduler

**Owner:** Backend Providers and Backend Platform

**Files:**

- Create: `internal/certs/renewal_service.go`
- Modify: `internal/scheduler/jobs.go`
- Modify: `web/src/features/certificates/*`

- [ ] Implement renewal threshold config.
- [ ] Implement automatic renewal job.
- [ ] Preserve previous valid certificate until new certificate is stored and verified.
- [ ] Verify renewal task logs and failure states.

### Milestone 4.4: Server Sync and Compose References

**Owner:** Backend Providers, Backend Operations, Frontend Features

**Files:**

- Create: `internal/certs/sync_service.go`
- Modify: `internal/compose/model.go`
- Modify: `internal/compose/deployment_service.go`
- Modify: `web/src/features/certificates/*`
- Modify: `web/src/features/compose/*`

- [ ] Implement certificate sync targets.
- [ ] Upload cert/key files to managed servers through SSH.
- [ ] Set remote file permissions.
- [ ] Allow Compose projects to reference certificate deployment paths.
- [ ] Make project reload after certificate sync explicit and task-backed.
- [ ] Verify failed sync does not delete the last known good certificate.

## Cross-Phase Verification

- [ ] Unit tests cover parsers, validators, provider adapters, and task transitions.
- [ ] Service tests cover unsupported state, validation, and failure behavior.
- [ ] Handler tests cover API envelope, status codes, and secret redaction.
- [ ] Browser checks cover every frontend page's loading, empty, error, and success states.
- [ ] Integration tests requiring live servers or providers are opt-in and documented.
- [ ] Documentation is updated in the same milestone as feature behavior.

## Global Done Criteria

- [ ] Phase 1 MVP runs end to end on Debian 12/13 test servers.
- [ ] Phase 2 manages Compose projects through SSH and supports migration.
- [ ] Phase 3 manages Cloudflare DNS records through provider-neutral contracts.
- [ ] Phase 4 issues, renews, syncs, and references certificates safely.
- [ ] No feature module bypasses `RemoteExecutor`, provider interfaces, or `TaskRunner`.
- [ ] No API returns secrets.
- [ ] All long-running mutating operations expose task status and logs.
