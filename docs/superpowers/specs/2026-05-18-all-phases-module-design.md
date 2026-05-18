# All Phases Module Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document defines module ownership for the full roadmap. It supersedes the Phase 1-only module view when planning work beyond the MVP.

## Repository Layout

```text
cmd/panel/
internal/app/
internal/auth/
internal/config/
internal/storage/
internal/tasks/
internal/scheduler/
internal/server/
internal/credential/
internal/sshx/
internal/linux/
internal/metrics/
internal/packages/
internal/docker/
internal/compose/
internal/dns/
internal/certs/
internal/templatex/
internal/provider/
web/
docs/
data/
```

## Dependency Rules

- `internal/app` wires modules together.
- Feature modules depend on interfaces, repositories, and services, not on HTTP handlers from other modules.
- `internal/sshx` is the only module that uses raw SSH libraries.
- `internal/linux` is the only module that owns distro-specific command generation and parsing.
- `internal/tasks` is the only module that mutates task lifecycle state directly.
- `internal/docker` owns Docker CLI capability and status operations.
- `internal/compose` owns panel-managed Compose project metadata, resources, deployment, and migration.
- `internal/dns` owns provider-neutral DNS behavior.
- `internal/certs` owns provider-neutral certificate lifecycle and server sync.
- Provider-specific code must stay under provider packages, not in feature services or frontend DTO assumptions.

## Backend Modules by Phase

### Phase 1 Foundation Modules

`internal/config`:

- Loads config, validates defaults, redacts sensitive values.
- Must include future config sections for Docker paths, DNS providers, ACME settings, certificate sync defaults, and data-root directories without requiring those features to be active.

`internal/storage`:

- Opens app and metrics SQLite databases.
- Runs migrations.
- Provides transaction helpers.
- Keeps app DB and metrics DB separate for the entire roadmap.

`internal/auth`:

- Handles single local admin login, session validation, logout, and auth middleware.

`internal/tasks`:

- Persists task metadata and logs.
- Provides lifecycle transitions and polling APIs.
- Used by package updates, Docker actions, migrations, DNS sync, certificate issuance, renewal, and sync.

`internal/scheduler`:

- Runs metrics collection, package refresh, retention cleanup, Docker status refresh, DNS cache refresh, and certificate renewal jobs.
- Jobs must be independently enableable.

`internal/server`:

- Owns managed server records and server capability state.
- Capability state grows over phases: distro, sudo, Docker, Compose, certificate sync readiness.

`internal/credential`:

- Owns SSH credential metadata and secret storage.
- Must support future provider credentials either directly or through `internal/provider`.

`internal/sshx`:

- Owns SSH command execution, file upload/download, sudo wrapping, and remote filesystem helpers.

`internal/linux`:

- Owns Debian adapter for system status, metrics, package updates, and prerequisites.

`internal/metrics`:

- Owns metrics collection, chart queries, and retention cleanup.

`internal/packages`:

- Owns package cache refresh and upgrade workflows.

### Phase 2 Docker and Compose Modules

`internal/docker`:

- Detects Docker CLI and Compose CLI.
- Lists Docker containers and Compose project status.
- Executes Docker and Compose commands through `RemoteExecutor`.
- Does not own panel project metadata or local resources.

Exports:

- `ContainerRuntime`
- `DockerCapability`
- `ComposeStatus`
- `ContainerStatus`
- `RefreshDockerStatus`

`internal/compose`:

- Owns panel-managed Compose project records.
- Owns static resources, template resources, rendered outputs, deployment plans, and migration bundles.
- Calls `internal/docker` for runtime actions.
- Calls `internal/templatex` for rendering.
- Calls `internal/certs` only through certificate reference contracts.

Exports:

- `ProjectService`
- `ResourceService`
- `DeploymentService`
- `MigrationService`
- `ComposeProject`
- `ComposeResource`
- `MigrationBundle`

`internal/templatex`:

- Renders dynamic text resources using Go templates.
- Validates template input and blocks binary dynamic resources.

Exports:

- `TemplateRenderer`
- `RenderInput`
- `RenderResult`

### Phase 3 DNS Modules

`internal/provider`:

- Owns generic provider credential metadata and secret access where provider credentials differ from SSH credentials.
- Provides redaction and usage checks.

`internal/dns`:

- Owns provider-neutral zones, records, provider sync, record cache, and domain references.
- Cloudflare implementation lives behind `DNSProvider`.
- Exposes domain selection models for Compose and certificate modules.

Exports:

- `DNSProvider`
- `ProviderRegistry`
- `ZoneService`
- `RecordService`
- `DomainReferenceService`
- `DNSRecord`
- `DNSZone`
- `DomainReference`

### Phase 4 Certificate Modules

`internal/certs`:

- Owns certificate metadata, filesystem storage, issuance, renewal, sync, and references.
- Uses `internal/dns` for DNS validation abstractions when needed.
- Uses `internal/sshx` for server sync.
- Exposes references consumed by `internal/compose`.

Exports:

- `CertificateProvider`
- `CertificateService`
- `RenewalService`
- `SyncService`
- `CertificateReference`
- `CertificateBundle`

## Frontend Layout

```text
web/src/app/
web/src/api/
web/src/router/
web/src/stores/
web/src/layouts/
web/src/components/
web/src/components/tasks/
web/src/components/forms/
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
web/src/types/
```

Frontend rules:

- Feature pages call only typed API clients.
- Shared task/log widgets live in `web/src/components/tasks`.
- Provider credential forms must use redacted DTOs and write-only secret inputs.
- Compose resource editors must separate static file resources from dynamic template resources.
- Certificate and DNS features share domain-selection components without importing provider-specific UI state.

## Phase-Aware Navigation

Phase 1:

- Login, Overview, Servers, Package Updates, Task Center, Settings.

Phase 2:

- Add Docker/Compose section with Projects, Project Detail, Resources, Deployments, Migration.

Phase 3:

- Add DNS section with Providers, Zones, Records, Domain References.

Phase 4:

- Add Certificates section with Certificates, Requests, Renewals, Sync Targets.

## Data Boundary Rules

Application DB:

- Relational business state and caches.

Metrics DB:

- Retention-managed time-series metrics only.

Filesystem:

- Private keys, provider secret material when file-backed, Compose resources, rendered outputs, migration archives, certificate files, temporary deployment artifacts.

No module may store secrets in task logs or frontend-visible DTOs.

## Implementation Difficulty Notes

Hard early decisions that reduce later cost:

- Build generic task/log APIs in Phase 1.
- Store server capability records as extensible key/value or versioned structured fields.
- Keep file artifact metadata in the app DB and bytes on disk.
- Use provider registries for DNS and certificates from the beginning.
- Treat Compose project migration as a first-class service, not as a UI-only export button.

Decisions that would increase later cost:

- Embedding Debian commands in packages or metrics services.
- Embedding Docker commands in Compose project services.
- Storing certificates or Compose resources directly in SQLite blobs by default.
- Letting Cloudflare record IDs become generic domain identifiers.
- Letting Let's Encrypt certificate metadata leak into Compose project schemas.
