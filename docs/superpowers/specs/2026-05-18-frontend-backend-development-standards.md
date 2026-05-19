# Frontend and Backend Development Standards

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document defines development conventions so multiple contributors can build the panel without drifting across modules. It applies to all phases: MVP, Docker Compose, DNS, and SSL certificates.

## General Collaboration Rules

- Keep each change inside one module or one vertical feature slice.
- Do not bypass module interfaces to access another module's implementation details.
- Keep raw SSH behavior inside `internal/sshx`.
- Keep Debian command strings and parsers inside `internal/linux`.
- Keep task state transitions inside `internal/tasks`.
- Keep Docker CLI command behavior inside `internal/docker`.
- Keep service template metadata, deployed service metadata, labels, files, rendering, deployments, sync, updates, and migrations inside `internal/compose`.
- Keep DNS provider behavior inside `internal/dns`.
- Keep certificate provider, renewal, and sync behavior inside `internal/certs`.
- Keep frontend API calls inside `web/src/api`.
- Prefer small files with one clear responsibility.
- Add tests for behavior before or alongside implementation.

## Backend Standards

### Package Structure

Each backend feature package should use this internal shape when useful:

```text
internal/<module>/
  model.go
  repository.go
  service.go
  handler.go
  errors.go
  *_test.go
```

Guidelines:

- `model.go` defines domain types.
- `repository.go` owns database access.
- `service.go` owns business behavior.
- `handler.go` translates HTTP input and output.
- `errors.go` defines typed domain errors.
- Tests focus on repository behavior, service decisions, and handler status mapping.

### Error Handling

Use typed errors for expected domain failures:

- `ErrNotFound`
- `ErrUnauthorized`
- `ErrConflict`
- `ErrValidation`
- `ErrUnsupportedDistro`
- `ErrRemoteTimeout`
- `ErrRemoteCommandFailed`
- `ErrPasswordlessSudoRequired`
- `ErrProviderAuthFailed`
- `ErrProviderRateLimited`
- `ErrTemplateInvalid`
- `ErrArtifactPathInvalid`
- `ErrCertificateValidationFailed`

HTTP mapping:

- `400` malformed request body or query.
- `401` unauthenticated.
- `403` authenticated but operation not allowed.
- `404` resource not found.
- `409` conflict with existing references or running tasks.
- `422` domain validation failure.
- `429` provider rate limit when the upstream state is known.
- `500` unexpected server failure.
- `502` upstream provider or remote server failure.

### Database Rules

Application database owns:

- Servers.
- Credential metadata.
- Package update cache.
- Task metadata and logs.
- Docker capability cache.
- Service template metadata.
- Deployed service metadata and file metadata.
- DNS provider metadata, zone cache, record cache, and domain references.
- Certificate metadata, renewal state, sync targets, and service references.

Metrics database owns:

- CPU snapshots.
- Memory snapshots.
- Disk snapshots.
- Network snapshots.
- Optional status snapshots used for chart correlation.

Rules:

- Do not store time-series metrics in the application database.
- Do not store business state in the metrics database.
- Do not store service template file bytes, migration archives, or certificate private keys in SQLite by default.
- Migrations must be deterministic and idempotent.
- Repository methods receive `context.Context`.

### Remote Command Rules

- All remote commands must set a timeout.
- Commands must be non-interactive.
- Privileged commands must use passwordless sudo.
- Command output may be logged to task logs, but secrets must be redacted first.
- Feature modules must call `RemoteExecutor` and `DistroAdapter`, not raw SSH or inline distro commands.
- Docker and Compose modules must call `RemoteExecutor`; they must not open separate remote APIs.
- Certificate sync must call `RemoteExecutor`; it must not assume mounted filesystems.

### Task Rules

- Create a task before starting a long-running operation.
- Move status from `queued` to `running` before remote side effects.
- Update stage whenever the operator-facing activity changes.
- Append stdout and stderr lines in received order.
- Mark final status exactly once.
- Do not claim true percentage progress unless command output provides it reliably.

Task-backed operations across phases:

- Package refresh and upgrades.
- Docker status refresh when slow.
- Compose deploy, restart, stop, remove, migration export, and migration import.
- DNS provider cache refresh and bulk sync.
- Certificate issuance, renewal, and server sync.

### Scheduler Rules

- Scheduler jobs must accept `context.Context`.
- Jobs must not overlap for the same server and job type.
- A single server failure must not stop collection for other servers.
- Cleanup jobs must log deleted counts.
- Certificate renewal jobs must preserve the last known good certificate on failure.
- DNS and Docker refresh jobs must not delete local cache data until a provider/runtime response is successfully parsed.

### Filesystem Artifact Rules

Filesystem artifacts live under the configured data root:

- `data/keys/` for SSH private keys.
- `data/service_templates/<template-id>/static/` for binary/static template files.
- `data/service_templates/<template-id>/templates/` for text template files.
- `data/compose/<server-id>/<service>/rendered/` for rendered outputs.
- `data/compose/<server-id>/<service>/migration/` for migration bundles.
- `data/certs/` for certificate bundles and ACME material.
- `data/tmp/` for temporary staging files.

Rules:

- Write metadata to the app DB and file bytes to disk.
- Use staging paths before replacing active deployment artifacts.
- Never write secrets to task logs.
- Validate paths so project-level operations cannot escape the configured data root.

### Provider Rules

- Provider credentials are write-only secrets.
- Provider DTOs must be redacted.
- Provider implementations must map remote API failures into typed domain errors.
- Cloudflare-specific fields must not become generic DNS domain identifiers.
- Let's Encrypt-specific metadata must not leak into service schemas.

