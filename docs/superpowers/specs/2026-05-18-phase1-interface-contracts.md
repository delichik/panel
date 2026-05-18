# Phase 1 Interface Contracts

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document defines early contracts for backend interfaces and HTTP APIs. Implementations can evolve, but consumers should not depend on lower-level details.

## Backend Interface Contracts

### RemoteExecutor

Purpose:

- Hide SSH connection details from all feature modules.

Suggested Go shape:

```go
type RemoteExecutor interface {
    Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
    ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
    Upload(ctx context.Context, target Target, transfer UploadSpec) error
    Download(ctx context.Context, target Target, transfer DownloadSpec) error
}

type Target struct {
    ServerID     string
    Host         string
    Port         int
    Username     string
    CredentialID string
}

type CommandSpec struct {
    Command string
    Env     map[string]string
    Timeout time.Duration
}

type CommandResult struct {
    Stdout     string
    Stderr     string
    ExitCode   int
    StartedAt  time.Time
    FinishedAt time.Time
    TimedOut   bool
}
```

Rules:

- `Command` is executed non-interactively.
- `ExecSudo` may only use passwordless sudo.
- Timeout errors must be typed so callers can show specific messages.
- Secret material must not appear in `CommandResult`.

### DistroAdapter

Purpose:

- Hide distribution-specific commands and parsers.

Suggested Go shape:

```go
type DistroAdapter interface {
    ID() string
    Supports(info OSRelease) bool
    ReadStatus(ctx context.Context, exec RemoteExecutor, target Target) (SystemStatus, error)
    CollectMetrics(ctx context.Context, exec RemoteExecutor, target Target) (MetricsSnapshot, error)
    ListUpgradeable(ctx context.Context, exec RemoteExecutor, target Target) ([]PackageUpdate, error)
    UpgradeSelected(ctx context.Context, exec RemoteExecutor, target Target, packages []string, log LogSink) error
    UpgradeAll(ctx context.Context, exec RemoteExecutor, target Target, log LogSink) error
}

type OSRelease struct {
    ID        string
    VersionID string
    PrettyName string
}
```

Rules:

- Phase 1 registers only `DebianAdapter`.
- Unsupported distributions return support metadata but no Debian commands.
- Package operation logs go through `LogSink`, not direct storage calls.

### TaskRunner

Purpose:

- Provide consistent task lifecycle and logs for all long-running workflows.

Suggested Go shape:

```go
type TaskRunner interface {
    Create(ctx context.Context, input CreateTaskInput) (Task, error)
    Start(ctx context.Context, taskID string) error
    Advance(ctx context.Context, taskID string, stage string, message string) error
    AppendLog(ctx context.Context, taskID string, stream string, line string) error
    Complete(ctx context.Context, taskID string, summary string) error
    Fail(ctx context.Context, taskID string, err error) error
}
```

Rules:

- `Create` must happen before remote execution starts.
- Logs are append-only.
- `cancelled` is reserved in Phase 1 and does not require cancellation execution.

### Future Interfaces

These interfaces are defined to protect future work from Phase 1 rewrites.

```go
type ContainerRuntime interface {
    ListProjects(ctx context.Context, target Target) ([]ComposeProject, error)
    ReadStatus(ctx context.Context, target Target, project string) (ComposeStatus, error)
}

type DNSProvider interface {
    ListRecords(ctx context.Context, zone string) ([]DNSRecord, error)
    CreateRecord(ctx context.Context, zone string, record DNSRecordInput) (DNSRecord, error)
    UpdateRecord(ctx context.Context, zone string, id string, record DNSRecordInput) (DNSRecord, error)
    DeleteRecord(ctx context.Context, zone string, id string) error
}

type CertificateProvider interface {
    Issue(ctx context.Context, req CertificateRequest) (CertificateBundle, error)
    Renew(ctx context.Context, certID string) (CertificateBundle, error)
}

type TemplateRenderer interface {
    Render(ctx context.Context, source string, data map[string]any) (string, error)
}
```

Phase 1 only needs types, package boundaries, and compile-time contracts for these future modules.

## HTTP API Principles

Base path:

- `/api/v1`

Response envelope:

```json
{
  "data": {},
  "error": null
}
```

Error envelope:

```json
{
  "data": null,
  "error": {
    "code": "server_not_supported",
    "message": "Server distribution is not supported",
    "details": {}
  }
}
```

Rules:

- Use stable IDs in URLs.
- Use `202 Accepted` for long-running actions that create tasks.
- Never return secrets.
- Use `409 Conflict` for delete attempts blocked by active references.
- Use `422 Unprocessable Entity` for valid JSON with invalid domain input.

## Auth APIs

`POST /api/v1/auth/login`

Request:

```json
{
  "username": "admin",
  "password": "secret"
}
```

Response:

```json
{
  "data": {
    "authenticated": true
  },
  "error": null
}
```

`POST /api/v1/auth/logout`

Response:

```json
{
  "data": {
    "authenticated": false
  },
  "error": null
}
```

`GET /api/v1/auth/session`

Response:

```json
{
  "data": {
    "authenticated": true,
    "username": "admin"
  },
  "error": null
}
```

## Server APIs

`GET /api/v1/servers`

Response:

```json
{
  "data": [
    {
      "id": "srv_01",
      "name": "debian-13-a",
      "host": "192.168.242.130",
      "port": 22,
      "sshUsername": "du",
      "credentialId": "cred_01",
      "os": {
        "id": "debian",
        "versionId": "13",
        "prettyName": "Debian GNU/Linux 13",
        "supported": true
      },
      "sudo": {
        "passwordless": true,
        "lastCheckedAt": "2026-05-18T12:00:00Z"
      },
      "createdAt": "2026-05-18T12:00:00Z",
      "updatedAt": "2026-05-18T12:00:00Z"
    }
  ],
  "error": null
}
```

