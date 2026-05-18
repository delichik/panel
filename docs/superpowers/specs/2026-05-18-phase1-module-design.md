# Phase 1 Module Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document defines module boundaries for a modular monolith. The boundary rule is simple: modules may depend on interfaces owned by another module, but raw implementation details must stay inside the owning module.

## Recommended Repository Layout

```text
cmd/panel/
internal/app/
internal/auth/
internal/config/
internal/credential/
internal/linux/
internal/metrics/
internal/packages/
internal/scheduler/
internal/server/
internal/sshx/
internal/storage/
internal/tasks/
internal/docker/
internal/dns/
internal/certs/
internal/templatex/
web/
docs/
data/
```

## Module Dependency Direction

Allowed dependency direction:

```text
cmd/panel
  -> internal/app
  -> feature modules
  -> internal/storage, internal/config, shared interfaces
```

Rules:

- `internal/sshx` owns raw `golang.org/x/crypto/ssh` usage.
- `internal/linux` owns distro-specific command generation and parsing.
- `internal/tasks` owns task status transitions and log persistence.
- `internal/metrics` owns the metrics database.
- `internal/packages` owns package update workflows but delegates command details to `internal/linux`.
- Future modules (`docker`, `dns`, `certs`, `templatex`) expose interfaces now and implementation later.

## Backend Modules

### `internal/config`

Responsibilities:

- Load and validate panel config.
- Provide defaults for database paths, data root, retention, and cleanup schedule.
- Redact sensitive values for settings APIs.

Exports:

- `Config`
- `Load(path string) (Config, error)`
- `Redacted() RedactedConfig`

Depends on:

- Standard library only.

### `internal/storage`

Responsibilities:

- Open application and metrics SQLite databases.
- Run schema migrations.
- Provide transaction helpers.
- Keep app DB and metrics DB handles separate.

Exports:

- `Store`
- `AppDB()`
- `MetricsDB()`
- `WithAppTx(ctx, fn)`
- `WithMetricsTx(ctx, fn)`

Depends on:

- `internal/config`
- SQLite driver and query layer.

### `internal/auth`

Responsibilities:

- Verify configured admin credentials.
- Manage sessions.
- Provide HTTP middleware for authenticated APIs.

Exports:

- `Service`
- `Login(ctx, username, password)`
- `Logout(ctx, sessionID)`
- `RequireAuth()`

Depends on:

- `internal/config`
- `internal/storage` only if sessions are persisted in app DB.

### `internal/server`

Responsibilities:

- CRUD managed server records.
- Associate a default credential.
- Store distro and sudo support state.
- Coordinate connectivity tests without owning raw SSH behavior.

Exports:

- `Service`
- `Repository`
- `Server`
- `CreateServer`, `UpdateServer`, `DeleteServer`, `ListServers`, `GetServer`
- `TestConnectivity`

Depends on:

- `internal/storage`
- `internal/credential`
- `internal/sshx`
- `internal/linux`
- `internal/tasks` for asynchronous connectivity tests if enabled.

### `internal/credential`

Responsibilities:

- Store credential metadata.
- Store and retrieve secrets through a credential store.
- Prevent secret leakage in DTOs.
- Validate credential usage before deletion.

Exports:

- `Service`
- `Repository`
- `Credential`
- `SecretStore`
- `CreateCredential`, `UpdateCredential`, `DeleteCredential`, `ResolveAuth`

Depends on:

- `internal/config`
- `internal/storage`

### `internal/sshx`

Responsibilities:

- Connect to remote hosts.
- Execute commands and sudo commands.
- Upload and download files.
- Enforce timeouts.
- Return structured execution results.

Exports:

- `RemoteExecutor`
- `SSHExecutor`
- `CommandSpec`
- `CommandResult`
- `TransferSpec`

Depends on:

- `internal/credential`
- `golang.org/x/crypto/ssh`

### `internal/linux`

Responsibilities:

- Detect Linux distribution support.
- Provide adapter interface for system status, metrics, and package operations.
- Implement Debian 12/13 commands and output parsing.

Exports:

- `DistroAdapter`
- `Detector`
- `DebianAdapter`
- `SystemStatus`
- `MetricsSnapshot`
- `PackageUpdate`

Depends on:

- `internal/sshx`

### `internal/tasks`

Responsibilities:

- Persist task metadata and logs.
- Standardize status and stage transitions.
- Expose polling APIs.
- Provide `TaskRunner` for long-running operations.

Exports:

- `Task`
- `TaskLog`
- `TaskRunner`
- `Repository`
- `Create`, `Start`, `Advance`, `AppendLog`, `Complete`, `Fail`

Depends on:

- `internal/storage`

### `internal/scheduler`

Responsibilities:

- Run periodic metrics collection, package cache refresh, and retention cleanup.
- Ensure jobs do not overlap per server and job type.
- Log job outcomes.

Exports:

- `Scheduler`
- `Job`
- `Start(ctx)`
- `Stop(ctx)`

Depends on:

- `internal/server`
- `internal/metrics`
- `internal/packages`
- `internal/tasks`

### `internal/metrics`

Responsibilities:

- Collect metrics through the distro adapter.
- Store snapshots in the metrics database.
- Query chart series.
- Delete expired snapshots.

Exports:

- `Collector`
- `Repository`
- `QueryService`
- `RetentionService`
- `ChartSeries`

Depends on:

- `internal/storage`
- `internal/server`
- `internal/linux`
- `internal/sshx`

### `internal/packages`

Responsibilities:

- Refresh package update cache.
- Execute selected package upgrades.
- Execute full package upgrades.
- Record task logs and current stage.

Exports:

- `Service`
- `Repository`
- `RefreshUpdates`
- `UpgradeSelected`
- `UpgradeAll`

Depends on:

- `internal/storage`
- `internal/server`
- `internal/linux`
- `internal/sshx`
- `internal/tasks`

### Future Placeholder Modules

`internal/docker`:

- Define `ContainerRuntime` and Compose project types.
- No Phase 1 UI or workflow implementation.

`internal/dns`:

- Define `DNSProvider`, record DTOs, and provider-neutral errors.
- No Phase 1 UI or provider implementation.

`internal/certs`:

- Define `CertificateProvider`, certificate metadata, and sync contract.
- No Phase 1 issuance workflow.

`internal/templatex`:

- Define `TemplateRenderer`.
- No Phase 1 project template workflow.

## Frontend Modules

Recommended layout:

```text
web/src/app/
web/src/api/
web/src/router/
web/src/stores/
web/src/layouts/
web/src/components/
web/src/features/auth/
web/src/features/overview/
web/src/features/servers/
web/src/features/packages/
web/src/features/tasks/
web/src/features/settings/
web/src/types/
```

Frontend boundary rules:

- Feature pages call backend through `web/src/api` clients only.
- Pinia stores own cross-page state; local component state stays in components.
- Shared UI components must not know feature-specific API endpoints.
- ECharts setup lives in feature or shared chart helpers, not inside API clients.

## Parallel Work Packages

Backend Platform:

- Owns `config`, `storage`, `auth`, `tasks`, and `scheduler`.
- Provides API middleware, database handles, migrations, and task primitives.

Backend Remote:

- Owns `credential`, `sshx`, `linux`, and server connectivity workflows.
- Provides stable remote execution and distro contracts.

Backend Operations:

- Owns `metrics` and `packages`.
- Uses platform and remote contracts but does not bypass them.

Frontend Shell:

- Owns app bootstrap, router, auth flow, API client, layout, and shared components.

Frontend Features:

- Owns overview, servers, package updates, task center, and settings pages.

QA/Docs:

- Owns test server setup notes, deployment docs, operations guide, and cross-module acceptance testing.

## Integration Contracts

Each backend feature must expose:

- Repository methods for persistence.
- Service methods for business behavior.
- HTTP handlers that translate request/response DTOs.
- Tests at repository and service level for behavior with meaningful branching.

Each frontend feature must expose:

- Page entry component.
- Feature-local components.
- API calls and TypeScript DTOs.
- Empty, loading, success, and error states.

Shared integration rule:

- Long-running actions return a task object or task ID immediately. The UI observes task progress through task APIs.