### Backend Testing Rules

Required test levels:

- Unit tests for distro parsing and command result parsing.
- Unit tests for Docker and Compose output parsing.
- Unit tests for Go template validation and rendering.
- Unit tests for DNS provider request/response mapping with mocked provider clients.
- Unit tests for certificate storage, renewal eligibility, and sync destination validation.
- Service tests for validation, unsupported distro blocking, provider failures, and task transitions.
- Repository tests for schema and persistence behavior.
- Handler tests for response codes, response envelopes, and secret redaction.

Remote integration tests:

- Use explicit test server or provider configuration, not hardcoded credentials in test code.
- Mark as integration tests so normal unit test runs do not require live servers or provider accounts.

## Frontend Standards

### Feature Layout

Each feature should use this shape:

```text
web/src/features/<feature>/
  pages/
  components/
  composables/
  types.ts
```

Shared code:

```text
web/src/api/
web/src/components/
web/src/components/tasks/
web/src/components/forms/
web/src/stores/
web/src/types/
```

Feature directories:

```text
web/src/features/auth/
web/src/features/overview/
web/src/features/servers/
web/src/features/packages/
web/src/features/tasks/
web/src/features/docker/
web/src/features/compose/
web/src/features/dns/
web/src/features/certificates/
web/src/features/settings/
```

Guidelines:

- Pages orchestrate data loading and layout.
- Components render focused UI pieces.
- Composables own reusable page behavior.
- Stores own cross-route state only.

### API Client Rules

- All HTTP calls go through `web/src/api`.
- API clients return typed data or throw typed API errors.
- Feature components do not build raw URLs.
- Polling logic is centralized in composables, not duplicated inside components.
- Provider secret inputs are write-only and never hydrated from API responses.

### UI State Rules

Every async page must handle:

- Initial loading.
- Empty state.
- Success state.
- Error state.
- Refreshing state when a background reload is active.

Long-running operations:

- Start operation through the feature API.
- Receive `taskId`.
- Poll task detail and logs through task API.
- Render current status, stage, and logs.

Provider and secret forms:

- Use write-only inputs for tokens, private keys, ACME account material, and passphrases.
- Never display saved secret values after creation.
- Show whether a secret is configured using metadata such as `configured: true`.

Artifact and resource forms:

- Compose static resources use file/directory-oriented UI.
- Compose dynamic resources use text template editors and variable forms.
- Certificate sync targets show remote destination paths and permission state.

### Page Responsibilities

Login:

- Minimal login form.
- Show authentication failure without exposing backend internals.

Overview:

- Show server summaries.
- Show selected server charts.
- Keep zero-server and stale-metrics states usable.

Servers:

- CRUD servers.
- Create and select credentials.
- Trigger connectivity test.
- Show distro, sudo, Docker, and certificate sync readiness states.

Package Updates:

- Select server.
- Refresh package updates.
- Select packages.
- Start selected or full upgrade.
- Embed task progress and logs.

Task Center:

- List running and recent tasks.
- Show task detail and logs.

Docker:

- Show Docker and Compose availability per server.
- Show runtime project and service/container status.
- Keep read-only discovery separate from mutating service template, service, cleanup, and update management.

Compose:

- Manage panel-owned projects.
- Manage static resources and dynamic templates.
- Render templates before deployment.
- Show deployment and migration tasks.
- Surface certificate references when Phase 4 is available.

DNS:

- Manage provider credentials.
- List zones and records.
- Create, update, and delete supported records.
- Manage domain references independent of provider-specific record IDs.

Certificates:

- Request certificates for managed domains.
- Show issuance, renewal, and sync task progress.
- Manage sync targets.
- Link certificates to service references.

Settings:

- Show read-only runtime config with secrets redacted.

### Frontend Testing Rules

Required test levels:

- API client tests for envelope and error handling.
- Store tests for auth and cross-route state.
- Component tests for empty, loading, error, and success states.
- Page tests for critical flows with mocked APIs.
- Feature tests for provider secret redaction behavior.
- Feature tests for Compose template validation states.
- Feature tests for certificate issuance and sync task polling.

Manual browser checks:

- Login redirect flow.
- Server create/edit/delete.
- Connectivity test task polling.
- Overview charts with empty and populated data.
- Package refresh and upgrade task polling.
- Task Center log polling.
- Docker unsupported and supported server states.
- Service template file creation, template rendering, deployment, sync, update, cleanup, and migration.
- DNS provider credential creation, zone refresh, and record CRUD.
- Certificate issue, renew, sync, and service reference flows.

## API Contract Rules

- Backend and frontend share the Phase 1 contract in `2026-05-18-phase1-interface-contracts.md` for MVP work.
- Backend and frontend share the all-phase contract in `2026-05-18-all-phases-interface-contracts.md` for roadmap work.
- Backend must not rename JSON fields without updating frontend DTOs and contract docs in the same change.
- Use camelCase JSON fields.
- Long-running action endpoints return `202` and `taskId`.
- Query endpoints return `200` and the requested data.
- Delete endpoints return `204` when no response body is needed.

## Documentation Rules

Required docs before Phase 1 is considered complete:

- Deployment and configuration guide.
- Managed server prerequisites guide.
- Operations guide for metrics retention, package updates, and troubleshooting.
- API contract updates for implemented endpoints.

Required docs before later phases are considered complete:

- Docker and Compose operations guide.
- Compose migration guide.
- DNS provider setup guide.
- Certificate issuance and renewal guide.
- Certificate sync and remote permissions guide.
- Provider troubleshooting guide.

Documentation must avoid secrets. Example server credentials in local notes must not be copied into public deployment docs.