`POST /api/v1/servers`

Request:

```json
{
  "name": "debian-13-a",
  "host": "192.168.242.130",
  "port": 22,
  "sshUsername": "du",
  "credentialId": "cred_01",
  "labels": ["lab"],
  "notes": "Debian 13 test server"
}
```

`PUT /api/v1/servers/{serverId}` uses the same editable fields.

`DELETE /api/v1/servers/{serverId}` removes the server if no active task blocks deletion.

`POST /api/v1/servers/{serverId}/test`

Response status:

- `202 Accepted`

Response:

```json
{
  "data": {
    "taskId": "task_01"
  },
  "error": null
}
```

## Credential APIs

`GET /api/v1/credentials`

Response:

```json
{
  "data": [
    {
      "id": "cred_01",
      "name": "lab password",
      "type": "password",
      "createdAt": "2026-05-18T12:00:00Z",
      "updatedAt": "2026-05-18T12:00:00Z"
    }
  ],
  "error": null
}
```

`POST /api/v1/credentials`

Password request:

```json
{
  "name": "lab password",
  "type": "password",
  "username": "du",
  "password": "secret"
}
```

Private key request:

```json
{
  "name": "lab key",
  "type": "private_key",
  "username": "du",
  "privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----",
  "passphrase": ""
}
```

Rules:

- List and detail responses never include `password`, `privateKey`, or `passphrase`.
- Updating a secret requires sending a new secret value.

## Overview and Metrics APIs

`GET /api/v1/overview`

Response:

```json
{
  "data": {
    "servers": [
      {
        "id": "srv_01",
        "name": "debian-13-a",
        "host": "192.168.242.130",
        "supported": true,
        "reachable": true,
        "metricsFresh": true,
        "packageUpdateCount": 12,
        "lastMetricsAt": "2026-05-18T12:00:00Z",
        "lastPackageRefreshAt": "2026-05-18T11:55:00Z"
      }
    ]
  },
  "error": null
}
```

`GET /api/v1/servers/{serverId}/metrics?range=1h`

Allowed ranges:

- `1h`
- `6h`
- `24h`

Response:

```json
{
  "data": {
    "range": "1h",
    "cpu": [{"time": "2026-05-18T12:00:00Z", "usagePercent": 14.2}],
    "memory": [{"time": "2026-05-18T12:00:00Z", "usedBytes": 2147483648, "totalBytes": 8589934592}],
    "disk": [{"time": "2026-05-18T12:00:00Z", "usedBytes": 32212254720, "totalBytes": 107374182400}],
    "network": [{"time": "2026-05-18T12:00:00Z", "rxBytesPerSecond": 1024, "txBytesPerSecond": 2048}]
  },
  "error": null
}
```

## Package APIs

`GET /api/v1/servers/{serverId}/packages/updates`

Response:

```json
{
  "data": {
    "serverId": "srv_01",
    "lastRefreshedAt": "2026-05-18T12:00:00Z",
    "updates": [
      {
        "name": "openssl",
        "installedVersion": "3.0.0-1",
        "candidateVersion": "3.0.0-2",
        "source": "bookworm-security"
      }
    ]
  },
  "error": null
}
```

`POST /api/v1/servers/{serverId}/packages/refresh`

Response status:

- `202 Accepted`

`POST /api/v1/servers/{serverId}/packages/upgrade-selected`

Request:

```json
{
  "packages": ["openssl", "curl"]
}
```

Response:

```json
{
  "data": {
    "taskId": "task_02"
  },
  "error": null
}
```

`POST /api/v1/servers/{serverId}/packages/upgrade-all`

Response:

```json
{
  "data": {
    "taskId": "task_03"
  },
  "error": null
}
```

## Task APIs

`GET /api/v1/tasks?status=running&limit=50`

Response:

```json
{
  "data": [
    {
      "id": "task_02",
      "type": "package_upgrade_selected",
      "serverId": "srv_01",
      "status": "running",
      "stage": "running",
      "percentage": null,
      "summary": "Upgrading 2 packages",
      "startedAt": "2026-05-18T12:00:00Z",
      "finishedAt": null
    }
  ],
  "error": null
}
```

`GET /api/v1/tasks/{taskId}`

`GET /api/v1/tasks/{taskId}/logs?after=0`

Response:

```json
{
  "data": {
    "nextCursor": 3,
    "logs": [
      {"cursor": 1, "time": "2026-05-18T12:00:01Z", "stream": "system", "line": "connecting"},
      {"cursor": 2, "time": "2026-05-18T12:00:02Z", "stream": "stdout", "line": "Reading package lists..."}
    ]
  },
  "error": null
}
```

## Settings API

`GET /api/v1/settings/runtime`

Response:

```json
{
  "data": {
    "listenAddress": "0.0.0.0:8080",
    "appDatabase": "data/db/app.db",
    "metricsDatabase": "data/db/metrics.db",
    "dataRoot": "data",
    "metricsRetentionDays": 7,
    "metricsCollectionIntervalSeconds": 60,
    "cleanupSchedule": "daily"
  },
  "error": null
}
```

## Frontend Type Naming

Frontend TypeScript DTOs should mirror API field names exactly:

- `ServerDto`
- `CredentialDto`
- `OverviewDto`
- `MetricsSeriesDto`
- `PackageUpdateDto`
- `TaskDto`
- `TaskLogDto`
- `RuntimeSettingsDto`

Use camelCase JSON fields in API responses to avoid frontend mapping boilerplate.
