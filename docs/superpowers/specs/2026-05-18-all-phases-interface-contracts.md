# All Phases Interface Contracts

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document defines interface and API contracts for all product phases. Contracts should be implemented incrementally, but their boundaries should remain stable.

## Shared Backend Interfaces

### RemoteExecutor

```go
type RemoteExecutor interface {
    Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
    ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
    Upload(ctx context.Context, target Target, transfer UploadSpec) error
    Download(ctx context.Context, target Target, transfer DownloadSpec) error
    Stat(ctx context.Context, target Target, path string) (RemoteFileInfo, error)
    MkdirAll(ctx context.Context, target Target, path string, mode string) error
}
```

Needed by:

- Phase 1 packages and metrics.
- Phase 2 Docker/Compose deployment and migration.
- Phase 4 certificate sync.

### DistroAdapter

```go
type DistroAdapter interface {
    ID() string
    Supports(info OSRelease) bool
    ReadStatus(ctx context.Context, exec RemoteExecutor, target Target) (SystemStatus, error)
    CollectMetrics(ctx context.Context, exec RemoteExecutor, target Target) (MetricsSnapshot, error)
    ListUpgradeable(ctx context.Context, exec RemoteExecutor, target Target) ([]PackageUpdate, error)
    UpgradeSelected(ctx context.Context, exec RemoteExecutor, target Target, packages []string, log LogSink) error
    UpgradeAll(ctx context.Context, exec RemoteExecutor, target Target, log LogSink) error
    ReadPrerequisites(ctx context.Context, exec RemoteExecutor, target Target) (ServerPrerequisites, error)
}
```

Needed by:

- Phase 1 server support, packages, and metrics.
- Phase 2 Docker prerequisite display.
- Phase 4 certificate sync prerequisite display.

### TaskRunner

```go
type TaskRunner interface {
    Create(ctx context.Context, input CreateTaskInput) (Task, error)
    Start(ctx context.Context, taskID string) error
    Advance(ctx context.Context, taskID string, stage string, message string) error
    SetPercentage(ctx context.Context, taskID string, percentage *int) error
    AppendLog(ctx context.Context, taskID string, stream string, line string) error
    Complete(ctx context.Context, taskID string, summary string) error
    Fail(ctx context.Context, taskID string, err error) error
}
```

Task types:

- `server_connectivity_test`
- `package_refresh`
- `package_upgrade_selected`
- `package_upgrade_all`
- `docker_status_refresh`
- `compose_deploy`
- `compose_restart`
- `compose_migration_export`
- `compose_migration_import`
- `dns_provider_sync`
- `certificate_issue`
- `certificate_renew`
- `certificate_sync`

## Phase 2 Interfaces

### ContainerRuntime

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
```

Rules:

- Implementation uses SSH plus `docker` and `docker compose` CLI.
- Docker Engine API is not required.
- Mutating operations must be task-backed.

### TemplateRenderer

```go
type TemplateRenderer interface {
    Validate(ctx context.Context, source string, variables map[string]TemplateVariable) error
    Render(ctx context.Context, source string, values map[string]any) (string, error)
}
```

Rules:

- Dynamic resources are text only.
- Rendering happens locally before upload.
- Render failures block deployment.

### MigrationBundle

```go
type MigrationBundle struct {
    Version        string
    Project        ComposeProject
    StaticFiles    []ResourceManifest
    Templates      []TemplateManifest
    RenderValues   map[string]any
    Certificates   []CertificateReference
    CreatedAt      time.Time
}
```

Rules:

- Bundle version must be explicit.
- Import validates server capability and remote paths before deployment.

## Phase 3 Interfaces

### DNSProvider

```go
type DNSProvider interface {
    ID() string
    ListZones(ctx context.Context, credential ProviderCredential) ([]DNSZone, error)
    ListRecords(ctx context.Context, credential ProviderCredential, zoneID string) ([]DNSRecord, error)
    CreateRecord(ctx context.Context, credential ProviderCredential, zoneID string, input DNSRecordInput) (DNSRecord, error)
    UpdateRecord(ctx context.Context, credential ProviderCredential, zoneID string, recordID string, input DNSRecordInput) (DNSRecord, error)
    DeleteRecord(ctx context.Context, credential ProviderCredential, zoneID string, recordID string) error
}
```

Record model:

```go
type DNSRecord struct {
    ID        string
    ZoneID    string
    Type      string
    Name      string
    Content   string
    TTL       int
    Proxied   *bool
    Priority  *int
    Provider  string
    SyncedAt  time.Time
}
```

Rules:

- Provider IDs are external IDs and must not be used as internal domain reference IDs.
- Provider tokens are write-only secrets.
- Provider rate-limit and validation errors map to typed API errors.

## Phase 4 Interfaces

### CertificateProvider

```go
type CertificateProvider interface {
    ID() string
    Issue(ctx context.Context, input CertificateIssueInput, log LogSink) (CertificateBundle, error)
    Renew(ctx context.Context, input CertificateRenewInput, log LogSink) (CertificateBundle, error)
}
```

Certificate sync:

```go
type CertificateSyncer interface {
    Sync(ctx context.Context, target Target, cert CertificateBundle, destination CertificateDestination, log LogSink) error
}
```

Rules:

- Certificate private keys never appear in API responses or logs.
- Renewal must preserve the previous valid bundle until the new bundle is stored and verified.
- Server sync uses SSH upload and permission changes through `RemoteExecutor`.

## HTTP API Groups

All APIs use `/api/v1` and the shared envelope:

```json
{
  "data": {},
  "error": null
}
```

Long-running mutating APIs return `202 Accepted` and `taskId`.

### Phase 1 API Groups

- `/auth/*`
- `/servers/*`
- `/credentials/*`
- `/overview`
- `/servers/{serverId}/metrics`
- `/servers/{serverId}/packages/*`
- `/tasks/*`
- `/settings/runtime`

### Phase 2 API Groups

Docker capability and runtime:

- `GET /api/v1/servers/{serverId}/docker/capability`
- `POST /api/v1/servers/{serverId}/docker/refresh`
- `GET /api/v1/servers/{serverId}/docker/projects`
- `GET /api/v1/servers/{serverId}/docker/projects/{projectName}/status`

Panel-managed Compose projects:

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

### Phase 3 API Groups

Providers:

- `GET /api/v1/provider-credentials`
- `POST /api/v1/provider-credentials`
- `PUT /api/v1/provider-credentials/{credentialId}`
- `DELETE /api/v1/provider-credentials/{credentialId}`

DNS:

- `GET /api/v1/dns/providers`
- `GET /api/v1/dns/zones`
- `POST /api/v1/dns/zones/refresh`
- `GET /api/v1/dns/zones/{zoneId}/records`
- `POST /api/v1/dns/zones/{zoneId}/records`
- `PUT /api/v1/dns/zones/{zoneId}/records/{recordId}`
- `DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}`
- `GET /api/v1/domain-references`
- `POST /api/v1/domain-references`

### Phase 4 API Groups

Certificates:

- `GET /api/v1/certificates`
- `POST /api/v1/certificates/issue`
- `GET /api/v1/certificates/{certificateId}`
- `POST /api/v1/certificates/{certificateId}/renew`
- `DELETE /api/v1/certificates/{certificateId}`

Certificate sync:

- `GET /api/v1/certificates/{certificateId}/sync-targets`
- `POST /api/v1/certificates/{certificateId}/sync`
- `DELETE /api/v1/certificates/{certificateId}/sync-targets/{targetId}`

Compose certificate references:

- `POST /api/v1/compose/projects/{projectId}/certificate-references`
- `DELETE /api/v1/compose/projects/{projectId}/certificate-references/{referenceId}`

## Frontend DTO Naming

Phase 1:

- `ServerDto`, `CredentialDto`, `OverviewDto`, `MetricsSeriesDto`, `PackageUpdateDto`, `TaskDto`, `TaskLogDto`.

Phase 2:

- `DockerCapabilityDto`, `ComposeProjectDto`, `ComposeStatusDto`, `ComposeResourceDto`, `MigrationBundleDto`.

Phase 3:

- `ProviderCredentialDto`, `DNSZoneDto`, `DNSRecordDto`, `DomainReferenceDto`.

Phase 4:

- `CertificateDto`, `CertificateRequestDto`, `CertificateSyncTargetDto`, `CertificateReferenceDto`.

Use camelCase JSON fields across all phases.
